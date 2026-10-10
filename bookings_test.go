package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// createRide posts a ride with the given extra JSON fields and returns its id
func (e *testEnv) createRide(t *testing.T, driver *http.Cookie, extra string) string {
	t.Helper()
	rec := e.do("POST", "/api/rides", `{"originCity":"Ljubljana","originCountry":"Slovenia",
		"destinationCity":"Zagreb","destinationCountry":"Croatia","departureAt":"2099-01-01T09:00:00Z",`+extra+`}`, driver)
	var created struct{ Ride struct{ ID string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create ride = %d %s", rec.Code, rec.Body)
	}
	return created.Ride.ID
}

func TestBaggageCapacity(t *testing.T) {
	e := newTestEnv(t)
	_, driver := e.registerUser(t)
	_, first := e.registerUser(t)
	_, second := e.registerUser(t)

	rideID := e.createRide(t, driver, `"seatsTotal":4,"smallBagsTotal":2,"largeBagsTotal":1`)
	book := func(small, large int) string {
		return fmt.Sprintf(`{"tripId":%q,"seats":1,"smallBags":%d,"largeBags":%d}`, rideID, small, large)
	}

	rec := e.do("POST", "/api/bookings", book(1, 1), first)
	var b struct {
		ID                   string
		SmallBags, LargeBags int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || rec.Code != http.StatusCreated || b.SmallBags != 1 || b.LargeBags != 1 {
		t.Fatalf("book within capacity = %d %s", rec.Code, rec.Body)
	}

	// Large bags are used up, small ones are not: each size is checked on its own
	rec = e.do("POST", "/api/bookings", book(0, 1), second)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "No large bag space left") {
		t.Errorf("large over capacity = %d %s, want 409 no large space", rec.Code, rec.Body)
	}
	rec = e.do("POST", "/api/bookings", book(2, 0), second)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Only 1 small bag left") {
		t.Errorf("small over capacity = %d %s, want 409 only 1 left", rec.Code, rec.Body)
	}

	var trips []struct{ SmallBagsTotal, SmallBagsBooked, LargeBagsTotal, LargeBagsBooked int }
	_ = json.Unmarshal(e.do("GET", "/api/trips?origin=Ljubljana", "", second).Body.Bytes(), &trips)
	if len(trips) == 0 || trips[0].SmallBagsBooked != 1 || trips[0].LargeBagsBooked != 1 || trips[0].SmallBagsTotal != 2 || trips[0].LargeBagsTotal != 1 {
		t.Errorf("search bags = %+v, want 1/2 small and 1/1 large", trips)
	}

	// Cancelling frees the bags
	e.do("DELETE", "/api/bookings/"+b.ID, "", first)
	if rec := e.do("POST", "/api/bookings", book(2, 1), second); rec.Code != http.StatusCreated {
		t.Errorf("book after cancel = %d %s, want 201", rec.Code, rec.Body)
	}
}

func TestBaggageDefaultsToNone(t *testing.T) {
	e := newTestEnv(t)
	_, driver := e.registerUser(t)
	_, passenger := e.registerUser(t)

	rideID := e.createRide(t, driver, `"seatsTotal":2`)

	if rec := e.do("POST", "/api/bookings", fmt.Sprintf(`{"tripId":%q,"seats":1}`, rideID), passenger); rec.Code != http.StatusCreated {
		t.Fatalf("book without bags = %d %s", rec.Code, rec.Body)
	}
	_, other := e.registerUser(t)
	rec := e.do("POST", "/api/bookings", fmt.Sprintf(`{"tripId":%q,"seats":1,"smallBags":1}`, rideID), other)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "no space for small bags") {
		t.Errorf("bag on a ride without space = %d %s, want 409", rec.Code, rec.Body)
	}
}

