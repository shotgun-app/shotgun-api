package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shotgun-api/config"
	"shotgun-api/handlers"
)

func TestImpactUnauthenticatedWithoutDB(t *testing.T) {
	router := setupRouter(&config.Config{}, nil)

	for _, path := range []string{"/api/impact", "/api/sustainability-impact", "/impact"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
			}

			var body handlers.SustainabilityStats
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal = %v", err)
			}

			if body.TotalCompletedRides != 0 {
				t.Errorf("TotalCompletedRides = %d, want 0", body.TotalCompletedRides)
			}
		})
	}
}

func TestImpactCalculationIntegration(t *testing.T) {
	e := newTestEnv(t)
	driverID, _ := e.registerUser(t)
	passengerID, _ := e.registerUser(t)

	// Create a completed ride in the past (e.g. 2 hours ago)
	pastTime := time.Now().Add(-2 * time.Hour)
	var rideID string
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO rides (
			driver_id, origin_city, origin_country, destination_city, destination_country,
			departure_at, seats_total, price_per_seat, currency, notes
		) VALUES ($1, 'Gothenburg', 'Sweden', 'Stockholm', 'Sweden', $2, 4, 25.0, 'EUR', 'Test ride')
		RETURNING id
	`, driverID, pastTime).Scan(&rideID)
	if err != nil {
		t.Fatalf("insert completed ride: %v", err)
	}
	defer e.pool.Exec(context.Background(), "DELETE FROM rides WHERE id = $1", rideID)

	// Add confirmed booking with 2 seats
	var bookingID string
	err = e.pool.QueryRow(context.Background(), `
		INSERT INTO bookings (ride_id, passenger_id, seats, status)
		VALUES ($1, $2, 2, 'confirmed')
		RETURNING id
	`, rideID, passengerID).Scan(&bookingID)
	if err != nil {
		t.Fatalf("insert booking: %v", err)
	}

	// Fetch impact anonymously (no auth cookie)
	rec := e.do(http.MethodGet, "/api/impact", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var stats handlers.SustainabilityStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if stats.TotalCompletedRides < 1 {
		t.Errorf("expected at least 1 completed ride, got %d", stats.TotalCompletedRides)
	}
	if stats.TotalPassengers < 2 {
		t.Errorf("expected at least 2 passengers, got %d", stats.TotalPassengers)
	}
	if stats.IndividualRidesSaved < 2 {
		t.Errorf("expected at least 2 individual rides saved, got %d", stats.IndividualRidesSaved)
	}
	if stats.CO2SavedKg <= 0 {
		t.Errorf("expected CO2SavedKg > 0, got %f", stats.CO2SavedKg)
	}
	if stats.CO2WouldHaveBeenConsumedKg <= stats.CO2ConsumedKg {
		t.Errorf("expected CO2WouldHaveBeenConsumedKg (%f) > CO2ConsumedKg (%f)",
			stats.CO2WouldHaveBeenConsumedKg, stats.CO2ConsumedKg)
	}

}
