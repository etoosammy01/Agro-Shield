package otp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTermiiProvider_Send(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sms/send" {
			t.Errorf("expected path /api/sms/send, got %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["to"] != "2348012345678" {
			t.Errorf("expected recipient 2348012345678, got %s", payload["to"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, _ := NewTermiiProvider(TermiiConfig{
		APIKey:   "key",
		SenderID: "sender",
		BaseURL:  server.URL,
	})

	err := provider.Send(context.Background(), "+2348012345678", "123456", "verification")
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}
package otp

import (
"context"
"encoding/json"
"net/http"
"net/http/httptest"
"testing"
)

func TestTermiiProvider_Send(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
if r.URL.Path != "/api/sms/send" {
t.Errorf("expected path /api/sms/send, got %s", r.URL.Path)
}
var payload map[string]string
if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["to"] != "2348012345678" {
			t.Errorf("expected recipient 2348012345678, got %s", payload["to"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, _ := NewTermiiProvider(TermiiConfig{
APIKey:   "key",
SenderID: "sender",
BaseURL:  server.URL,
})

	err := provider.Send(context.Background(), "+2348012345678", "123456", "verification")
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}
