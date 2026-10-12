package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Bookings groups the booking handlers and the DB pool they share.
type Bookings struct {
	DB *pgxpool.Pool
}

// Booking is the JSON shape returned to the web app for a single booking.
type Booking struct {
	ID          string    `json:"id"`
	TripID      string    `json:"tripId"`
	PassengerID string    `json:"passengerId"`
	Seats       int       `json:"seats"`
	SmallBags   int       `json:"smallBags"`
	LargeBags   int       `json:"largeBags"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

// BookingWithTrip wraps a booking with its ride and everyone on it.
type BookingWithTrip struct {
	Booking
	Trip         TripWithDriver `json:"trip"`
	ReviewRating *int           `json:"reviewRating"`
}

type bookingRequest struct {
	TripID    string `json:"tripId"`
	Seats     int    `json:"seats"`
	SmallBags int    `json:"smallBags"`
	LargeBags int    `json:"largeBags"`
}

// bagsLeftError returns the 409 message when a booking asks for more bags of one size than the ride has left, or "".
// A ride that offers none of that size says so instead of "Only 0 left"
func bagsLeftError(size string, want, total, left int) string {
	if want <= left {
		return ""
	}
	if total == 0 {
		return fmt.Sprintf("This ride has no space for %s bags.", size)
	}
	if left <= 0 {
		return fmt.Sprintf("No %s bag space left on this ride.", size)
	}
	plural := "s"
	if left == 1 {
		plural = ""
	}
	return fmt.Sprintf("Only %d %s bag%s left.", left, size, plural)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUID guards queries on uuid columns: Postgres rejects malformed ids with an error, which would surface as a 500
func isUUID(s string) bool { return uuidPattern.MatchString(s) }

// ListMine returns confirmed bookings for the authenticated passenger, soonest-departing first.
func (bkg *Bookings) ListMine(c *gin.Context) {
	user := CurrentUser(c)
	rows, err := bkg.DB.Query(c.Request.Context(), `
		SELECT
			b.id, b.ride_id, b.passenger_id, b.seats, b.small_bags, b.large_bags, b.status, b.created_at,
			r.id, r.driver_id, r.origin_city, r.origin_country, r.destination_city, r.destination_country,
			r.departure_at, r.seats_total,
			`+bookedOf("r.id")+`,
				r.small_bags_total, r.large_bags_total,
			r.price_per_seat, r.currency, r.notes, r.created_at,
			`+passengersOf("r.id", "r.driver_id")+`,
			d.id, d.name, d.created_at,
			(
				SELECT rv.rating
				FROM reviews rv
				WHERE rv.ride_id = r.id
				  AND rv.reviewer_id = b.passenger_id
				LIMIT 1
			) AS review_rating
		FROM bookings b
		JOIN rides r ON b.ride_id = r.id
		JOIN users d ON r.driver_id = d.id
		WHERE b.passenger_id = $1 AND b.status = 'confirmed'
		ORDER BY r.departure_at ASC`, user.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	list := make([]*BookingWithTrip, 0)
	for rows.Next() {
		var bt BookingWithTrip
		var status string
		err := rows.Scan(
			&bt.ID, &bt.TripID, &bt.PassengerID, &bt.Seats, &bt.SmallBags, &bt.LargeBags, &status, &bt.CreatedAt,
			&bt.Trip.ID, &bt.Trip.DriverID,
			&bt.Trip.OriginCity, &bt.Trip.OriginCountry,
			&bt.Trip.DestinationCity, &bt.Trip.DestinationCountry,
			&bt.Trip.DepartureAt,
			&bt.Trip.SeatsTotal, &bt.Trip.SeatsBooked, &bt.Trip.SmallBagsBooked, &bt.Trip.LargeBagsBooked,
			&bt.Trip.SmallBagsTotal, &bt.Trip.LargeBagsTotal,
			&bt.Trip.PricePerSeat, &bt.Trip.Currency, &bt.Trip.Notes,
			&bt.Trip.CreatedAt, &bt.Trip.Passengers,
			&bt.Trip.Driver.ID, &bt.Trip.Driver.Name, &bt.Trip.Driver.JoinedAt,
			&bt.ReviewRating,
		)
		if err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}
		bt.Status = status
		list = append(list, &bt)
	}
	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, list)
}

// Create inserts a new booking for the authenticated passenger.
func (bkg *Bookings) Create(c *gin.Context) {
	var req bookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if req.Seats < 1 {
		fail(c, http.StatusBadRequest, "You must book at least 1 seat.")
		return
	}
	if req.SmallBags < 0 || req.LargeBags < 0 {
		fail(c, http.StatusBadRequest, "Bag counts cannot be negative.")
		return
	}
	tripID := req.TripID
	if tripID == "" {
		fail(c, http.StatusBadRequest, "Trip ID is required.")
		return
	}
	if !isUUID(tripID) {
		fail(c, http.StatusNotFound, "Trip not found.")
		return
	}

	user := CurrentUser(c)

	tx, err := bkg.DB.Begin(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer tx.Rollback(c.Request.Context())

	// Check ride existence and capacity with row lock
	var driverID string
	var seatsTotal, smallTotal, largeTotal int
	var departureAt time.Time
	err = tx.QueryRow(c.Request.Context(),
		`SELECT driver_id, seats_total, small_bags_total, large_bags_total, departure_at FROM rides WHERE id = $1 FOR UPDATE`, tripID,
	).Scan(&driverID, &seatsTotal, &smallTotal, &largeTotal, &departureAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, http.StatusNotFound, "Trip not found.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	if driverID == user.ID {
		fail(c, http.StatusBadRequest, "You cannot book your own ride.")
		return
	}
	if !departureAt.After(time.Now()) {
		fail(c, http.StatusConflict, "This trip has already departed.")
		return
	}

	// Check if already booked
	var existingCount int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM bookings WHERE ride_id = $1 AND passenger_id = $2 AND status = 'confirmed'`,
		tripID, user.ID,
	).Scan(&existingCount)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if existingCount > 0 {
		fail(c, http.StatusConflict, "You already have a booking on this trip.")
		return
	}

	// Calculate currently booked seats and bags
	var seatsBooked, smallBooked, largeBooked int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT COALESCE(SUM(seats), 0)::int, COALESCE(SUM(small_bags), 0)::int, COALESCE(SUM(large_bags), 0)::int
		 FROM bookings WHERE ride_id = $1 AND status = 'confirmed'`,
		tripID,
	).Scan(&seatsBooked, &smallBooked, &largeBooked)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	freeSeats := seatsTotal - seatsBooked
	if req.Seats > freeSeats {
		if freeSeats <= 0 {
			fail(c, http.StatusConflict, "This trip is fully booked.")
		} else {
			plural := "s"
			if freeSeats == 1 {
				plural = ""
			}
			fail(c, http.StatusConflict, fmt.Sprintf("Only %d seat%s left.", freeSeats, plural))
		}
		return
	}
	if msg := bagsLeftError("small", req.SmallBags, smallTotal, smallTotal-smallBooked); msg != "" {
		fail(c, http.StatusConflict, msg)
		return
	}
	if msg := bagsLeftError("large", req.LargeBags, largeTotal, largeTotal-largeBooked); msg != "" {
		fail(c, http.StatusConflict, msg)
		return
	}

	var b Booking
	var status string
	err = tx.QueryRow(c.Request.Context(),
		`INSERT INTO bookings (ride_id, passenger_id, seats, small_bags, large_bags, status)
		 VALUES ($1, $2, $3, $4, $5, 'confirmed')
		 RETURNING id, ride_id, passenger_id, seats, small_bags, large_bags, status, created_at`,
		tripID, user.ID, req.Seats, req.SmallBags, req.LargeBags,
	).Scan(&b.ID, &b.TripID, &b.PassengerID, &b.Seats, &b.SmallBags, &b.LargeBags, &status, &b.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			fail(c, http.StatusConflict, "You already have a booking on this trip.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	b.Status = status

	if err := tx.Commit(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	c.JSON(http.StatusCreated, b)
}

// Bag fields are pointers so a seats-only PATCH keeps the bags already booked
type updateBookingRequest struct {
	Seats     int  `json:"seats"`
	SmallBags *int `json:"smallBags"`
	LargeBags *int `json:"largeBags"`
}

// Update changes the number of seats on an existing confirmed booking.
func (bkg *Bookings) Update(c *gin.Context) {
	id := c.Param("id")
	if !isUUID(id) {
		fail(c, http.StatusNotFound, "Booking not found.")
		return
	}
	var req updateBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if req.Seats < 1 {
		fail(c, http.StatusBadRequest, "You must book at least 1 seat.")
		return
	}
	if (req.SmallBags != nil && *req.SmallBags < 0) || (req.LargeBags != nil && *req.LargeBags < 0) {
		fail(c, http.StatusBadRequest, "Bag counts cannot be negative.")
		return
	}

	user := CurrentUser(c)

	tx, err := bkg.DB.Begin(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer tx.Rollback(c.Request.Context())

	// Find the booking and lock it
	var rideID string
	var currentSeats, currentSmall, currentLarge int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT ride_id, seats, small_bags, large_bags FROM bookings WHERE id = $1 AND passenger_id = $2 AND status = 'confirmed' FOR UPDATE`,
		id, user.ID,
	).Scan(&rideID, &currentSeats, &currentSmall, &currentLarge)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, http.StatusNotFound, "Booking not found.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	// Check capacity on ride
	var seatsTotal, smallTotal, largeTotal int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT seats_total, small_bags_total, large_bags_total FROM rides WHERE id = $1 FOR UPDATE`, rideID,
	).Scan(&seatsTotal, &smallTotal, &largeTotal)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	var seatsBooked, smallBooked, largeBooked int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT COALESCE(SUM(seats), 0)::int, COALESCE(SUM(small_bags), 0)::int, COALESCE(SUM(large_bags), 0)::int
		 FROM bookings WHERE ride_id = $1 AND status = 'confirmed'`,
		rideID,
	).Scan(&seatsBooked, &smallBooked, &largeBooked)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	available := seatsTotal - (seatsBooked - currentSeats)
	if req.Seats > available {
		plural := "s"
		if available == 1 {
			plural = ""
		}
		fail(c, http.StatusConflict, fmt.Sprintf("Only %d seat%s available.", available, plural))
		return
	}

	small, large := currentSmall, currentLarge
	if req.SmallBags != nil {
		small = *req.SmallBags
	}
	if req.LargeBags != nil {
		large = *req.LargeBags
	}
	if msg := bagsLeftError("small", small, smallTotal, smallTotal-(smallBooked-currentSmall)); msg != "" {
		fail(c, http.StatusConflict, msg)
		return
	}
	if msg := bagsLeftError("large", large, largeTotal, largeTotal-(largeBooked-currentLarge)); msg != "" {
		fail(c, http.StatusConflict, msg)
		return
	}

	var b Booking
	var status string
	err = tx.QueryRow(c.Request.Context(),
		`UPDATE bookings
		 SET seats = $1, small_bags = $4, large_bags = $5, updated_at = now()
		 WHERE id = $2 AND passenger_id = $3 AND status = 'confirmed'
		 RETURNING id, ride_id, passenger_id, seats, small_bags, large_bags, status, created_at`,
		req.Seats, id, user.ID, small, large,
	).Scan(&b.ID, &b.TripID, &b.PassengerID, &b.Seats, &b.SmallBags, &b.LargeBags, &status, &b.CreatedAt)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	b.Status = status

	if err := tx.Commit(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	c.JSON(http.StatusOK, b)
}

// Cancel sets a confirmed booking's status to 'cancelled', returning seats to the ride.
func (bkg *Bookings) Cancel(c *gin.Context) {
	id := c.Param("id")
	if !isUUID(id) {
		fail(c, http.StatusNotFound, "Booking not found.")
		return
	}
	user := CurrentUser(c)

	tag, err := bkg.DB.Exec(c.Request.Context(),
		`UPDATE bookings
		 SET status = 'cancelled', updated_at = now()
		 WHERE id = $1 AND passenger_id = $2 AND status = 'confirmed'`,
		id, user.ID,
	)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(c, http.StatusNotFound, "Booking not found.")
		return
	}
	c.Status(http.StatusNoContent)
}
