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

	var driverScore float64
	err = u.DB.QueryRow(c.Request.Context(), `
		SELECT COALESCE(AVG(rating), 0)
		FROM reviews
		WHERE reviewee_id = $1
	`, id).Scan(&driverScore)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	rows, err := u.DB.Query(c.Request.Context(), `
		SELECT id, ride_id, reviewer_id, rating, comment, created_at
		FROM reviews
		WHERE reviewee_id = $1
		ORDER BY created_at DESC
	`, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	reviews := make([]ProfileReview, 0)
	for rows.Next() {
		var review ProfileReview
		if err := rows.Scan(
			&review.ID,
			&review.RideID,
			&review.ReviewerID,
			&review.Rating,
			&review.Comment,
			&review.CreatedAt,
		); err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}
		reviews = append(reviews, review)
	}

	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user":        user,
		"driverScore": driverScore,
		"reviews":     reviews,
	})
}
