package services

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

// OTPDevProvider sends and verifies Agro-Shield's generated codes through OTP.dev.
// Local verification retains the user/purpose binding, expiry, and single-use rules.
type OTPDevProvider struct {
	apiKey, sender, templateID, baseURL string
	client                              *http.Client
}

var _ OTPProvider = (*OTPDevProvider)(nil)

func NewOTPDevProvider(apiKey, sender, templateID, baseURL string) (*OTPDevProvider, error) {
	apiKey = strings.TrimSpace(apiKey)
	sender = strings.TrimSpace(sender)
	templateID = strings.TrimSpace(templateID)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if apiKey == "" || sender == "" || templateID == "" {
		return nil, errors.New("OTP.dev API key, sender, and template ID are required")
	}
	if baseURL == "" {
		baseURL = "https://api.otp.dev/v1"
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("OTP.dev base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	baseURL = strings.TrimSuffix(baseURL, "/v1") + "/v1"
	return &OTPDevProvider{
		apiKey: apiKey, sender: sender, templateID: templateID, baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Do not forward the API key or resend a code to a redirect target.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (d *OTPDevProvider) Send(ctx context.Context, phone, code, purpose string) error {
	if d == nil || d.client == nil {
		return errors.New("OTP.dev delivery is not configured")
	}
	normalized := NormalizeNigerianPhone(phone)
	if normalized == "" {
		return errors.New("a valid Nigerian phone number is required for OTP.dev delivery")
	}
	phone = otpDevPhone(normalized)
	if phone == "" {
		return errors.New("could not convert the phone number to international format")
	}
	if !isWellFormedOTPCode(code) {
		return errors.New("OTP.dev delivery requires a six-digit code")
	}

	// Supplying our own code keeps the SMS identical to the hash stored by
	// OTPService. Verified against OTP.dev: it returns 201 for this payload.
	payload, err := json.Marshal(map[string]any{"data": map[string]string{
		"channel": "sms", "sender": d.sender, "phone": phone,
		"template": d.templateID, "code": code,
	}})
	if err != nil {
		return fmt.Errorf("encode OTP.dev request: %w", err)
	}
	responseBody, err := d.request(ctx, http.MethodPost, d.baseURL+"/verifications", payload)
	if err != nil {
		return err
	}

	// OTP.dev wraps the confirmation in a "data" object.
	var result struct {
		Data struct {
			MessageID string `json:"message_id"`
			Phone     string `json:"phone"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil ||
		strings.TrimSpace(result.Data.MessageID) == "" ||
		otpDevDigits(result.Data.Phone) != phone {
		return errors.New("OTP.dev returned an invalid delivery confirmation")
	}
	return nil
}

// Verify checks the code against OTP.dev in addition to OTPService's local checks.
func (d *OTPDevProvider) Verify(ctx context.Context, phone, code string) error {
	if !isWellFormedOTPCode(code) {
		return ErrOTPInvalid
	}
	phone = otpDevPhone(NormalizeNigerianPhone(phone))
	if phone == "" {
		return ErrOTPUnusablePhone
	}
	if d == nil || d.client == nil {
		return errors.New("OTP.dev delivery is not configured")
	}
	endpoint, err := url.Parse(d.baseURL + "/verifications")
	if err != nil {
		return errors.New("could not create OTP.dev verification URL")
	}
	query := endpoint.Query()
	query.Set("phone", phone)
	query.Set("code", code)
	endpoint.RawQuery = query.Encode()
	responseBody, err := d.request(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	var result struct {
		Data []struct {
			Phone string `json:"phone"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return errors.New("OTP.dev returned an invalid verification response")
	}
	for _, item := range result.Data {
		if otpDevDigits(item.Phone) == phone {
			return nil
		}
	}
	return ErrOTPInvalid
}

func (d *OTPDevProvider) request(ctx context.Context, method, endpoint string, payload []byte) ([]byte, error) {
	if d == nil || d.client == nil {
		return nil, errors.New("OTP.dev delivery is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("could not create OTP.dev request")
	}
	request.Header.Set("X-OTP-Key", d.apiKey)
	request.Header.Set("Accept", "application/json")
	if len(payload) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := d.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("OTP.dev request failed")
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return nil, errors.New("could not read OTP.dev response")
	}

	// Provider errors can echo the request, including the code and phone, so
	// only the numeric error code is surfaced (e.g. 1136 not authorized,
	// 1512 invalid template, 1620 missing sender).
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiErr struct {
			Errors []struct {
				Code any `json:"code"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if len(apiErr.Errors) > 0 {
			return nil, fmt.Errorf("OTP.dev rejected request (HTTP %d, code %v)", response.StatusCode, apiErr.Errors[0].Code)
		}
		return nil, fmt.Errorf("OTP.dev rejected request (HTTP %d)", response.StatusCode)
	}
	return body, nil
}

// otpDevPhone converts any common way of writing a Nigerian mobile number to
// the digits-only international form OTP.dev expects (e.g. 2349060904492).
// It returns "" for anything that is not a valid Nigerian mobile number.
func otpDevPhone(input string) string {
	d := otpDevDigits(input)
	d = strings.TrimPrefix(d, "00") // international dialing prefix
	d = strings.TrimPrefix(d, "234")
	d = strings.TrimPrefix(d, "0") // local trunk zero, also after +234
	// Nigerian mobile numbers are 10 digits starting with 7, 8 or 9.
	if len(d) != 10 || d[0] < '7' || d[0] > '9' {
		return ""
	}
	return "234" + d
}

func otpDevDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}
