package services

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var (
	// ErrInvalidLocation is returned when origin or destination is empty.
	ErrInvalidLocation = errors.New("origin and destination cannot be empty")
	// ErrRouteNotFound is returned when no distance or coordinates can be resolved.
	ErrRouteNotFound = errors.New("route distance not found")
)

// Coordinates represents latitude and longitude for a city.
type Coordinates struct {
	Lat float64
	Lon float64
}

// SupportedLocations lists the countries and cities supported by the application.
var SupportedLocations = map[string][]string{
	"Sweden":         {"Gothenburg", "Stockholm", "Malmö", "Uppsala"},
	"Denmark":        {"Copenhagen"},
	"Germany":        {"Berlin", "Munich", "Hamburg", "Frankfurt", "Cologne"},
	"France":         {"Paris", "Lyon", "Marseille", "Toulouse", "Nice"},
	"Netherlands":    {"Amsterdam", "Rotterdam", "The Hague", "Utrecht"},
	"Spain":          {"Madrid", "Barcelona", "Valencia", "Seville"},
	"United Kingdom": {"London", "Manchester", "Birmingham", "Edinburgh"},
	"Italy":          {"Rome", "Milan", "Florence", "Naples"},
}

// cityCoordinates stores the geographic coordinates of supported cities.
var cityCoordinates = map[string]Coordinates{
	"stockholm":  {Lat: 59.3293, Lon: 18.0686},
	"gothenburg": {Lat: 57.7089, Lon: 11.9746},
	"malmo":      {Lat: 55.6050, Lon: 13.0038},
	"uppsala":    {Lat: 59.8586, Lon: 17.6389},
	"copenhagen": {Lat: 55.6761, Lon: 12.5683},
	"berlin":     {Lat: 52.5200, Lon: 13.4050},
	"munich":     {Lat: 48.1351, Lon: 11.5820},
	"hamburg":    {Lat: 53.5511, Lon: 9.9937},
	"frankfurt":  {Lat: 50.1109, Lon: 8.6821},
	"cologne":    {Lat: 50.9375, Lon: 6.9603},
	"paris":      {Lat: 48.8566, Lon: 2.3522},
	"lyon":       {Lat: 45.7640, Lon: 4.8357},
	"marseille":  {Lat: 43.2965, Lon: 5.3698},
	"toulouse":   {Lat: 43.6047, Lon: 1.4442},
	"nice":       {Lat: 43.7102, Lon: 7.2620},
	"amsterdam":  {Lat: 52.3676, Lon: 4.9041},
	"rotterdam":  {Lat: 51.9244, Lon: 4.4777},
	"the hague":  {Lat: 52.0705, Lon: 4.3007},
	"utrecht":    {Lat: 52.0907, Lon: 5.1214},
	"madrid":     {Lat: 40.4168, Lon: -3.7038},
	"barcelona":  {Lat: 41.3879, Lon: 2.1699},
	"valencia":   {Lat: 39.4699, Lon: -0.3763},
	"seville":    {Lat: 37.3891, Lon: -5.9845},
	"london":     {Lat: 51.5074, Lon: -0.1278},
	"manchester": {Lat: 53.4808, Lon: -2.2426},
	"birmingham": {Lat: 52.4862, Lon: -1.8904},
	"edinburgh":  {Lat: 55.9533, Lon: -3.1883},
	"rome":       {Lat: 41.9028, Lon: 12.4964},
	"milan":      {Lat: 45.4642, Lon: 9.1900},
	"florence":   {Lat: 43.7696, Lon: 11.2558},
	"naples":     {Lat: 40.8518, Lon: 14.2681},
}

