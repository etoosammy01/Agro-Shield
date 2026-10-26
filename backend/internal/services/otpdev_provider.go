package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OTPDevProvider struct {
	apiKey     string
	sender     string
	templateID string
	baseURL    string
	client     *http.Client
}

func NewOTPDevProvider(
	apiKey, sender, templateID, baseURL string,
) *OTPDevProvider {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.otp.dev"
	}

	return &OTPDevProvider{
		apiKey:     strings.TrimSpace(apiKey),
		sender:     strings.TrimSpace(sender),
		templateID: strings.TrimSpace(templateID),
		baseURL:    baseURL,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// normalizeOTPPhone removes formatting and converts a Nigerian local
// number beginning with 0 to its international 234 format.
func normalizeOTPPhone(phone string) string {
	var digits strings.Builder

	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}

	result := digits.String()
	if strings.HasPrefix(result, "0") {
		result = "234" + result[1:]
	}

	return result
}

func validOTPCode(code string) bool {
	if len(code) < 4 || len(code) > 8 {
		return false
	}

	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

func (p *OTPDevProvider) request(
	ctx context.Context,
	method string,
	endpoint string,
	payload []byte,
) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("OTP.dev provider is nil")
	}
	if p.apiKey == "" {
		return nil, fmt.Errorf("OTP.dev API key is not configured")
	}

	var body io.Reader
	if len(payload) > 0 {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		endpoint,
		body,
	)
	if err != nil {
		return nil, fmt.Errorf("create OTP.dev request: %w", err)
	}

	req.Header.Set("X-OTP-Key", p.apiKey)
	req.Header.Set("Accept", "application/json")

	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	client := p.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact OTP.dev: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(
		io.LimitReader(resp.Body, 4096),
	)
	if readErr != nil {
		return nil, fmt.Errorf("read OTP.dev response: %w", readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"OTP.dev request failed (HTTP %d): %s",
			resp.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	return responseBody, nil
}

// Send delivers the application-generated OTP through OTP.dev.
func (p *OTPDevProvider) Send(
	ctx context.Context,
	phone, code, purpose string,
) error {
	_ = purpose // The configured OTP.dev template handles message content.

	if p == nil {
		return fmt.Errorf("OTP.dev provider is nil")
	}
	if p.apiKey == "" || p.sender == "" || p.templateID == "" {
		return fmt.Errorf("OTP.dev configuration is incomplete")
	}
	if !validOTPCode(code) {
		return fmt.Errorf("OTP code must contain 4 to 8 digits")
	}

	phone = normalizeOTPPhone(phone)
	if phone == "" {
		return fmt.Errorf("recipient phone number is invalid")
	}

	payload := struct {
		Data struct {
			Channel  string `json:"channel"`
			Sender   string `json:"sender"`
			Phone    string `json:"phone"`
			Template string `json:"template"`
			Code     string `json:"code"`
		} `json:"data"`
	}{}

	payload.Data.Channel = "sms"
	payload.Data.Sender = p.sender
	payload.Data.Phone = phone
	payload.Data.Template = p.templateID
	payload.Data.Code = code

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("prepare OTP.dev request: %w", err)
	}

	endpoint := p.baseURL + "/v1/verifications"
	_, err = p.request(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("send OTP: %w", err)
	}

	return nil
}

// Verify checks the code against OTP.dev's verification records.
func (p *OTPDevProvider) Verify(
	ctx context.Context,
	phone, code string,
) error {
	if p == nil {
		return fmt.Errorf("OTP.dev provider is nil")
	}
	if !validOTPCode(code) {
		return fmt.Errorf("OTP code must contain 4 to 8 digits")
	}

	phone = normalizeOTPPhone(phone)
	if phone == "" {
		return fmt.Errorf("recipient phone number is invalid")
	}

	endpoint, err := url.Parse(
		p.baseURL + "/v1/verifications",
	)
	if err != nil {
		return fmt.Errorf("prepare OTP verification URL: %w", err)
	}

	query := endpoint.Query()
	query.Set("code", code)
	query.Set("phone", phone)
	endpoint.RawQuery = query.Encode()

	responseBody, err := p.request(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
	)
	if err != nil {
		return fmt.Errorf("verify OTP: %w", err)
	}

	var result struct {
		Data []struct {
			Phone string `json:"phone"`
		} `json:"data"`
	}

	if err := json.Unmarshal(responseBody, &result); err != nil {
		return fmt.Errorf("decode OTP.dev verification response: %w", err)
	}

	for _, item := range result.Data {
		if normalizeOTPPhone(item.Phone) == phone {
			return nil
		}
	}

	return fmt.Errorf("invalid OTP code")
}
