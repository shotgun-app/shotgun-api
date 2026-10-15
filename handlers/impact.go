package handlers

import (
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shotgun-api/services"
)

// Impact groups the sustainability impact handlers and the DB pool they share.
type Impact struct {
	DB *pgxpool.Pool
}

// SustainabilityStats holds live environmental impact statistics for completed rides.
type SustainabilityStats struct {
	// 3 Primary Highlights (in kilograms of CO2)
	CO2ConsumedKg              float64 `json:"co2ConsumedKg"`
	CO2WouldHaveBeenConsumedKg float64 `json:"co2WouldHaveBeenConsumedKg"`
	CO2SavedKg                 float64 `json:"co2SavedKg"`

	// Completed rides and passenger counts
	TotalCompletedRides   int `json:"totalCompletedRides"`
	TotalSharedRides      int `json:"totalSharedRides"`
	TotalPassengers       int `json:"totalPassengers"`
	IndividualRidesSaved  int `json:"individualRidesSaved"`
	DriversWithoutShotgun int `json:"driversWithoutShotgun"`
	TotalDistanceKm       int `json:"totalDistanceKm"`
}

// Get returns the dynamic sustainability impact of all completed rides.
// This endpoint is unauthenticated and publicly accessible.
func (h *Impact) Get(c *gin.Context) {
	// Router tests run without a database; an empty platform reports all zeros.
	if h.DB == nil {
		c.JSON(http.StatusOK, SustainabilityStats{})
		return
	}

	query := `
		SELECT
			r.origin_city,
			r.destination_city,
			COALESCE((SELECT SUM(bk.seats) FROM bookings bk WHERE bk.ride_id = r.id AND bk.status = 'confirmed'), 0)::int AS seats_booked,
			c.co2
		FROM rides r
		JOIN users u ON r.driver_id = u.id
		LEFT JOIN cars c ON u.car_id = c.id
		WHERE r.departure_at <= NOW()
	`

	rows, err := h.DB.Query(c.Request.Context(), query)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	var totalCompletedRides int
	var totalSharedRides int
	var totalPassengers int
	var totalDistanceKm int
	var totalCO2Consumed float64
	var totalCO2Saved float64

	for rows.Next() {
		var originCity, destCity string
		var seatsBooked int
		var carCO2 *int

		if err := rows.Scan(&originCity, &destCity, &seatsBooked, &carCO2); err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}

		totalCompletedRides++
		if seatsBooked > 0 {
			totalSharedRides++
		}
		totalPassengers += seatsBooked

		dist, err := services.GetDistanceKm(originCity, destCity)
		if err != nil || dist < 0 {
			dist = 0
		}
		totalDistanceKm += dist

		var carEmission int
		if carCO2 != nil {
			if *carCO2 == 0 {
				carEmission = 0
			} else {
				carEmission = *carCO2
			}
		} else {
			carEmission = services.BaselineCO2PerKm
		}

		// Actual CO2 emitted by the vehicle for this completed ride
		co2Consumed := float64(dist*carEmission) / 1000.0
		totalCO2Consumed += co2Consumed

		// If Shotgun didn't exist, each passenger would have driven an average car (120 g/km)
		passengerSaved := float64(seatsBooked*dist*services.BaselineCO2PerKm) / 1000.0

		// Additional savings if the vehicle emits less than baseline (e.g. electric car)
		cleanCarSaved := 0.0
		if carEmission < services.BaselineCO2PerKm {
			cleanCarSaved = float64(dist*(services.BaselineCO2PerKm-carEmission)) / 1000.0
		}

		totalCO2Saved += passengerSaved + cleanCarSaved
	}

	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	totalCO2Consumed = math.Round(totalCO2Consumed*100) / 100
	totalCO2Saved = math.Round(totalCO2Saved*100) / 100
	totalCO2Would := math.Round((totalCO2Consumed+totalCO2Saved)*100) / 100

	individualRidesSaved := totalPassengers
	driversWithoutShotgun := totalCompletedRides + totalPassengers

	c.JSON(http.StatusOK, SustainabilityStats{
		CO2ConsumedKg:              totalCO2Consumed,
		CO2WouldHaveBeenConsumedKg: totalCO2Would,
		CO2SavedKg:                 totalCO2Saved,

		TotalCompletedRides:   totalCompletedRides,
		TotalSharedRides:      totalSharedRides,
		TotalPassengers:       totalPassengers,
		IndividualRidesSaved:  individualRidesSaved,
		DriversWithoutShotgun: driversWithoutShotgun,
		TotalDistanceKm:       totalDistanceKm,
	})
}
