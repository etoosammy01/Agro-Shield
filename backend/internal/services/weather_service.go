package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Weather struct {
	Available      bool
	Location       string
	Source         string
	Temperature    string
	Summary        string
	Humidity       string
	RainChance     string
	Conditions     string
	Recommendation string
	LastUpdated    time.Time
	Cached         bool
}

type cachedWeather struct {
	weather   Weather
	fetchedAt time.Time
}

type WeatherService struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]cachedWeather
}

func NewWeatherService() *WeatherService {
	return &WeatherService{
		client: &http.Client{Timeout: 10 * time.Second},
		cache:  make(map[string]cachedWeather),
	}
}

func (s *WeatherService) Current(ctx context.Context, location string) (Weather, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return Weather{}, fmt.Errorf("farmer location is empty")
	}
	cacheKey := strings.ToLower(location)
	if cached, ok := s.cached(cacheKey); ok && time.Since(cached.fetchedAt) < 15*time.Minute {
		weather := cached.weather
		weather.Cached = true
		return weather, nil
	}

	weather, err := s.fetch(ctx, location)
	if err != nil {
		if cached, ok := s.cached(cacheKey); ok {
			fallback := cached.weather
			fallback.Cached = true
			return fallback, err
		}
		return Weather{}, err
	}

	s.mu.Lock()
	s.cache[cacheKey] = cachedWeather{weather: weather, fetchedAt: weather.LastUpdated}
	s.mu.Unlock()
	return weather, nil
}

func (s *WeatherService) cached(key string) (cachedWeather, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	weather, ok := s.cache[key]
	return weather, ok
}

func (s *WeatherService) fetch(ctx context.Context, location string) (Weather, error) {
	q := url.Values{"name": {location}, "count": {"1"}, "language": {"en"}, "format": {"json"}}
	var geo struct {
		Results []struct{ Latitude, Longitude float64 } `json:"results"`
	}
	if err := s.getJSON(ctx, "https://geocoding-api.open-meteo.com/v1/search?"+q.Encode(), &geo); err != nil {
		return Weather{}, err
	}
	if len(geo.Results) == 0 {
		return Weather{}, fmt.Errorf("location %q not found", location)
	}
	lat, lon := geo.Results[0].Latitude, geo.Results[0].Longitude
	wq := url.Values{"latitude": {strconv.FormatFloat(lat, 'f', 4, 64)}, "longitude": {strconv.FormatFloat(lon, 'f', 4, 64)}, "current": {"temperature_2m,relative_humidity_2m,weather_code"}, "hourly": {"precipitation_probability"}, "forecast_days": {"1"}, "timezone": {"auto"}}
	var raw struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Humidity    float64 `json:"relative_humidity_2m"`
			Code        int     `json:"weather_code"`
		} `json:"current"`
		Hourly struct {
			Rain []float64 `json:"precipitation_probability"`
		} `json:"hourly"`
	}
	if err := s.getJSON(ctx, "https://api.open-meteo.com/v1/forecast?"+wq.Encode(), &raw); err != nil {
		return Weather{}, err
	}
	rain := 0.0
	if len(raw.Hourly.Rain) > 0 {
		rain = raw.Hourly.Rain[0]
	}
	summary := weatherSummary(raw.Current.Code)
	return Weather{Available: true, Location: location, Source: "Open-Meteo", Temperature: fmt.Sprintf("%.0f°", raw.Current.Temperature), Summary: summary, Humidity: fmt.Sprintf("%.0f%%", raw.Current.Humidity), RainChance: fmt.Sprintf("%.0f%%", rain), Conditions: summary, Recommendation: weatherRecommendation(raw.Current.Code), LastUpdated: time.Now()}, nil
}

func (s *WeatherService) getJSON(ctx context.Context, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather API returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func weatherSummary(code int) string {
	switch {
	case code == 0:
		return "Clear sky"
	case code <= 3:
		return "Partly cloudy"
	case code <= 48:
		return "Foggy"
	case code <= 67:
		return "Rainy"
	case code <= 77:
		return "Snowy"
	case code <= 82:
		return "Rain showers"
	default:
		return "Thunderstorms"
	}
}
func weatherRecommendation(code int) string {
	if code >= 51 {
		return "Consider protecting crops from wet conditions and postpone moisture-sensitive work."
	}
	return "Current conditions are suitable for routine farm activities and harvesting."
}
