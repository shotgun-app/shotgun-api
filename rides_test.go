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
