package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"shotgun-api/config"
)

func TestPing(t *testing.T) {
	router := setupRouter(&config.Config{})

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
