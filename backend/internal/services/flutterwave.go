// Package services holds clients for external providers - Flutterwave
// today, potentially others later. Kept separate from repository (our own
// DB) and handlers (our own HTTP surface).
package services

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const flutterwaveBaseURL = "https://api.flutterwave.com/v3"

type FlutterwaveClient struct {
	SecretKey   string
	SecretHash  string
	RedirectURL string
	httpClient  *http.Client
}

func NewFlutterwaveClient(secretKey, secretHash, redirectURL string) *FlutterwaveClient {
	return &FlutterwaveClient{
		SecretKey:   secretKey,
		SecretHash:  secretHash,
		RedirectURL: redirectURL,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

type InitializePaymentRequest struct {
	Reference     string
	Amount        string // pass the same string you stored in the payments table
	Currency      string
	CustomerEmail string
	CustomerName  string
	Title         string
	Description   string
}

type initializePaymentPayload struct {
	TxRef          string           `json:"tx_ref"`
	Amount         string           `json:"amount"`
	Currency       string           `json:"currency"`
	RedirectURL    string           `json:"redirect_url"`
	Customer       fwCustomer       `json:"customer"`
	Customizations fwCustomizations `json:"customizations"`
}

type fwCustomer struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type fwCustomizations struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type initializePaymentResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Link string `json:"link"`
	} `json:"data"`
}

// InitializePayment asks Flutterwave for a hosted checkout link.
func (c *FlutterwaveClient) InitializePayment(req InitializePaymentRequest) (string, error) {
	payload := initializePaymentPayload{
		TxRef:       req.Reference,
		Amount:      req.Amount,
		Currency:    req.Currency,
		RedirectURL: c.RedirectURL,
		Customer: fwCustomer{
			Email: req.CustomerEmail,
			Name:  req.CustomerName,
		},
		Customizations: fwCustomizations{
			Title:       req.Title,
			Description: req.Description,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, flutterwaveBaseURL+"/payments", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("call flutterwave: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("flutterwave returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed initializePaymentResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if parsed.Status != "success" {
		return "", fmt.Errorf("flutterwave rejected the request: %s", parsed.Message)
	}

	return parsed.Data.Link, nil
}

type VerifiedTransaction struct {
	TransactionID string
	TxRef         string
	Status        string
	Amount        float64
	Currency      string
}

type verifyTransactionResponse struct {
	Status string `json:"status"`
	Data   struct {
		ID       int64   `json:"id"`
		TxRef    string  `json:"tx_ref"`
		Status   string  `json:"status"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	} `json:"data"`
}

// VerifyTransaction confirms a transaction's real status directly with
// Flutterwave. Call this from BOTH the redirect callback and the webhook
// handler - never trust either one's claimed status without this check.
func (c *FlutterwaveClient) VerifyTransaction(transactionID string) (*VerifiedTransaction, error) {
	url := fmt.Sprintf("%s/transactions/%s/verify", flutterwaveBaseURL, transactionID)

	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call flutterwave: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flutterwave returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed verifyTransactionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &VerifiedTransaction{
		TransactionID: fmt.Sprintf("%d", parsed.Data.ID),
		TxRef:         parsed.Data.TxRef,
		Status:        parsed.Data.Status,
		Amount:        parsed.Data.Amount,
		Currency:      parsed.Data.Currency,
	}, nil
}

// VerifyWebhookSignature checks the "verif-hash" header Flutterwave sends
// on every webhook against our configured secret hash. This is a shared-
// secret compare, not an HMAC - reject anything that doesn't match exactly.
func (c *FlutterwaveClient) VerifyWebhookSignature(receivedHash string) bool {
	if c.SecretHash == "" || receivedHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.SecretHash), []byte(receivedHash)) == 1
}