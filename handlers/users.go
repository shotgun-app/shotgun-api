package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Users serves public profiles to signed-in users
type Users struct {
	DB *pgxpool.Pool
}

type ProfileReview struct {
	ID         string    `json:"id"`
	RideID     string    `json:"rideId"`
	ReviewerID string    `json:"reviewerId"`
	Rating     int       `json:"rating"`
	Comment    string    `json:"comment"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (u *Users) Get(c *gin.Context) {
	id := c.Param("id")
	if !isUUID(id) {
		fail(c, http.StatusNotFound, "User not found.")
		return
	}

	user, err := scanUser(u.DB.QueryRow(c.Request.Context(),
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, http.StatusNotFound, "User not found.")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	rows, err := u.DB.Query(c.Request.Context(), `
		SELECT r.id, r.ride_id, r.reviewer_id, r.rating, r.comment, r.created_at, rd.driver_id
		FROM reviews r
		JOIN rides rd ON rd.id = r.ride_id
		WHERE r.reviewee_id = $1
		ORDER BY r.created_at DESC
	`, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	driverReviews := make([]ProfileReview, 0)
	passengerReviews := make([]ProfileReview, 0)
	var driverSum, passengerSum int
	var driverCount, passengerCount int

	for rows.Next() {
		var review ProfileReview
		var driverID string
		if err := rows.Scan(
			&review.ID,
			&review.RideID,
			&review.ReviewerID,
			&review.Rating,
			&review.Comment,
			&review.CreatedAt,
			&driverID,
		); err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}

		if driverID == id {
			driverReviews = append(driverReviews, review)
			driverSum += review.Rating
			driverCount++
		} else {
			passengerReviews = append(passengerReviews, review)
			passengerSum += review.Rating
			passengerCount++
		}
	}

	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	var driverScore, passengerScore float64
	if driverCount > 0 {
		driverScore = float64(driverSum) / float64(driverCount)
	}
	if passengerCount > 0 {
		passengerScore = float64(passengerSum) / float64(passengerCount)
	}

	c.JSON(http.StatusOK, gin.H{
		"user":             user,
		"driverScore":      driverScore,
		"passengerScore":   passengerScore,
		"reviews":          driverReviews,
		"passengerReviews": passengerReviews,
	})
}
