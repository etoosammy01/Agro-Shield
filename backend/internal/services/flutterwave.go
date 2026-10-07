package services

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const flutterwaveBaseURL = "https://api.flutterwave.com/v3"
const flutterwaveRequestTimeout = 10 * time.Second
const maxFlutterwaveResponseBytes = 1 << 20
const bankDirectoryCacheTTL = 6 * time.Hour

type FlutterwaveClient struct {
	SecretKey   string
	SecretHash  string
	RedirectURL string
	httpClient  *http.Client
	bankMu      sync.Mutex
	bankCache   []Bank
	bankCacheAt time.Time
	bankCountry string
}

func NewFlutterwaveClient(secretKey, secretHash, redirectURL string) *FlutterwaveClient {
	return &FlutterwaveClient{
		SecretKey:   secretKey,
		SecretHash:  secretHash,
		RedirectURL: redirectURL,
		httpClient:  &http.Client{Timeout: flutterwaveRequestTimeout},
	}
}

func readFlutterwaveResponse(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFlutterwaveResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read flutterwave response: %w", err)
	}
	if len(body) > maxFlutterwaveResponseBytes {
		return nil, fmt.Errorf("flutterwave response exceeds %d bytes", maxFlutterwaveResponseBytes)
	}
	return body, nil
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

	respBody, err := readFlutterwaveResponse(resp)
	if err != nil {
		return "", err
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

	respBody, err := readFlutterwaveResponse(resp)
	if err != nil {
		return nil, err
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

type CreateTransferRequest struct {
	BankCode      string
	AccountNumber string
	AccountName   string
	Amount        int64
	Currency      string
	Reference     string
}

type transferResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		ID int64 `json:"id"`
	} `json:"data"`
}

func (c *FlutterwaveClient) CreateTransfer(req CreateTransferRequest) (string, error) {
	payload := struct {
		AccountBank   string `json:"account_bank"`
		AccountNumber string `json:"account_number"`
		Amount        int64  `json:"amount"`
		Currency      string `json:"currency"`
		Reference     string `json:"reference"`
		Beneficiary   string `json:"beneficiary_name"`
		Narration     string `json:"narration"`
	}{
		AccountBank:   req.BankCode,
		AccountNumber: req.AccountNumber,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Reference:     req.Reference,
		Beneficiary:   req.AccountName,
		Narration:     "Agro-Shield wallet withdrawal",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal transfer: %w", err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, flutterwaveBaseURL+"/transfers", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build transfer request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("call flutterwave transfer: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readFlutterwaveResponse(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("flutterwave transfer returned %d: %s", resp.StatusCode, string(respBody))
	}
	var parsed transferResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal transfer response: %w", err)
	}
	if parsed.Status != "success" || parsed.Data.ID <= 0 {
		return "", fmt.Errorf("flutterwave rejected transfer: %s", parsed.Message)
	}
	return fmt.Sprintf("%d", parsed.Data.ID), nil
}

type Bank struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

func (c *FlutterwaveClient) ListBanks(country string) ([]Bank, error) {
	c.bankMu.Lock()
	defer c.bankMu.Unlock()
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		return nil, fmt.Errorf("bank directory country is required")
	}
	if c.bankCountry == country && time.Since(c.bankCacheAt) < bankDirectoryCacheTTL && len(c.bankCache) > 0 {
		return append([]Bank(nil), c.bankCache...), nil
	}

	httpReq, err := http.NewRequest(http.MethodGet, flutterwaveBaseURL+"/banks/"+url.PathEscape(country), nil)
	if err != nil {
		return nil, fmt.Errorf("build bank list request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call flutterwave bank list: %w", err)
	}
	defer resp.Body.Close()
	body, err := readFlutterwaveResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flutterwave bank list returned %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    []struct {
			Name string          `json:"name"`
			Code json.RawMessage `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal bank list: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("flutterwave bank list failed: %s", parsed.Message)
	}
	banks := make([]Bank, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		var code string
		if err := json.Unmarshal(item.Code, &code); err != nil {
			code = strings.Trim(string(item.Code), `"`)
		}
		name := strings.TrimSpace(item.Name)
		code = strings.TrimSpace(code)
		if name != "" && code != "" {
			banks = append(banks, Bank{Name: name, Code: code})
		}
	}
	c.bankCache = append([]Bank(nil), banks...)
	c.bankCacheAt = time.Now()
	c.bankCountry = country
	return banks, nil
}

type VerifiedTransfer struct {
	ID        string
	Reference string
	Status    string
	Amount    int64
	Currency  string
}

func (c *FlutterwaveClient) VerifyTransfer(transferID string) (*VerifiedTransfer, error) {
	url := fmt.Sprintf("%s/transfers/%s", flutterwaveBaseURL, transferID)
	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build transfer verification request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call flutterwave transfer verification: %w", err)
	}
	defer resp.Body.Close()
	body, err := readFlutterwaveResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flutterwave transfer verification returned %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Status string `json:"status"`
		Data   struct {
			ID        int64  `json:"id"`
			Reference string `json:"reference"`
			Status    string `json:"status"`
			Amount    int64  `json:"amount"`
			Currency  string `json:"currency"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal transfer verification: %w", err)
	}
	if parsed.Status != "success" || parsed.Data.ID <= 0 {
		return nil, fmt.Errorf("flutterwave could not verify transfer")
	}
	return &VerifiedTransfer{
		ID:        fmt.Sprintf("%d", parsed.Data.ID),
		Reference: parsed.Data.Reference,
		Status:    parsed.Data.Status,
		Amount:    parsed.Data.Amount,
		Currency:  parsed.Data.Currency,
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
