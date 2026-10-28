package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type emailTransportFunc func(*http.Request) (*http.Response, error)

func (f emailTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBrevoWelcomeEmailRequest(t *testing.T) {
	s := NewBrevoEmailSender("email-key", "welcome@example.test", "Agro-Shield")
	s.client.Transport = emailTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.String() != "https://api.brevo.com/v3/smtp/email" || r.Header.Get("api-key") != "email-key" {
			t.Fatal("incorrect Brevo endpoint or credentials")
		}
		var payload struct {
			Sender  map[string]string   `json:"sender"`
			To      []map[string]string `json:"to"`
			Subject string              `json:"subject"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Sender["email"] != "welcome@example.test" || len(payload.To) != 1 || payload.To[0]["email"] != "member@example.test" || payload.Subject != "Welcome to Agro-Shield" {
			t.Fatal("incorrect welcome email sender, recipient or subject")
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"messageId":"test-message"}`))}, nil
	})
	if err := s.SendWelcome(context.Background(), "member@example.test", "Member"); err != nil {
		t.Fatal(err)
	}
}

func TestBrevoRejectionDoesNotExposeRecipientOrKey(t *testing.T) {
	s := NewBrevoEmailSender("secret-email-key", "welcome@example.test", "Agro-Shield")
	s.client.Transport = emailTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"message":"member@example.test secret-email-key"}`))}, nil
	})
	err := s.SendWelcome(context.Background(), "member@example.test", "Member")
	if err == nil || strings.Contains(err.Error(), "member@example.test") || strings.Contains(err.Error(), "secret-email-key") {
		t.Fatalf("unsafe email error=%v", err)
	}
}

func TestBrevoWelcomeEmailHonorsCancellation(t *testing.T) {
	s := NewBrevoEmailSender("email-key", "welcome@example.test", "Agro-Shield")
	s.client.Transport = emailTransportFunc(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.SendWelcome(ctx, "member@example.test", "Member"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}
