package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOTPDevProviderSendAndVerify(t *testing.T) {
	for _, suffix := range []string{"", "/v1/"} {
		t.Run("baseURL="+suffix, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/verifications" {
					t.Errorf("request path = %q", r.URL.Path)
				}
				if r.Header.Get("X-OTP-Key") != "test-key" {
					t.Error("missing API key")
				}
				switch r.Method {
				case http.MethodPost:
					var payload struct {
						Data map[string]string `json:"data"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Errorf("decode request: %v", err)
					}
					for key, want := range map[string]string{
						"phone": "2349060904492", "code": "012345", "channel": "sms",
						"sender": "AgroShield", "template": "test-template",
					} {
						if got := payload.Data[key]; got != want {
							t.Errorf("payload %s = %q, want %q", key, got, want)
						}
					}
					requests.Add(1)
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"data":{"message_id":"test-message","phone":"2349060904492"}}`))
				case http.MethodGet:
					if r.URL.Query().Get("phone") != "2349060904492" || r.URL.Query().Get("code") != "012345" {
						t.Errorf("unexpected verification query: %s", r.URL.RawQuery)
					}
					requests.Add(1)
					_, _ = w.Write([]byte(`{"data":[{"phone":"+2349060904492"}]}`))
				default:
					t.Errorf("unexpected method: %s", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			defer server.Close()
			provider, err := NewOTPDevProvider("test-key", "AgroShield", "test-template", server.URL+suffix)
			if err != nil {
				t.Fatal(err)
			}
			if err := provider.Send(context.Background(), "09060904492", "012345", "withdrawal"); err != nil {
				t.Fatalf("Send: %v", err)
			}
			if err := provider.Verify(context.Background(), "09060904492", "012345"); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if requests.Load() != 2 {
				t.Fatal("send and verification must both contact OTP.dev")
			}
		})
	}
}

func TestOTPDevProviderRejectsInvalidVerification(t *testing.T) {
	for _, response := range []string{`{"data":[]}`, `{"data":[{"phone":"2348012345678"}]}`} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			provider, err := NewOTPDevProvider("test-key", "AgroShield", "test-template", server.URL)
			if err != nil {
				t.Fatal(err)
			}
			if err := provider.Verify(context.Background(), "09060904492", "012345"); !errors.Is(err, ErrOTPInvalid) {
				t.Fatalf("Verify error = %v, want ErrOTPInvalid", err)
			}
		})
	}
}

func TestOTPDevProviderFailureDoesNotExposeCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"errors":[{"code":1136,"message":"phone=2349060904492 code=012345"}]}`))
	}))
	defer server.Close()
	provider, err := NewOTPDevProvider("test-key", "AgroShield", "test-template", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	err = provider.Verify(context.Background(), "09060904492", "012345")
	if err == nil || errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("provider outage must return an operational error, got %v", err)
	}
	if strings.Contains(err.Error(), "012345") || strings.Contains(err.Error(), "2349060904492") {
		t.Fatalf("error exposed verification credentials: %v", err)
	}
}
