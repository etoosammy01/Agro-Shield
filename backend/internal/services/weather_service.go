package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Weather struct {
	Available      bool
	Temperature    string
	Summary        string
	Humidity       string
	RainChance     string
	Conditions     string
	Recommendation string
}

type WeatherService struct{ client *http.Client }

func NewWeatherService() *WeatherService {
	return &WeatherService{client: &http.Client{Timeout: 5 * time.Second}}
}

func (s *WeatherService) Current(ctx context.Context, location string) (Weather, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return Weather{}, fmt.Errorf("farmer location is empty")
	}
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
	return Weather{true, fmt.Sprintf("%.0f°", raw.Current.Temperature), summary, fmt.Sprintf("%.0f%%", raw.Current.Humidity), fmt.Sprintf("%.0f%%", rain), summary, weatherRecommendation(raw.Current.Code)}, nil
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
