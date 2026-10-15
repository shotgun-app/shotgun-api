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
