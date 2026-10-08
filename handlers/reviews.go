package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reviews groups the review handlers and the DB pool they share.
type Reviews struct {
	DB *pgxpool.Pool
}

// Review is the JSON shape returned to the web app.
type Review struct {
	ID         string    `json:"id"`
	RideID     string    `json:"rideId"`
	ReviewerID string    `json:"reviewerId"`
	RevieweeID string    `json:"revieweeId"`
	Rating     int       `json:"rating"`
	Comment    string    `json:"comment"`
	CreatedAt  time.Time `json:"createdAt"`
}

type reviewRequest struct {
	RideID   string `json:"rideId"`
	TargetID string `json:"targetId"`
	Rating   int    `json:"rating"`
	Comment  string `json:"comment"`
}

func validateReviewRequest(req *reviewRequest) string {
	if req.RideID == "" {
		return "Ride ID is required."
	}
	if !isUUID(req.RideID) {
		return "Ride not found."
	}
	if req.TargetID == "" {
		return "Target ID is required."
	}
	if !isUUID(req.TargetID) {
		return "Target not found."
	}
	if req.Rating < 1 || req.Rating > 5 {
		return "Rating must be between 1 and 5."
	}
	return ""
}

// Create adds a review for the driver of a completed ride.
func (r *Reviews) Create(c *gin.Context) {
	var req reviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}

	if msg := validateReviewRequest(&req); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}

	user := CurrentUser(c)

	if user.ID == req.TargetID {
		fail(c, http.StatusBadRequest, "You cannot review yourself.")
		return
	}

	var departureAt time.Time
	var driverID string
	var myBookingStatus *string
	var targetBookingStatus *string

	err := r.DB.QueryRow(c.Request.Context(), `
		SELECT r.departure_at, r.driver_id,
		       (SELECT status FROM bookings b WHERE b.ride_id = r.id AND b.passenger_id = $1),
		       (SELECT status FROM bookings b WHERE b.ride_id = r.id AND b.passenger_id = $2)
		FROM rides r
		WHERE r.id = $3
	`, user.ID, req.TargetID, req.RideID).Scan(&departureAt, &driverID, &myBookingStatus, &targetBookingStatus)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, http.StatusNotFound, "Ride not found.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	isDriver := driverID == user.ID
	targetIsDriver := driverID == req.TargetID
	isConfirmedPassenger := myBookingStatus != nil && *myBookingStatus == "confirmed"
	targetIsConfirmedPassenger := targetBookingStatus != nil && *targetBookingStatus == "confirmed"

	if !((isDriver && targetIsConfirmedPassenger) || (isConfirmedPassenger && targetIsDriver)) {
		fail(c, http.StatusNotFound, "Ride not found or invalid review target.")
		return
	}

	if !departureAt.Before(time.Now()) {
		fail(c, http.StatusConflict, "You can only review a ride after it has departed.")
		return
	}

	var review Review

	err = r.DB.QueryRow(c.Request.Context(), `
		INSERT INTO reviews (
			ride_id,
			reviewer_id,
			reviewee_id,
			rating,
			comment
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, ride_id, reviewer_id, reviewee_id, rating, comment, created_at
	`,
		req.RideID,
		user.ID,
		req.TargetID,
		req.Rating,
		req.Comment,
	).Scan(
		&review.ID,
		&review.RideID,
		&review.ReviewerID,
		&review.RevieweeID,
		&review.Rating,
		&review.Comment,
		&review.CreatedAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "reviews_once_per_ride_idx" {
			fail(c, http.StatusConflict, "You have already reviewed this ride.")
			return
		}

		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	c.JSON(http.StatusCreated, review)
}
