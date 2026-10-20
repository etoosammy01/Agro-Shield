package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type EmailSender interface {
	SendWelcome(to, name string) error
}

type BrevoEmailSender struct {
	apiKey    string
	fromEmail string
	fromName  string
	client    *http.Client
}

func NewBrevoEmailSender(
	apiKey, fromEmail, fromName string,
) *BrevoEmailSender {
	return &BrevoEmailSender{
		apiKey:    strings.TrimSpace(apiKey),
		fromEmail: strings.TrimSpace(fromEmail),
		fromName:  strings.TrimSpace(fromName),
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *BrevoEmailSender) SendWelcome(to, name string) error {
	to = strings.TrimSpace(to)
	name = strings.TrimSpace(name)

	if s.apiKey == "" || s.fromEmail == "" {
		return fmt.Errorf("Brevo API email configuration is incomplete")
	}

	parsed, err := mail.ParseAddress(to)
	if err != nil || parsed.Address != to {
		return fmt.Errorf("recipient email is invalid")
	}

	if strings.ContainsAny(name, "\r\n") {
		return fmt.Errorf("recipient name is invalid")
	}

	fromName := s.fromName
	if fromName == "" {
		fromName = "Agro-Shield"
	}

	payload := struct {
		Sender struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"sender"`
		To []struct {
			Email string `json:"email"`
			Name  string `json:"name,omitempty"`
		} `json:"to"`
		Subject     string `json:"subject"`
		TextContent string `json:"textContent"`
	}{}

	payload.Sender.Name = fromName
	payload.Sender.Email = s.fromEmail
	payload.To = append(payload.To, struct {
		Email string `json:"email"`
		Name  string `json:"name,omitempty"`
	}{
		Email: to,
		Name:  name,
	})
	payload.Subject = "Welcome to Agro-Shield"
	payload.TextContent = fmt.Sprintf(
		"Hello %s,\n\n"+
			"Welcome to Agro-Shield!\n\n"+
			"Your account has been created successfully. "+
			"Please complete your profile to get started.\n\n"+
			"Thank you for joining us.\n\n"+
			"The Agro-Shield Team\n",
		name,
	)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("prepare welcome email: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.brevo.com/v3/smtp/email",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create Brevo email request: %w", err)
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("api-key", s.apiKey)

	client := s.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send welcome email through Brevo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"Brevo rejected welcome email (HTTP %d): %s",
			resp.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	return nil
}
