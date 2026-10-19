package services_test

import (
	"errors"
	"testing"

	"shotgun-api/services"
)

func TestGetDistanceKm_KnownRoutes(t *testing.T) {
	tests := []struct {
		origin      string
		destination string
		wantKm      int
	}{
		{"Stockholm", "Munich", 1420},
		{"Stockholm", "Gothenburg", 470},
		{"Stockholm", "Malmö", 615},
		{"Stockholm", "Uppsala", 70},
		{"Gothenburg", "Malmo", 270},
		{"Malmö", "Copenhagen", 40},
		{"Berlin", "Munich", 585},
		{"Berlin", "Hamburg", 290},
		{"Paris", "Lyon", 465},
		{"Paris", "Marseille", 775},
		{"Rome", "Milan", 575},
		{"Rome", "Florence", 275},
		{"Rome", "Naples", 225},
		{"Madrid", "Barcelona", 620},
		{"London", "Manchester", 335},
		{"Amsterdam", "Rotterdam", 75},
	}

	for _, tt := range tests {
		// Test direct route (A -> B)
		direct, err := services.GetDistanceKm(tt.origin, tt.destination)
		if err != nil {
			t.Errorf("GetDistanceKm(%q, %q) unexpected error: %v", tt.origin, tt.destination, err)
		}
		if direct != tt.wantKm {
			t.Errorf("GetDistanceKm(%q, %q) = %d, want %d", tt.origin, tt.destination, direct, tt.wantKm)
		}

		// Test inverse route (B -> A)
		inverse, err := services.GetDistanceKm(tt.destination, tt.origin)
		if err != nil {
			t.Errorf("GetDistanceKm(%q, %q) unexpected error: %v", tt.destination, tt.origin, err)
		}
		if inverse != tt.wantKm {
			t.Errorf("GetDistanceKm(%q, %q) = %d, want %d", tt.destination, tt.origin, inverse, tt.wantKm)
		}

		// Verify symmetry
		if direct != inverse {
			t.Errorf("asymmetry between %s->%s (%d) and %s->%s (%d)",
				tt.origin, tt.destination, direct, tt.destination, tt.origin, inverse)
		}
	}
}

func TestGetDistanceKm_AliasesAndNormalization(t *testing.T) {
	tests := []struct {
		origin      string
		destination string
		wantKm      int
	}{
		{"stockholm", "munich", 1420},
		{"  Stockholm  ", " Munich ", 1420},
		{"Stoccolma", "Monaco", 1420},
		{"Stoccolma", "Monaco di Baviera", 1420},
		{"Göteborg", "Stockholm", 470},
		{"Goteborg", "Stockholm", 470},
		{"Roma", "Milano", 575},
		{"Parigi", "Lione", 465},
		{"Copenaghen", "Malmo", 40},
		{"København", "Malmö", 40},
	}

	for _, tt := range tests {
		km, err := services.GetDistanceKm(tt.origin, tt.destination)
		if err != nil {
			t.Errorf("GetDistanceKm(%q, %q) error: %v", tt.origin, tt.destination, err)
		}
		if km != tt.wantKm {
			t.Errorf("GetDistanceKm(%q, %q) = %d, want %d", tt.origin, tt.destination, km, tt.wantKm)
		}
	}
}

func TestGetDistanceKm_SameCity(t *testing.T) {
	cities := []string{"Stockholm", "Munich", "Rome", "Paris", "Berlin"}
	for _, city := range cities {
		km, err := services.GetDistanceKm(city, city)
		if err != nil {
			t.Errorf("GetDistanceKm(%q, %q) error: %v", city, city, err)
		}
		if km != 0 {
			t.Errorf("GetDistanceKm(%q, %q) = %d, want 0", city, city, km)
		}
	}
}

func TestGetDistanceKm_EstimatedFallback(t *testing.T) {
	// City pair without explicit entry in knownDistances, resolved via coordinates
	d1, err := services.GetDistanceKm("Uppsala", "Nice")
	if err != nil {
		t.Fatalf("unexpected error for Uppsala -> Nice: %v", err)
	}
	if d1 <= 0 {
		t.Errorf("expected positive distance, got %d", d1)
	}

	d2, err := services.GetDistanceKm("Nice", "Uppsala")
	if err != nil {
		t.Fatalf("unexpected error for Nice -> Uppsala: %v", err)
	}
	if d1 != d2 {
		t.Errorf("asymmetry in estimated route: Uppsala->Nice (%d) != Nice->Uppsala (%d)", d1, d2)
	}
}

