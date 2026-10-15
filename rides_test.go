package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// registerUser signs up a fresh user and returns their id and session cookie
func (e *testEnv) registerUser(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	email := uniqueEmail()
	e.cleanup(t, email)
	rec := e.do("POST", "/auth/register", registerBody(email), nil)
	var body struct{ User struct{ ID string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.User.ID == "" {
		t.Fatalf("register = %d %s", rec.Code, rec.Body)
	}
	return body.User.ID, sessionCookie(rec)
}

func TestRidePassengers(t *testing.T) {
	e := newTestEnv(t)
	driverID, driver := e.registerUser(t)
	stayingID, staying := e.registerUser(t)
	_, leaving := e.registerUser(t)

	rec := e.do("POST", "/api/rides", `{"originCity":"Ljubljana","originCountry":"Slovenia",
		"destinationCity":"Zagreb","destinationCountry":"Croatia","departureAt":"2099-01-01T09:00:00Z","seatsTotal":3}`, driver)
	var created struct{ Ride struct{ ID string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create ride = %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"passengers":[]`) {
		t.Errorf("new ride should have no passengers: %s", rec.Body)
	}

	book := fmt.Sprintf(`{"tripId":%q,"seats":1}`, created.Ride.ID)
	e.do("POST", "/api/bookings", book, staying)
	var cancelled struct{ ID string }
	_ = json.Unmarshal(e.do("POST", "/api/bookings", book, leaving).Body.Bytes(), &cancelled)
	e.do("DELETE", "/api/bookings/"+cancelled.ID, "", leaving)

	var mine struct {
		Rides []struct{ Passengers []struct{ ID string } }
	}
	_ = json.Unmarshal(e.do("GET", "/api/rides/mine", "", driver).Body.Bytes(), &mine)
	if len(mine.Rides) != 1 || len(mine.Rides[0].Passengers) != 1 || mine.Rides[0].Passengers[0].ID != stayingID {
		t.Errorf("rides/mine = %+v, want one ride with passenger %s", mine.Rides, stayingID)
	}

	var booked []struct {
		Trip struct {
			Driver     struct{ ID string }
			Passengers []struct{ ID string }
		}
	}
	_ = json.Unmarshal(e.do("GET", "/api/bookings/mine", "", staying).Body.Bytes(), &booked)
	if len(booked) != 1 || booked[0].Trip.Driver.ID != driverID || len(booked[0].Trip.Passengers) != 1 {
		t.Errorf("bookings/mine = %+v, want driver %s and one passenger", booked, driverID)
	}

	if rec := e.do("GET", "/api/trips?origin=Ljubljana", "", leaving); strings.Contains(rec.Body.String(), "passengers") {
		t.Errorf("search exposes passengers: %s", rec.Body)
	}
}

func TestRideCO2SearchAndDetail(t *testing.T) {
	e := newTestEnv(t)
	_, driver := e.registerUser(t)
	_, passenger := e.registerUser(t)

	// Fetch cars to find an electric car (CO2 = 0)
	var carsRes struct {
		Cars []struct {
			ID  string
			CO2 int
		}
	}
	_ = json.Unmarshal(e.do("GET", "/api/cars", "", driver).Body.Bytes(), &carsRes)
	var electricCarID string
	for _, car := range carsRes.Cars {
		if car.CO2 == 0 {
			electricCarID = car.ID
			break
		}
	}
	if electricCarID != "" {
		e.do("PUT", "/auth/me/car", fmt.Sprintf(`{"carId":%q}`, electricCarID), driver)
	}

	// Create a ride from Stockholm to Munich (1420 km)
	rec := e.do("POST", "/api/rides", `{"originCity":"Stockholm","originCountry":"Sweden",
		"destinationCity":"Munich","destinationCountry":"Germany","departureAt":"2099-01-01T09:00:00Z","seatsTotal":3}`, driver)
	var created struct {
		Ride struct {
			ID         string
			DistanceKm int     `json:"distance_km"`
			IsElectric bool    `json:"is_electric"`
			Consumed   float64 `json:"co2_consumed_kg"`
			Saved      float64 `json:"co2_saved_kg"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create ride = %d %s", rec.Code, rec.Body)
	}
	if created.Ride.DistanceKm != 1420 {
		t.Errorf("created.Ride.DistanceKm = %d, want 1420", created.Ride.DistanceKm)
	}

	// 1. Search GET /api/trips
	searchRec := e.do("GET", "/api/trips?origin=Stockholm&destination=Munich", "", passenger)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search = %d %s", searchRec.Code, searchRec.Body)
	}
	var results []struct {
		ID            string
		DistanceKm    int     `json:"distance_km"`
		DistanceCamel int     `json:"distanceKm"`
		CO2Consumed   float64 `json:"co2_consumed_kg"`
		CO2Saved      float64 `json:"co2_saved_kg"`
		IsElectric    bool    `json:"is_electric"`
	}
	if err := json.Unmarshal(searchRec.Body.Bytes(), &results); err != nil || len(results) == 0 {
		t.Fatalf("search decode error: %v, body=%s", err, searchRec.Body)
	}
	r0 := results[0]
	if r0.DistanceKm != 1420 || r0.DistanceCamel != 1420 {
		t.Errorf("distance = %d / %d, want 1420", r0.DistanceKm, r0.DistanceCamel)
	}
	if electricCarID != "" {
		if !r0.IsElectric {
			t.Errorf("expected electric car, got %v", r0.IsElectric)
		}
		if r0.CO2Consumed != 0.0 {
			t.Errorf("expected 0 consumed, got %f", r0.CO2Consumed)
		}
		if r0.CO2Saved <= 0 {
			t.Errorf("expected positive savings, got %f", r0.CO2Saved)
		}
	}

	// 2. Detail GET /api/trips/:id
	detailRec := e.do("GET", "/api/trips/"+created.Ride.ID, "", passenger)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", detailRec.Code, detailRec.Body)
	}
	var detail struct {
		Trip struct {
			ID          string
			DistanceKm  int     `json:"distance_km"`
			CO2Consumed float64 `json:"co2_consumed_kg"`
			CO2Saved    float64 `json:"co2_saved_kg"`
			IsElectric  bool    `json:"is_electric"`
		}
	}
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil || detail.Trip.ID != created.Ride.ID {
		t.Fatalf("detail decode error: %v, body=%s", err, detailRec.Body)
	}
	if detail.Trip.DistanceKm != 1420 {
		t.Errorf("detail distance = %d, want 1420", detail.Trip.DistanceKm)
	}
}
