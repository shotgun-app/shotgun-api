package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Cars serves the car catalogue that drivers pick from
type Cars struct {
	DB *pgxpool.Pool
}

// Car is the JSON shape returned to the web app. CO2 is in grams per km
type Car struct {
	ID    string `json:"id"`
	Brand string `json:"brand"`
	Model string `json:"model"`
	CO2   int    `json:"co2"`
}

// List returns the whole catalogue, already sorted so the web app can show it as is
func (h *Cars) List(c *gin.Context) {
	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT id, brand, model, co2 FROM cars ORDER BY brand, model`)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	list := make([]Car, 0)
	for rows.Next() {
		var car Car
		if err := rows.Scan(&car.ID, &car.Brand, &car.Model, &car.CO2); err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}
		list = append(list, car)
	}
	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"cars": list})
}

type setCarRequest struct {
	CarID *string `json:"carId"`
}

// SetMine sets the signed-in user's car, or clears it when carId is null, and returns the updated user
func (h *Cars) SetMine(c *gin.Context) {
	var req setCarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if req.CarID != nil && !isUUID(*req.CarID) {
		fail(c, http.StatusBadRequest, "Invalid car id.")
		return
	}

	user, err := scanUser(h.DB.QueryRow(c.Request.Context(),
		`UPDATE users SET car_id = $2, updated_at = now() WHERE id = $1 RETURNING `+userColumns,
		CurrentUser(c).ID, req.CarID))
	if err != nil {
		// The foreign key rejects ids that are not in the catalogue
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			fail(c, http.StatusNotFound, "Car not found.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}
