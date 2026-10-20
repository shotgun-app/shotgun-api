package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"shotgun-api/services"
)

func TestGetUser(t *testing.T) {
	e := newTestEnv(t)
	owner, viewer := uniqueEmail(), uniqueEmail()
	e.cleanup(t, owner)
	e.cleanup(t, viewer)

	var body struct{ User struct{ ID string } }
	_ = json.Unmarshal(e.do("POST", "/auth/register", registerBody(owner), nil).Body.Bytes(), &body)
	cookie := sessionCookie(e.do("POST", "/auth/register", registerBody(viewer), nil))

	if rec := e.do("GET", "/api/users/"+body.User.ID, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without cookie = %d, want 401", rec.Code)
	}
	rec := e.do("GET", "/api/users/"+body.User.ID, "", cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), owner) {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("response leaks password data: %s", rec.Body)
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "nope"} {
		if rec := e.do("GET", "/api/users/"+id, "", cookie); rec.Code != http.StatusNotFound {
			t.Errorf("id %s = %d, want 404", id, rec.Code)
		}
	}
}

func TestGetUserPassengerImpact(t *testing.T) {
	e := newTestEnv(t)
	driverID, _ := e.registerUser(t)
	passengerID, cookie := e.registerUser(t)
	ctx := context.Background()

	// Rides and bookings go away with their users, so the registerUser cleanup covers them
	addRide := func(departure time.Time) string {
		var id string
		if err := e.pool.QueryRow(ctx, `
			INSERT INTO rides (driver_id, origin_city, origin_country, destination_city, destination_country,
				departure_at, seats_total, price_per_seat)
			VALUES ($1, 'Gothenburg', 'Sweden', 'Stockholm', 'Sweden', $2, 4, 25)
			RETURNING id
		`, driverID, departure).Scan(&id); err != nil {
			t.Fatalf("insert ride: %v", err)
		}
		return id
	}
	book := func(rideID string, seats int, status string) {
		if _, err := e.pool.Exec(ctx, `
			INSERT INTO bookings (ride_id, passenger_id, seats, status) VALUES ($1, $2, $3, $4)
		`, rideID, passengerID, seats, status); err != nil {
			t.Fatalf("insert booking: %v", err)
		}
	}

	past := time.Now().Add(-2 * time.Hour)
	completed := addRide(past)
	book(completed, 2, "confirmed")
	cancelled := addRide(past)
	book(cancelled, 1, "cancelled")
	upcoming := addRide(time.Now().Add(24 * time.Hour))
	book(upcoming, 1, "confirmed")

	type impact struct {
		Rides      int
		CO2SavedKg float64
	}
	get := func(id string) impact {
		rec := e.do("GET", "/api/users/"+id, "", cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("get %s = %d %s", id, rec.Code, rec.Body)
		}
		var body struct{ PassengerImpact *impact }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.PassengerImpact == nil {
			t.Fatalf("no passengerImpact in %s", rec.Body)
		}
		return *body.PassengerImpact
	}

	// Only the completed confirmed booking counts. The driver has no car, so the baseline applies
	_, _, perSeat, _ := services.CalculateCO2("Gothenburg", "Stockholm", 2, nil)
	want := impact{Rides: 1, CO2SavedKg: math.Round(perSeat*2*100) / 100}
	if got := get(passengerID); got != want || got.CO2SavedKg <= 0 {
		t.Errorf("passenger impact = %+v, want %+v", got, want)
	}
	if got := get(driverID); got != (impact{}) {
		t.Errorf("driver passenger impact = %+v, want zero", got)
	}
}