// cityAliases maps common variations, translations, and spellings to canonical city keys.
var cityAliases = map[string]string{
	"stockholm":         "stockholm",
	"stoccolma":         "stockholm",
	"gothenburg":        "gothenburg",
	"goteborg":          "gothenburg",
	"göteborg":          "gothenburg",
	"malmo":             "malmo",
	"malmö":             "malmo",
	"uppsala":           "uppsala",
	"copenhagen":        "copenhagen",
	"copenaghen":        "copenhagen",
	"kobenhavn":         "copenhagen",
	"københavn":         "copenhagen",
	"berlin":            "berlin",
	"berlino":           "berlin",
	"munich":            "munich",
	"monaco":            "munich",
	"monaco di baviera": "munich",
	"münchen":           "munich",
	"muenchen":          "munich",
	"hamburg":           "hamburg",
	"amburgo":           "hamburg",
	"frankfurt":         "frankfurt",
	"francoforte":       "frankfurt",
	"cologne":           "cologne",
	"colonia":           "cologne",
	"koln":              "cologne",
	"köln":              "cologne",
	"paris":             "paris",
	"parigi":            "paris",
	"lyon":              "lyon",
	"lione":             "lyon",
	"marseille":         "marseille",
	"marsiglia":         "marseille",
	"toulouse":          "toulouse",
	"tolosa":            "toulouse",
	"nice":              "nice",
	"nizza":             "nice",
	"amsterdam":         "amsterdam",
	"rotterdam":         "rotterdam",
	"the hague":         "the hague",
	"den haag":          "the hague",
	"l'aia":             "the hague",
	"laia":              "the hague",
	"utrecht":           "utrecht",
	"madrid":            "madrid",
	"barcelona":         "barcelona",
	"barcellona":        "barcelona",
	"valencia":          "valencia",
	"seville":           "seville",
	"sevilla":           "seville",
	"siviglia":          "seville",
	"london":            "london",
	"londra":            "london",
	"manchester":        "manchester",
	"birmingham":        "birmingham",
	"edinburgh":         "edinburgh",
	"edimburgo":         "edinburgh",
	"rome":              "rome",
	"roma":              "rome",
	"milan":             "milan",
	"milano":            "milan",
	"florence":          "florence",
	"firenze":           "florence",
	"naples":            "naples",
	"napoli":            "naples",
}

// routeKey builds a canonical bidirectional key so order does not matter.
func routeKey(c1, c2 string) string {
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	return c1 + "<->" + c2
}

// knownDistances maps canonical city pairs to verified driving road distances in km.
var knownDistances = map[string]int{
	routeKey("stockholm", "munich"):      1420,
	routeKey("stockholm", "gothenburg"):  470,
	routeKey("stockholm", "malmo"):       615,
	routeKey("stockholm", "uppsala"):     70,
	routeKey("stockholm", "copenhagen"):  650,
	routeKey("stockholm", "berlin"):      1010,
	routeKey("stockholm", "paris"):       1890,
	routeKey("stockholm", "rome"):        2450,
	routeKey("gothenburg", "malmo"):      270,
	routeKey("gothenburg", "copenhagen"): 315,
	routeKey("gothenburg", "uppsala"):    490,
	routeKey("gothenburg", "berlin"):     810,
	routeKey("malmo", "copenhagen"):      40,
	routeKey("malmo", "berlin"):          440,
	routeKey("berlin", "munich"):         585,
	routeKey("berlin", "hamburg"):        290,
	routeKey("berlin", "frankfurt"):      550,
	routeKey("berlin", "cologne"):        575,
	routeKey("berlin", "copenhagen"):     440,
	routeKey("berlin", "amsterdam"):      655,
	routeKey("berlin", "paris"):          1050,
	routeKey("berlin", "rome"):           1500,
	routeKey("munich", "frankfurt"):      390,
	routeKey("munich", "hamburg"):        790,
	routeKey("munich", "cologne"):        575,
	routeKey("munich", "milan"):          495,
	routeKey("munich", "rome"):           920,
	routeKey("munich", "paris"):          840,
	routeKey("munich", "copenhagen"):     1030,
	routeKey("hamburg", "copenhagen"):    330,
	routeKey("frankfurt", "cologne"):     190,
	routeKey("frankfurt", "paris"):       570,
	routeKey("frankfurt", "amsterdam"):   440,
	routeKey("cologne", "amsterdam"):     260,
	routeKey("cologne", "paris"):         500,
	routeKey("paris", "lyon"):            465,
	routeKey("paris", "marseille"):       775,
	routeKey("paris", "nice"):            930,
	routeKey("paris", "toulouse"):        680,
	routeKey("paris", "amsterdam"):       515,
	routeKey("paris", "london"):          460,
	routeKey("paris", "madrid"):          1270,
	routeKey("paris", "barcelona"):       1035,
	routeKey("paris", "rome"):            1420,
	routeKey("paris", "milan"):           850,
	routeKey("lyon", "marseille"):        315,
	routeKey("lyon", "nice"):             470,
	routeKey("lyon", "toulouse"):         540,
	routeKey("lyon", "milan"):            440,
	routeKey("marseille", "nice"):        200,
	routeKey("marseille", "toulouse"):    400,
	routeKey("marseille", "barcelona"):   510,
	routeKey("toulouse", "barcelona"):    395,
	routeKey("amsterdam", "rotterdam"):   75,
	routeKey("amsterdam", "the hague"):   65,
	routeKey("amsterdam", "utrecht"):     45,
	routeKey("rotterdam", "the hague"):   25,
	routeKey("rotterdam", "utrecht"):     60,
	routeKey("the hague", "utrecht"):     70,
	routeKey("madrid", "barcelona"):      620,
	routeKey("madrid", "valencia"):       360,
	routeKey("madrid", "seville"):        530,
	routeKey("barcelona", "valencia"):    350,
	routeKey("barcelona", "seville"):     995,
	routeKey("valencia", "seville"):      650,
	routeKey("london", "birmingham"):     190,
	routeKey("london", "manchester"):     335,
	routeKey("london", "edinburgh"):      650,
	routeKey("birmingham", "manchester"): 140,
	routeKey("birmingham", "edinburgh"):  475,
	routeKey("manchester", "edinburgh"):  350,
	routeKey("rome", "milan"):            575,
	routeKey("rome", "florence"):         275,
	routeKey("rome", "naples"):           225,
	routeKey("milan", "florence"):        305,
	routeKey("milan", "naples"):          775,
	routeKey("florence", "naples"):       475,
}