func TestBaggageUpdateBooking(t *testing.T) {
	e := newTestEnv(t)
	_, driver := e.registerUser(t)
	_, first := e.registerUser(t)
	_, second := e.registerUser(t)

	rideID := e.createRide(t, driver, `"seatsTotal":4,"smallBagsTotal":2,"largeBagsTotal":0`)
	var mine struct{ ID string }
	rec := e.do("POST", "/api/bookings", fmt.Sprintf(`{"tripId":%q,"seats":1,"smallBags":1}`, rideID), first)
	_ = json.Unmarshal(rec.Body.Bytes(), &mine)
	e.do("POST", "/api/bookings", fmt.Sprintf(`{"tripId":%q,"seats":1,"smallBags":1}`, rideID), second)

	// Its own bag doesn't count against it, but the other passenger's does
	if rec := e.do("PATCH", "/api/bookings/"+mine.ID, `{"seats":1,"smallBags":2}`, first); rec.Code != http.StatusConflict {
		t.Errorf("grow past capacity = %d %s, want 409", rec.Code, rec.Body)
	}
	if rec := e.do("PATCH", "/api/bookings/"+mine.ID, `{"seats":1,"smallBags":0}`, first); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"smallBags":0`) {
		t.Errorf("drop bags = %d %s, want 200 with 0 bags", rec.Code, rec.Body)
	}
	// A seats-only PATCH leaves bags alone
	e.do("PATCH", "/api/bookings/"+mine.ID, `{"seats":1,"smallBags":1}`, first)
	if rec := e.do("PATCH", "/api/bookings/"+mine.ID, `{"seats":2}`, first); !strings.Contains(rec.Body.String(), `"smallBags":1`) {
		t.Errorf("seats-only PATCH = %d %s, want bags kept", rec.Code, rec.Body)
	}
}

func TestBaggageRideValidation(t *testing.T) {
	e := newTestEnv(t)
	_, driver := e.registerUser(t)
	_, passenger := e.registerUser(t)

	rec := e.do("POST", "/api/rides", `{"originCity":"A","originCountry":"B","destinationCity":"C","destinationCountry":"D",
		"departureAt":"2099-01-01T09:00:00Z","seatsTotal":2,"smallBagsTotal":-1}`, driver)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("negative bags = %d %s, want 400", rec.Code, rec.Body)
	}

	for _, extra := range []string{`"seatsTotal":9`, `"seatsTotal":2,"smallBagsTotal":9`, `"seatsTotal":2,"largeBagsTotal":5`} {
		rec := e.do("POST", "/api/rides", `{"originCity":"A","originCountry":"B","destinationCity":"C","destinationCountry":"D",
			"departureAt":"2099-01-01T09:00:00Z",`+extra+`}`, driver)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d %s, want 400", extra, rec.Code, rec.Body)
		}
	}

	rideID := e.createRide(t, driver, `"seatsTotal":2,"smallBagsTotal":2,"largeBagsTotal":1`)
	e.do("POST", "/api/bookings", fmt.Sprintf(`{"tripId":%q,"seats":1,"smallBags":2,"largeBags":1}`, rideID), passenger)

	update := func(small, large int) *http.Response {
		body := fmt.Sprintf(`{"originCity":"Ljubljana","originCountry":"Slovenia","destinationCity":"Zagreb",
			"destinationCountry":"Croatia","departureAt":"2099-01-01T09:00:00Z","seatsTotal":2,"smallBagsTotal":%d,"largeBagsTotal":%d}`, small, large)
		return e.do("PUT", "/api/rides/"+rideID, body, driver).Result()
	}
	if res := update(1, 1); res.StatusCode != http.StatusBadRequest {
		t.Errorf("lower small below booked = %d, want 400", res.StatusCode)
	}
	if res := update(2, 0); res.StatusCode != http.StatusBadRequest {
		t.Errorf("lower large below booked = %d, want 400", res.StatusCode)
	}
	if res := update(3, 2); res.StatusCode != http.StatusOK {
		t.Errorf("raise bag space = %d, want 200", res.StatusCode)
	}
}
