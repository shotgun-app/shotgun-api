package main

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestGetUserDriverImpact(t *testing.T) {
	e := newTestEnv(t)
	driverID, driverCookie := e.registerUser(t)
	passengerID, passengerCookie := e.registerUser(t)
	ctx := context.Background()

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

	type driverResponse struct {
		DriverImpact *struct {
			Rides             int     `json:"rides"`
			CO2SavedKg        float64 `json:"co2SavedKg"`
			DriverCO2SavedKg  float64 `json:"driver_co2_saved_kg"`
			RidesOfferedCount int     `json:"rides_offered_count"`
		} `json:"driverImpact"`
		DriverCO2SavedKg  float64 `json:"driver_co2_saved_kg"`
		RidesOfferedCount int     `json:"rides_offered_count"`
		CarsReducedCount  int     `json:"cars_reduced_count"`
	}

	get := func(id string, cookie *http.Cookie) driverResponse {
		rec := e.do("GET", "/api/users/"+id, "", cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("get %s = %d %s", id, rec.Code, rec.Body)
		}
		var body driverResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.DriverImpact == nil {
			t.Fatalf("no driverImpact in %s", rec.Body)
		}
		return body
	}

	// 1. Initial edge case: Driver with 0 rides
	initDriver := get(driverID, driverCookie)
	if initDriver.DriverImpact.Rides != 0 || initDriver.DriverImpact.CO2SavedKg != 0 ||
		initDriver.RidesOfferedCount != 0 || initDriver.DriverCO2SavedKg != 0 || initDriver.CarsReducedCount != 0 {
		t.Errorf("initial driver impact = %+v, want all zeroes", initDriver)
	}

	// 2. Rides in the past and future
	past := time.Now().Add(-2 * time.Hour)
	completed := addRide(past)
	book(completed, 2, "confirmed")
	cancelled := addRide(past)
	book(cancelled, 1, "cancelled")
	upcoming := addRide(time.Now().Add(24 * time.Hour))
	book(upcoming, 1, "confirmed")

	// Completed ride: Gothenburg -> Stockholm = 470 km
	// 2 confirmed seats, baseline car (nil) = 120 g/km
	// passengerSaved = 2 * 470 * 120 / 1000 = 112.8 kg
	// cleanCarSaved = 0
	// Completed ride without confirmed passengers: cancelled booking, 0 seats booked, 0 saved
	// Total completed rides offered = 2 (upcoming is not completed)
	wantCO2 := 112.8
	wantRides := 2

	got := get(driverID, driverCookie)
	if got.DriverImpact.Rides != wantRides || got.DriverImpact.CO2SavedKg != wantCO2 {
		t.Errorf("driver impact = %+v, want rides=%d co2=%f", got.DriverImpact, wantRides, wantCO2)
	}
	if got.RidesOfferedCount != wantRides || got.DriverCO2SavedKg != wantCO2 || got.CarsReducedCount != wantRides {
		t.Errorf("top-level fields = rides=%d co2=%f cars=%d, want %d and %f",
			got.RidesOfferedCount, got.DriverCO2SavedKg, got.CarsReducedCount, wantRides, wantCO2)
	}

	// 3. Passenger user has 0 driver impact
	gotPassenger := get(passengerID, passengerCookie)
	if gotPassenger.DriverImpact.Rides != 0 || gotPassenger.DriverImpact.CO2SavedKg != 0 ||
		gotPassenger.RidesOfferedCount != 0 || gotPassenger.DriverCO2SavedKg != 0 {
		t.Errorf("passenger driver impact = %+v, want all zeroes", gotPassenger)
	}

	// 4. Test electric car saving for driver
	var carsRes struct {
		Cars []struct {
			ID  string
			CO2 int
		}
	}
	_ = json.Unmarshal(e.do("GET", "/api/cars", "", driverCookie).Body.Bytes(), &carsRes)
	var electricCarID string
	for _, car := range carsRes.Cars {
		if car.CO2 == 0 {
			electricCarID = car.ID
			break
		}
	}
	if electricCarID != "" {
		e.do("PUT", "/auth/me/car", fmt.Sprintf(`{"carId":%q}`, electricCarID), driverCookie)

		// With electric car, completed ride (470 km, 2 passengers):
		// passengerSaved = 112.8 kg
		// cleanCarSaved for completed ride = 470 * 120 / 1000 = 56.4 kg
		// cleanCarSaved for cancelled ride (0 passengers, but electric car completed 470 km) = 56.4 kg
		// Total CO2 saved = 112.8 + 56.4 + 56.4 = 225.6 kg
		gotElec := get(driverID, driverCookie)
		wantElecCO2 := 225.6
		if gotElec.DriverImpact.CO2SavedKg != wantElecCO2 || gotElec.DriverCO2SavedKg != wantElecCO2 {
			t.Errorf("driver impact with electric car = %f, want %f", gotElec.DriverImpact.CO2SavedKg, wantElecCO2)
		}
	}
}
