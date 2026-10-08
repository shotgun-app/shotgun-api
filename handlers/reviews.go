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
	RideID  string `json:"rideId"`
	Rating  int    `json:"rating"`
	Comment string `json:"comment"`
}

func validateReviewRequest(req *reviewRequest) string {
	if req.RideID == "" {
		return "Ride ID is required."
	}
	if !isUUID(req.RideID) {
		return "Ride not found."
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

	var driverID string
	var departureAt time.Time

	err := r.DB.QueryRow(c.Request.Context(), `
		SELECT r.driver_id, r.departure_at
		FROM rides r
		JOIN bookings b ON b.ride_id = r.id
		WHERE r.id = $1
		  AND b.passenger_id = $2
		  AND b.status = 'confirmed'
	`, req.RideID, user.ID).Scan(&driverID, &departureAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, http.StatusNotFound, "Ride not found or you are not a passenger on this ride.")
			return
		}

		fail(c, http.StatusInternalServerError, "Something went wrong.")
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
		driverID,
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
