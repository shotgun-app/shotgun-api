package handlers

import (
	"time"

	"github.com/gin-gonic/gin"
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

// Create adds a review for the driver of a completed ride.
func (r *Reviews) Create(c *gin.Context) {
	// TODO: implement
}