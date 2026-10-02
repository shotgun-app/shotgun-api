package handlers

import (
	"errors"
	"fmt"
	"net/http"
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
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

// BookingWithTrip wraps a booking and its associated ride data.
type BookingWithTrip struct {
	Booking
	Trip Ride `json:"trip"`
}

type bookingRequest struct {
	TripID string `json:"tripId"`
	RideID string `json:"rideId"`
	Seats  int    `json:"seats"`
}

func (r *bookingRequest) getTripID() string {
	if r.TripID != "" {
		return r.TripID
	}
	return r.RideID
}

// ListMine returns confirmed bookings for the authenticated passenger, soonest-departing first.
func (bkg *Bookings) ListMine(c *gin.Context) {
	user := CurrentUser(c)
	rows, err := bkg.DB.Query(c.Request.Context(), `
		SELECT
			b.id, b.ride_id, b.passenger_id, b.seats, b.status, b.created_at,
			r.id, r.driver_id, r.origin_city, r.origin_country, r.destination_city, r.destination_country,
			r.departure_at, r.seats_total,
			COALESCE((SELECT SUM(seats) FROM bookings b2 WHERE b2.ride_id = r.id AND b2.status = 'confirmed'), 0)::int AS seats_booked,
			r.price_per_seat, r.currency, r.notes, r.created_at
		FROM bookings b
		JOIN rides r ON b.ride_id = r.id
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
			&bt.ID, &bt.TripID, &bt.PassengerID, &bt.Seats, &status, &bt.CreatedAt,
			&bt.Trip.ID, &bt.Trip.DriverID,
			&bt.Trip.OriginCity, &bt.Trip.OriginCountry,
			&bt.Trip.DestinationCity, &bt.Trip.DestinationCountry,
			&bt.Trip.DepartureAt,
			&bt.Trip.SeatsTotal, &bt.Trip.SeatsBooked,
			&bt.Trip.PricePerSeat, &bt.Trip.Currency, &bt.Trip.Notes,
			&bt.Trip.CreatedAt,
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
	tripID := req.getTripID()
	if tripID == "" {
		fail(c, http.StatusBadRequest, "Trip ID is required.")
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
	var seatsTotal int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT driver_id, seats_total FROM rides WHERE id = $1 FOR UPDATE`, tripID,
	).Scan(&driverID, &seatsTotal)
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

	// Calculate currently booked seats
	var seatsBooked int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT COALESCE(SUM(seats), 0)::int FROM bookings WHERE ride_id = $1 AND status = 'confirmed'`,
		tripID,
	).Scan(&seatsBooked)
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

	var b Booking
	var status string
	err = tx.QueryRow(c.Request.Context(),
		`INSERT INTO bookings (ride_id, passenger_id, seats, status)
		 VALUES ($1, $2, $3, 'confirmed')
		 RETURNING id, ride_id, passenger_id, seats, status, created_at`,
		tripID, user.ID, req.Seats,
	).Scan(&b.ID, &b.TripID, &b.PassengerID, &b.Seats, &status, &b.CreatedAt)
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

type updateBookingRequest struct {
	Seats int `json:"seats"`
}

// Update changes the number of seats on an existing confirmed booking.
func (bkg *Bookings) Update(c *gin.Context) {
	id := c.Param("id")
	var req updateBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if req.Seats < 1 {
		fail(c, http.StatusBadRequest, "You must book at least 1 seat.")
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
	var currentSeats int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT ride_id, seats FROM bookings WHERE id = $1 AND passenger_id = $2 AND status = 'confirmed' FOR UPDATE`,
		id, user.ID,
	).Scan(&rideID, &currentSeats)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, http.StatusNotFound, "Booking not found.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	// Check capacity on ride
	var seatsTotal int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT seats_total FROM rides WHERE id = $1 FOR UPDATE`, rideID,
	).Scan(&seatsTotal)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	var seatsBooked int
	err = tx.QueryRow(c.Request.Context(),
		`SELECT COALESCE(SUM(seats), 0)::int FROM bookings WHERE ride_id = $1 AND status = 'confirmed'`,
		rideID,
	).Scan(&seatsBooked)
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

	var b Booking
	var status string
	err = tx.QueryRow(c.Request.Context(),
		`UPDATE bookings
		 SET seats = $1, updated_at = now()
		 WHERE id = $2 AND passenger_id = $3 AND status = 'confirmed'
		 RETURNING id, ride_id, passenger_id, seats, status, created_at`,
		req.Seats, id, user.ID,
	).Scan(&b.ID, &b.TripID, &b.PassengerID, &b.Seats, &status, &b.CreatedAt)
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
