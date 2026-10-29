package otp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	otpDevDefaultBaseURL = "https://api.otp.dev"
	otpDevResponseLimit  = 64 << 10
)

type OTPDevConfig struct {
	APIKey     string
	Sender     string
	TemplateID string
	BaseURL    string
}

// OTPDevProvider delivers application-generated codes through OTP.dev. The
// service verifies them locally, matching Agrilinks' delivery flow.
type OTPDevProvider struct {
	apiKey     string
	sender     string
	templateID string
	endpoint   string
	client     *http.Client
}

var _ Provider = (*OTPDevProvider)(nil)

func NewOTPDevProvider(cfg OTPDevConfig) (*OTPDevProvider, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Sender = strings.TrimSpace(cfg.Sender)
	cfg.TemplateID = strings.TrimSpace(cfg.TemplateID)
	if cfg.APIKey == "" || strings.ContainsAny(cfg.APIKey, "\r\n") {
		return nil, errors.New("OTP.dev requires a valid API key")
	}
	if cfg.Sender == "" || cfg.TemplateID == "" {
		return nil, errors.New("OTP.dev requires a sender and template ID")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = otpDevDefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("OTP.dev base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	return &OTPDevProvider{
		apiKey:     cfg.APIKey,
		sender:     cfg.Sender,
		templateID: cfg.TemplateID,
		endpoint:   baseURL + "/v1/verifications",
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Custom authentication headers must never follow an API redirect.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

type otpDevConfirmation struct {
	MessageID string `json:"message_id"`
	Phone     string `json:"phone"`
}

type otpDevSMSData struct {
	Channel  string `json:"channel"`
	Sender   string `json:"sender"`
	Phone    string `json:"phone"`
	Template string `json:"template"`
	Code     string `json:"code"`
}

func (p *OTPDevProvider) Send(ctx context.Context, phone, code, _ string) error {
	if p == nil || p.client == nil {
		return ErrDeliveryDisabled
	}
	recipient := NormalizePhone(phone)
	if recipient == "" {
		return ErrUnusablePhone
	}
	if !isWellFormedCode(code) {
		return ErrInvalid
	}
	payload, err := json.Marshal(struct {
		Data otpDevSMSData `json:"data"`
	}{Data: otpDevSMSData{Channel: "sms", Sender: p.sender, Phone: recipient, Template: p.templateID, Code: code}})
	if err != nil {
		return fmt.Errorf("encode OTP.dev SMS request: %w", err)
	}
	// OTP.dev requires either code or code_length, never both. Supplying the
	// generated code keeps it identical to the application's stored hash.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("could not create OTP.dev SMS request")
	}
	request.Header.Set("Content-Type", "application/json")
	var result struct {
		Data      *otpDevConfirmation `json:"data"`
		MessageID string              `json:"message_id"`
		Phone     string              `json:"phone"`
	}
	if err := p.do(request, &result); err != nil {
		return err
	}
	// Agrilinks' successful live request returns 201 with a nested data object.
	// Also accept the flat 200 confirmation shown in OTP.dev's API reference.
	confirmation := otpDevConfirmation{MessageID: result.MessageID, Phone: result.Phone}
	if result.Data != nil {
		confirmation = *result.Data
	}
	if strings.TrimSpace(confirmation.MessageID) == "" || NormalizePhone(confirmation.Phone) != recipient {
		return errors.New("OTP.dev returned an invalid SMS confirmation")
	}
	return nil
}

func (p *OTPDevProvider) do(request *http.Request, result any) error {
	request.Header.Set("X-OTP-Key", p.apiKey)
	request.Header.Set("Accept", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		// Transport errors can contain URLs or provider details. Expose only
		// safe cancellation/timeout causes.
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			if errors.Is(err, cause) {
				return fmt.Errorf("OTP.dev request failed: %w", cause)
			}
		}
		return errors.New("OTP.dev request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, otpDevResponseLimit+1))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return otpDevAPIError(response.StatusCode, body)
	}
	if err != nil {
		return errors.New("could not read OTP.dev response")
	}
	if len(body) > otpDevResponseLimit {
		return errors.New("OTP.dev response exceeded the size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return errors.New("could not decode OTP.dev response")
	}
	return nil
}

func otpDevAPIError(status int, body []byte) error {
	// Provider bodies can echo a phone, code, or API key. Only expose the
	// documented four-digit API error codes, which help identify account and
	// template problems without returning arbitrary response text.
	var result struct {
		Errors []struct {
			Code json.RawMessage `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &result) == nil && len(result.Errors) > 0 {
		raw := result.Errors[0].Code
		code := strings.TrimSpace(string(raw))
		if len(raw) > 0 && raw[0] == '"' {
			_ = json.Unmarshal(raw, &code)
		}
		if len(code) == 4 && code[0] >= '1' && code[0] <= '9' && strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			return fmt.Errorf("OTP.dev returned HTTP %d (code %s)", status, code)
		}
	}
	return fmt.Errorf("OTP.dev returned HTTP %d", status)
}
