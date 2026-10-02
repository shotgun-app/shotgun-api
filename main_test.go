package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"shotgun-api/config"
)

func TestPing(t *testing.T) {
	router := setupRouter(&config.Config{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "pong" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "pong")
	}
}

func TestUnauthenticatedRoutes(t *testing.T) {
	router := setupRouter(&config.Config{}, nil)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/trips"},
		{http.MethodGet, "/bookings/mine"},
		{http.MethodPost, "/bookings"},
		{http.MethodPatch, "/bookings/123"},
		{http.MethodDelete, "/bookings/123"},
		{http.MethodGet, "/api/trips"},
		{http.MethodGet, "/api/bookings/mine"},
		{http.MethodPost, "/api/bookings"},
		{http.MethodPatch, "/api/bookings/123"},
		{http.MethodDelete, "/api/bookings/123"},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s status = %d, want %d", rt.method, rt.path, rec.Code, http.StatusUnauthorized)
			}
		})
	}
}
