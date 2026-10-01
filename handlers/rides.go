package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rides groups the ride handlers and the DB pool they share.
type Rides struct {
	DB *pgxpool.Pool
}

// Ride is the JSON shape returned to the web app.
// Column names mirror migrations/000001_init_schema.up.sql exactly.
type Ride struct {
	ID                 string    `json:"id"`
	DriverID           string    `json:"driverId"`
	OriginCity         string    `json:"originCity"`
	OriginCountry      string    `json:"originCountry"`
	DestinationCity    string    `json:"destinationCity"`
	DestinationCountry string    `json:"destinationCountry"`
	DepartureAt        time.Time `json:"departureAt"`
	SeatsTotal         int       `json:"seatsTotal"`
	SeatsBooked        int       `json:"seatsBooked"`
	PricePerSeat       float64   `json:"pricePerSeat"`
	Currency           string    `json:"currency"`
	Notes              string    `json:"notes"`
	CreatedAt          time.Time `json:"createdAt"`
}

// DriverPublic is the public profile of a driver attached to a trip.
type DriverPublic struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Phone       *string   `json:"phone"`
	JoinedAt    time.Time `json:"joinedAt"`
	Rating      float64   `json:"rating"`
	RatingCount int       `json:"ratingCount"`
	CO2SavedKg  float64   `json:"co2SavedKg"`
}

// TripWithDriver represents a ride along with the driver profile.
type TripWithDriver struct {
	Ride
	Driver DriverPublic `json:"driver"`
}

const rideColumns = `id, driver_id, origin_city, origin_country, destination_city, destination_country,
	departure_at, seats_total,
	COALESCE((SELECT SUM(seats) FROM bookings WHERE ride_id = rides.id AND status = 'confirmed'), 0)::int AS seats_booked,
	price_per_seat, currency, notes, created_at`

func scanRide(row pgx.Row) (*Ride, error) {
	var r Ride
	err := row.Scan(
		&r.ID, &r.DriverID,
		&r.OriginCity, &r.OriginCountry,
		&r.DestinationCity, &r.DestinationCountry,
		&r.DepartureAt,
		&r.SeatsTotal, &r.SeatsBooked,
		&r.PricePerSeat, &r.Currency, &r.Notes,
		&r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListMine returns the rides driven by the current user, soonest first.
func (ri *Rides) ListMine(c *gin.Context) {
	user := CurrentUser(c)
	rows, err := ri.DB.Query(c.Request.Context(),
		`SELECT `+rideColumns+` FROM rides WHERE driver_id = $1 ORDER BY departure_at ASC`, user.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	list := make([]*Ride, 0)
	for rows.Next() {
		r, err := scanRide(rows)
		if err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"rides": list})
}

// Search returns rides matching search criteria, ordered by departure_at ASC.
func (ri *Rides) Search(c *gin.Context) {
	origin := strings.TrimSpace(c.Query("origin"))
	dest := strings.TrimSpace(c.Query("destination"))
	date := strings.TrimSpace(c.Query("date"))
	timeParam := strings.TrimSpace(c.Query("time"))

	query := `
		SELECT
			r.id, r.driver_id, r.origin_city, r.origin_country, r.destination_city, r.destination_country,
			r.departure_at, r.seats_total,
			COALESCE((SELECT SUM(seats) FROM bookings WHERE ride_id = r.id AND status = 'confirmed'), 0)::int AS seats_booked,
			r.price_per_seat, r.currency, r.notes, r.created_at,
			u.id, u.name, u.email, u.phone, u.created_at
		FROM rides r
		JOIN users u ON r.driver_id = u.id
		WHERE 1=1
	`
	var args []any
	argIdx := 1

	if origin != "" {
		query += fmt.Sprintf(" AND LOWER(r.origin_city) = LOWER($%d)", argIdx)
		args = append(args, origin)
		argIdx++
	}
	if dest != "" {
		query += fmt.Sprintf(" AND LOWER(r.destination_city) = LOWER($%d)", argIdx)
		args = append(args, dest)
		argIdx++
	}
	if date != "" {
		query += fmt.Sprintf(" AND r.departure_at::date = $%d::date", argIdx)
		args = append(args, date)
		argIdx++
	}
	if timeParam != "" {
		query += fmt.Sprintf(" AND TO_CHAR(r.departure_at, 'HH24:MI') = $%d", argIdx)
		args = append(args, timeParam)
		argIdx++
	}

	query += ` ORDER BY r.departure_at ASC`

	rows, err := ri.DB.Query(c.Request.Context(), query, args...)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	list := make([]*TripWithDriver, 0)
	for rows.Next() {
		var t TripWithDriver
		err := rows.Scan(
			&t.ID, &t.DriverID,
			&t.OriginCity, &t.OriginCountry,
			&t.DestinationCity, &t.DestinationCountry,
			&t.DepartureAt,
			&t.SeatsTotal, &t.SeatsBooked,
			&t.PricePerSeat, &t.Currency, &t.Notes,
			&t.CreatedAt,
			&t.Driver.ID, &t.Driver.Name, &t.Driver.Email, &t.Driver.Phone, &t.Driver.JoinedAt,
		)
		if err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}
		t.Driver.Rating = 5.0
		t.Driver.RatingCount = 1
		t.Driver.CO2SavedKg = 0.0
		list = append(list, &t)
	}
	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, list)
}