func TestGetDistanceKm_AllSupportedCitiesSymmetric(t *testing.T) {
	var allCities []string
	for _, list := range services.SupportedLocations {
		allCities = append(allCities, list...)
	}

	for i := 0; i < len(allCities); i++ {
		for j := i; j < len(allCities); j++ {
			c1 := allCities[i]
			c2 := allCities[j]

			dist1, err1 := services.GetDistanceKm(c1, c2)
			if err1 != nil {
				t.Fatalf("unexpected error for %s -> %s: %v", c1, c2, err1)
			}

			dist2, err2 := services.GetDistanceKm(c2, c1)
			if err2 != nil {
				t.Fatalf("unexpected error for %s -> %s: %v", c2, c1, err2)
			}

			if dist1 != dist2 {
				t.Errorf("asymmetry between %s and %s: %d != %d", c1, c2, dist1, dist2)
			}

			if c1 == c2 && dist1 != 0 {
				t.Errorf("expected 0 km between same city %s, got %d", c1, dist1)
			}
			if c1 != c2 && dist1 <= 0 {
				t.Errorf("expected positive distance between %s and %s, got %d", c1, c2, dist1)
			}
		}
	}
}

func TestGetDistanceKm_Errors(t *testing.T) {
	// Empty origin
	_, err := services.GetDistanceKm("", "Stockholm")
	if !errors.Is(err, services.ErrInvalidLocation) {
		t.Errorf("expected ErrInvalidLocation for empty origin, got %v", err)
	}

	// Empty destination
	_, err = services.GetDistanceKm("Stockholm", "   ")
	if !errors.Is(err, services.ErrInvalidLocation) {
		t.Errorf("expected ErrInvalidLocation for empty destination, got %v", err)
	}

	// Unknown city
	_, err = services.GetDistanceKm("Atlantis", "Stockholm")
	if !errors.Is(err, services.ErrRouteNotFound) {
		t.Errorf("expected ErrRouteNotFound for unknown city, got %v", err)
	}
}

func TestCalculateCO2(t *testing.T) {
	zero := 0
	co2_110 := 110

	// 1. Electric car (0 g/km) for Stockholm -> Munich (1420 km) with 0 passengers
	dist, consumed, saved, isElec := services.CalculateCO2("Stockholm", "Munich", 0, &zero)
	if dist != 1420 {
		t.Errorf("dist = %d, want 1420", dist)
	}
	if !isElec {
		t.Errorf("expected isElectric to be true")
	}
	if consumed != 0.0 {
		t.Errorf("consumed = %f, want 0.0", consumed)
	}
	// individual = 1420 * 120 / 1000 = 170.4; saved = 170.4 - 0 = 170.4
	if saved != 170.4 {
		t.Errorf("saved = %f, want 170.4", saved)
	}

	// 2. Combustion car with 110 g/km for Gothenburg -> Stockholm (470 km) with 1 passenger
	dist, consumed, saved, isElec = services.CalculateCO2("Gothenburg", "Stockholm", 1, &co2_110)
	if dist != 470 {
		t.Errorf("dist = %d, want 470", dist)
	}
	if isElec {
		t.Errorf("expected isElectric to be false")
	}
	// consumed = 470 * 110 / 1000 = 51.7
	if consumed != 51.7 {
		t.Errorf("consumed = %f, want 51.7", consumed)
	}
	// individual = 470 * 120 / 1000 = 56.4
	// saved = 56.4 - (51.7 / 2) = 56.4 - 25.85 = 30.55
	if saved != 30.55 {
		t.Errorf("saved = %f, want 30.55", saved)
	}

	// 3. Unassigned car (nil) defaults to 120 g/km baseline
	dist, consumed, saved, isElec = services.CalculateCO2("Gothenburg", "Stockholm", 1, nil)
	if dist != 470 || isElec != false {
		t.Errorf("unexpected dist=%d isElec=%v", dist, isElec)
	}
	if consumed != 56.4 {
		t.Errorf("consumed = %f, want 56.4", consumed)
	}
	// saved = 56.4 - 56.4 / 2 = 28.2
	if saved != 28.2 {
		t.Errorf("saved = %f, want 28.2", saved)
	}

	// 4. Unknown route returns zeros
	dist, consumed, saved, isElec = services.CalculateCO2("Unknown1", "Unknown2", 1, nil)
	if dist != 0 || consumed != 0 || saved != 0 || isElec != false {
		t.Errorf("expected zero values for unknown route, got %d, %f, %f, %v", dist, consumed, saved, isElec)
	}
}
