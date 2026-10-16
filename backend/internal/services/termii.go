package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Termii's send-SMS endpoint and request shape.
//
// Verified live (2026-10-14) against api.ng.termii.com:
//   - POST /api/sms/send exists and answers JSON
//   - authentication is by an "api_key" field, checked before the message body
//   - an unauthenticated call returns 401 {"error":"Unauthorized","message":"A valid API key is required"}
//
// NOT yet verified, because it needs a real account: the exact accepted values
// for "type" and "channel", and the response field names. Before going live,
// send one message with SMS_PROVIDER=termii and confirm a successful response
// carries a non-empty message_id. If any of that differs, only this file needs
// to change — the OTP service talks to the SMSSender interface, not to Termii.
const (
	termiiSendPath        = "/api/sms/send"
	termiiRequestTimeout  = 10 * time.Second
	maxTermiiResponseSize = 64 << 10
)

// TermiiSender sends SMS through Termii's HTTP API.
type TermiiSender struct {
	APIKey   string
	SenderID string
	BaseURL  string
	client   *http.Client
}

func NewTermiiSender(apiKey, senderID, baseURL string) *TermiiSender {
	return &TermiiSender{
		APIKey:   strings.TrimSpace(apiKey),
		SenderID: strings.TrimSpace(senderID),
		BaseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:   &http.Client{Timeout: termiiRequestTimeout},
	}
}

type termiiSendRequest struct {
	To      string `json:"to"`
	From    string `json:"from"`
	SMS     string `json:"sms"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	APIKey  string `json:"api_key"`
}

type termiiSendResponse struct {
	MessageID string `json:"message_id"`
	Message   string `json:"message"`
}

func (t *TermiiSender) Send(to, message string) error {
	if t.APIKey == "" || t.SenderID == "" {
		return fmt.Errorf("termii sender is not configured")
	}
	// Termii expects international format with no leading "+".
	recipient := NormalizeNigerianPhone(to)
	if recipient == "" {
		return fmt.Errorf("invalid Nigerian phone number for SMS delivery")
	}

	payload := termiiSendRequest{
		To:      recipient,
		From:    t.SenderID,
		SMS:     message,
		Type:    "plain",
		Channel: "generic",
		APIKey:  t.APIKey,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal termii request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, t.BaseURL+termiiSendPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build termii request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call termii: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTermiiResponseSize+1))
	if err != nil {
		return fmt.Errorf("read termii response: %w", err)
	}
	if len(respBody) > maxTermiiResponseSize {
		return fmt.Errorf("termii response exceeds %d bytes", maxTermiiResponseSize)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("termii returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed termiiSendResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("unmarshal termii response: %w", err)
	}
	// Termii can answer 200 while still rejecting the message, so the status
	// code alone is not proof of delivery.
	if parsed.MessageID == "" && !strings.Contains(strings.ToLower(parsed.Message), "success") {
		return fmt.Errorf("termii rejected the message: %s", parsed.Message)
	}
	return nil
}