type rideRequest struct {
	OriginCity         string  `json:"originCity"`
	OriginCountry      string  `json:"originCountry"`
	DestinationCity    string  `json:"destinationCity"`
	DestinationCountry string  `json:"destinationCountry"`
	DepartureAt        string  `json:"departureAt"` // RFC 3339 / ISO 8601
	SeatsTotal         int     `json:"seatsTotal"`
	PricePerSeat       float64 `json:"pricePerSeat"`
	Currency           string  `json:"currency"`
	Notes              string  `json:"notes"`
}

func validateRideRequest(req *rideRequest) string {
	if strings.TrimSpace(req.OriginCity) == "" || strings.TrimSpace(req.OriginCountry) == "" {
		return "Origin city and country are required."
	}
	if strings.TrimSpace(req.DestinationCity) == "" || strings.TrimSpace(req.DestinationCountry) == "" {
		return "Destination city and country are required."
	}
	if req.DepartureAt == "" {
		return "Departure date and time are required."
	}
	if req.SeatsTotal < 1 {
		return "Free seats must be at least 1."
	}
	if req.SeatsTotal > 15 {
		return "Free seats cannot exceed 15."
	}
	if req.PricePerSeat < 0 {
		return "Price per seat cannot be negative."
	}
	return ""
}

// Create inserts a new ride; the driver is always the authenticated user.
func (ri *Rides) Create(c *gin.Context) {
	var req rideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if msg := validateRideRequest(&req); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}

	// Normalise optional fields
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "EUR"
	}
	notes := strings.TrimSpace(req.Notes)

	user := CurrentUser(c)
	ride, err := scanRide(ri.DB.QueryRow(c.Request.Context(),
		`INSERT INTO rides
		   (driver_id, origin_city, origin_country, destination_city, destination_country,
		    departure_at, seats_total, price_per_seat, currency, notes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING `+rideColumns,
		user.ID,
		strings.TrimSpace(req.OriginCity), strings.TrimSpace(req.OriginCountry),
		strings.TrimSpace(req.DestinationCity), strings.TrimSpace(req.DestinationCountry),
		req.DepartureAt,
		req.SeatsTotal, req.PricePerSeat, currency, notes,
	))
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ride": ride})
}

// Update edits a ride the authenticated user owns.
func (ri *Rides) Update(c *gin.Context) {
	id := c.Param("id")
	var req rideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if msg := validateRideRequest(&req); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}

	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "EUR"
	}
	notes := strings.TrimSpace(req.Notes)

	user := CurrentUser(c)

	var seatsBooked int
	err := ri.DB.QueryRow(c.Request.Context(),
		`SELECT COALESCE(SUM(seats), 0)::int FROM bookings WHERE ride_id = $1 AND status = 'confirmed'`,
		id,
	).Scan(&seatsBooked)
	if err == nil && req.SeatsTotal < seatsBooked {
		fail(c, http.StatusBadRequest, fmt.Sprintf("Total seats cannot be fewer than already booked seats (%d).", seatsBooked))
		return
	}

	ride, err := scanRide(ri.DB.QueryRow(c.Request.Context(),
		`UPDATE rides SET
		   origin_city = $3, origin_country = $4,
		   destination_city = $5, destination_country = $6,
		   departure_at = $7, seats_total = $8,
		   price_per_seat = $9, currency = $10, notes = $11,
		   updated_at = now()
		 WHERE id = $1 AND driver_id = $2
		 RETURNING `+rideColumns,
		id, user.ID,
		strings.TrimSpace(req.OriginCity), strings.TrimSpace(req.OriginCountry),
		strings.TrimSpace(req.DestinationCity), strings.TrimSpace(req.DestinationCountry),
		req.DepartureAt,
		req.SeatsTotal, req.PricePerSeat, currency, notes,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either the ride doesn't exist or it belongs to a different driver
			fail(c, http.StatusNotFound, "Ride not found.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ride": ride})
}

// Delete removes a ride the authenticated user owns.
// Bookings cascade-delete via the FK constraint.
func (ri *Rides) Delete(c *gin.Context) {
	id := c.Param("id")
	user := CurrentUser(c)

	tag, err := ri.DB.Exec(c.Request.Context(),
		`DELETE FROM rides WHERE id = $1 AND driver_id = $2`, id, user.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(c, http.StatusNotFound, "Ride not found.")
		return
	}
	c.Status(http.StatusNoContent)
}