// NormalizeCity standardizes a city name (lowercase, trimmed, resolved alias).
func NormalizeCity(city string) (string, bool) {
	clean := strings.ToLower(strings.TrimSpace(city))
	if clean == "" {
		return "", false
	}
	canonical, exists := cityAliases[clean]
	if exists {
		return canonical, true
	}
	return clean, false
}

// haversineKm calculates the great-circle distance between two coordinates in km.
func haversineKm(c1, c2 Coordinates) float64 {
	const earthRadiusKm = 6371.0
	lat1 := c1.Lat * math.Pi / 180
	lon1 := c1.Lon * math.Pi / 180
	lat2 := c2.Lat * math.Pi / 180
	lon2 := c2.Lon * math.Pi / 180

	dlat := lat2 - lat1
	dlon := lon2 - lon1

	a := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dlon/2)*math.Sin(dlon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

// estimateRoadDistanceKm estimates driving road distance using great-circle distance
// multiplied by the typical European road circuity factor (1.25).
func estimateRoadDistanceKm(c1, c2 Coordinates) int {
	straightKm := haversineKm(c1, c2)
	roadKm := int(math.Round(straightKm * 1.25))
	if roadKm < 1 {
		return 1
	}
	return roadKm
}

// GetDistanceKm returns the driving distance in kilometers between origin and destination.
// It checks the explicit distance map first, and falls back to a coordinate-based road
// estimation when both locations have known geographic coordinates.
func GetDistanceKm(origin, destination string) (int, error) {
	normOrigin, originOk := NormalizeCity(origin)
	normDest, destOk := NormalizeCity(destination)

	if !originOk || !destOk {
		if strings.TrimSpace(origin) == "" || strings.TrimSpace(destination) == "" {
			return 0, ErrInvalidLocation
		}
	}

	if normOrigin == normDest {
		return 0, nil
	}

	// 1. Direct lookup in predefined road distances
	key := routeKey(normOrigin, normDest)
	if d, ok := knownDistances[key]; ok {
		return d, nil
	}

	// 2. Fallback: estimate from coordinates
	c1, hasC1 := cityCoordinates[normOrigin]
	c2, hasC2 := cityCoordinates[normDest]
	if hasC1 && hasC2 {
		return estimateRoadDistanceKm(c1, c2), nil
	}

	return 0, fmt.Errorf("%w: %s -> %s", ErrRouteNotFound, origin, destination)
}
