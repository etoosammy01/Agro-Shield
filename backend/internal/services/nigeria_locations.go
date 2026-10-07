package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const nigeriaLocationsURL = "https://nga-states-lga.onrender.com"
const locationCacheTTL = 24 * time.Hour
const locationRetryBackoff = 5 * time.Minute
const maxLocationResponseBytes = 1 << 20

var ErrUnknownNigeriaState = errors.New("unknown Nigerian state")

type NigeriaLocationService struct {
	client       *http.Client
	mu           sync.Mutex
	states       []string
	stateAt      time.Time
	stateRetryAt time.Time
	lgas         map[string]cachedLocationList
	lgaRetryAt   map[string]time.Time
}

type cachedLocationList struct {
	values  []string
	at      time.Time
	retryAt time.Time
}

func NewNigeriaLocationService() *NigeriaLocationService {
	return &NigeriaLocationService{
		client:     &http.Client{Timeout: 10 * time.Second},
		lgas:       make(map[string]cachedLocationList),
		lgaRetryAt: make(map[string]time.Time),
	}
}

func (s *NigeriaLocationService) States() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.states) > 0 && now.Sub(s.stateAt) < locationCacheTTL {
		return append([]string(nil), s.states...), nil
	}
	if now.Before(s.stateRetryAt) {
		if len(s.states) > 0 {
			return append([]string(nil), s.states...), nil
		}
		return nil, fmt.Errorf("state directory refresh is temporarily backed off")
	}

	states, err := s.fetch(nigeriaLocationsURL + "/fetch")
	if err != nil {
		s.stateRetryAt = now.Add(locationRetryBackoff)
		if len(s.states) > 0 {
			return append([]string(nil), s.states...), nil
		}
		return nil, err
	}
	s.states = states
	s.stateAt = time.Now()
	s.stateRetryAt = time.Time{}
	return append([]string(nil), states...), nil
}

func (s *NigeriaLocationService) LGAs(state string) ([]string, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return nil, fmt.Errorf("state is required")
	}
	states, err := s.States()
	if err != nil {
		return nil, fmt.Errorf("load Nigerian states: %w", err)
	}
	for _, knownState := range states {
		if strings.EqualFold(state, knownState) {
			state = knownState
			break
		}
	}
	if !containsState(states, state) {
		return nil, ErrUnknownNigeriaState
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(state)
	cached, exists := s.lgas[key]
	now := time.Now()
	if exists && now.Sub(cached.at) < locationCacheTTL {
		return append([]string(nil), cached.values...), nil
	}
	if exists && now.Before(cached.retryAt) {
		return append([]string(nil), cached.values...), nil
	}
	if retryAt := s.lgaRetryAt[key]; now.Before(retryAt) {
		return nil, fmt.Errorf("local government directory refresh is temporarily backed off")
	}

	endpoint := nigeriaLocationsURL + "/?state=" + url.QueryEscape(state)
	lgas, err := s.fetch(endpoint)
	if err != nil {
		s.lgaRetryAt[key] = now.Add(locationRetryBackoff)
		if exists && len(cached.values) > 0 {
			cached.retryAt = now.Add(locationRetryBackoff)
			s.lgas[key] = cached
			return append([]string(nil), cached.values...), nil
		}
		return nil, err
	}
	s.lgas[key] = cachedLocationList{values: lgas, at: time.Now()}
	delete(s.lgaRetryAt, key)
	return append([]string(nil), lgas...), nil
}

func containsState(states []string, state string) bool {
	for _, knownState := range states {
		if knownState == state {
			return true
		}
	}
	return false
}

func (s *NigeriaLocationService) fetch(endpoint string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build location directory request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request location directory: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("location directory returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLocationResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read location directory response: %w", err)
	}
	if len(body) > maxLocationResponseBytes {
		return nil, fmt.Errorf("location directory response exceeds %d bytes", maxLocationResponseBytes)
	}
	var values []string
	if err := json.Unmarshal(body, &values); err != nil {
		return nil, fmt.Errorf("decode location directory response: %w", err)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("location directory returned no locations")
	}
	return result, nil
}
