package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListCars(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	cookie := sessionCookie(e.do("POST", "/auth/register", registerBody(email), nil))

	if rec := e.do("GET", "/api/cars", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without cookie = %d, want 401", rec.Code)
	}

	rec := e.do("GET", "/api/cars", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Cars []struct {
			ID, Brand, Model string
			CO2              int
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The catalogue comes from the seed migration
	if len(body.Cars) < 100 {
		t.Fatalf("got %d cars, want at least 100", len(body.Cars))
	}
	var electric, combustion bool
	for _, car := range body.Cars {
		if car.ID == "" || car.Brand == "" || car.Model == "" {
			t.Fatalf("incomplete car: %+v", car)
		}
		electric = electric || car.CO2 == 0
		combustion = combustion || car.CO2 > 100
	}
	if !electric || !combustion {
		t.Errorf("expected both zero-emission and high-emission cars (electric=%v, combustion=%v)", electric, combustion)
	}
}

func TestSetMyCar(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	cookie := sessionCookie(e.do("POST", "/auth/register", registerBody(email), nil))

	var list struct{ Cars []struct{ ID string } }
	_ = json.Unmarshal(e.do("GET", "/api/cars", "", cookie).Body.Bytes(), &list)
	if len(list.Cars) == 0 {
		t.Fatal("no cars in the catalogue")
	}
	carID := list.Cars[0].ID
	setBody := `{"carId":"` + carID + `"}`

	type userBody struct {
		User struct{ Car *struct{ ID string } }
	}

	if rec := e.do("PUT", "/auth/me/car", setBody, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without cookie = %d, want 401", rec.Code)
	}
	if rec := e.do("PUT", "/auth/me/car", `{"carId":"nope"}`, cookie); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid id = %d, want 400", rec.Code)
	}
	unknown := `{"carId":"00000000-0000-0000-0000-000000000000"}`
	if rec := e.do("PUT", "/auth/me/car", unknown, cookie); rec.Code != http.StatusNotFound {
		t.Errorf("unknown car = %d, want 404", rec.Code)
	}

	rec := e.do("PUT", "/auth/me/car", setBody, cookie)
	var got userBody
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.User.Car == nil || got.User.Car.ID != carID {
		t.Fatalf("set = %d %s", rec.Code, rec.Body)
	}

	// The car survives a new request, so it was stored and not just echoed
	got = userBody{}
	_ = json.Unmarshal(e.do("GET", "/auth/me", "", cookie).Body.Bytes(), &got)
	if got.User.Car == nil || got.User.Car.ID != carID {
		t.Errorf("/auth/me car = %+v, want %s", got.User.Car, carID)
	}

	rec = e.do("PUT", "/auth/me/car", `{"carId":null}`, cookie)
	got = userBody{}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.User.Car != nil {
		t.Errorf("clear = %d, car = %+v, want 200 and no car", rec.Code, got.User.Car)
	}
}
