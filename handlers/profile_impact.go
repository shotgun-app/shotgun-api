package handlers

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"

	"shotgun-api/services"
)

// ProfileImpact is the CO2 a user saved in one role (passenger or driver) on completed rides
type ProfileImpact struct {
	Rides             int     `json:"rides"`
	CO2SavedKg        float64 `json:"co2SavedKg"`
	RidesOfferedCount int     `json:"rides_offered_count,omitempty"`
	DriverCO2SavedKg  float64 `json:"driver_co2_saved_kg,omitempty"`
}

// passengerImpact counts the completed rides userID was a confirmed passenger on and the CO2 they saved.
// A ride is completed once it has departed, as in the public impact stats
func passengerImpact(ctx context.Context, db *pgxpool.Pool, userID string) (ProfileImpact, error) {
	rows, err := db.Query(ctx, `
		SELECT
			r.origin_city,
			r.destination_city,
			b.seats,
			COALESCE((SELECT SUM(bk.seats) FROM bookings bk WHERE bk.ride_id = r.id AND bk.status = 'confirmed'), 0)::int,
			c.co2
		FROM bookings b
		JOIN rides r ON r.id = b.ride_id
		JOIN users d ON d.id = r.driver_id
		LEFT JOIN cars c ON c.id = d.car_id
		WHERE b.passenger_id = $1 AND b.status = 'confirmed' AND r.departure_at <= now()
	`, userID)
	if err != nil {
		return ProfileImpact{}, err
	}
	defer rows.Close()

	var impact ProfileImpact
	var saved float64
	for rows.Next() {
		var origin, destination string
		var seats, seatsBooked int
		var carCO2 *int
		if err := rows.Scan(&origin, &destination, &seats, &seatsBooked, &carCO2); err != nil {
			return ProfileImpact{}, err
		}

		impact.Rides++
		// CalculateCO2 gives the saving per seat; each seat booked is one person who did not drive alone
		_, _, perSeat, _ := services.CalculateCO2(origin, destination, seatsBooked, carCO2)
		saved += perSeat * float64(seats)
	}
	if err := rows.Err(); err != nil {
		return ProfileImpact{}, err
	}

	impact.CO2SavedKg = math.Round(saved*100) / 100
	return impact, nil
}

// driverImpact counts the completed rides userID offered as driver and the CO2 they saved.
// A ride is completed once it has departed (departure_at <= now()).
// Rides offered without passengers count as completed rides offered, but save CO2 only if the
// driver's vehicle emits less than the 120 g/km baseline (e.g. electric car). Shared passengers
// save CO2 by avoiding an individual solo drive for each seat booked.
func driverImpact(ctx context.Context, db *pgxpool.Pool, userID string) (ProfileImpact, error) {
	rows, err := db.Query(ctx, `
		SELECT
			r.origin_city,
			r.destination_city,
			COALESCE((SELECT SUM(bk.seats) FROM bookings bk WHERE bk.ride_id = r.id AND bk.status = 'confirmed'), 0)::int,
			c.co2
		FROM rides r
		JOIN users u ON u.id = r.driver_id
		LEFT JOIN cars c ON c.id = u.car_id
		WHERE r.driver_id = $1 AND r.departure_at <= now()
	`, userID)
	if err != nil {
		return ProfileImpact{}, err
	}
	defer rows.Close()

	var impact ProfileImpact
	var saved float64
	for rows.Next() {
		var origin, destination string
		var seatsBooked int
		var carCO2 *int
		if err := rows.Scan(&origin, &destination, &seatsBooked, &carCO2); err != nil {
			return ProfileImpact{}, err
		}

		impact.Rides++

		dist, err := services.GetDistanceKm(origin, destination)
		if err != nil || dist < 0 {
			dist = 0
		}

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

		// Avoided solo car trips for each passenger who shared the ride
		passengerSaved := float64(seatsBooked*dist*services.BaselineCO2PerKm) / 1000.0

		// Additional savings if the vehicle emits less than baseline
		cleanCarSaved := 0.0
		if carEmission < services.BaselineCO2PerKm {
			cleanCarSaved = float64(dist*(services.BaselineCO2PerKm-carEmission)) / 1000.0
		}

		saved += passengerSaved + cleanCarSaved
	}
	if err := rows.Err(); err != nil {
		return ProfileImpact{}, err
	}

	impact.CO2SavedKg = math.Round(saved*100) / 100
	impact.DriverCO2SavedKg = impact.CO2SavedKg
	impact.RidesOfferedCount = impact.Rides
	return impact, nil
}
