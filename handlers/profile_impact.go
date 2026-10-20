package handlers

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"

	"shotgun-api/services"
)

// ProfileImpact is the CO2 a user saved in one role (passenger or driver) on completed rides
type ProfileImpact struct {
	Rides      int     `json:"rides"`
	CO2SavedKg float64 `json:"co2SavedKg"`
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

// TODO(driver impact): add driverImpact(ctx, db, userID) (ProfileImpact, error) next to passengerImpact.
//   - Rides: completed rides (departure_at <= now()) where rides.driver_id = userID
//   - CO2SavedKg: CO2 saved by sharing those rides. handlers/impact.go has the platform-wide formula
//     (passengers * distance * BaselineCO2PerKm, plus the clean car saving), apply it per driver
//   - Decide if rides without passengers count, and note it in the comment
// Then return it from Users.Get as "driverImpact" and add a test to users_test.go like TestGetUserPassengerImpact
