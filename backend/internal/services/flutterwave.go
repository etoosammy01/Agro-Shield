package services

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

	// PaymentOptions is the default set of funding methods offered on hosted
	// checkout, e.g. "card,banktransfer".
	PaymentOptions string

	httpClient  *http.Client
	bankMu      sync.Mutex
	bankCache   []Bank
	bankCacheAt time.Time
	bankCountry string
}

func NewFlutterwaveClient(secretKey, secretHash, redirectURL, paymentOptions string) *FlutterwaveClient {
	return &FlutterwaveClient{
		SecretKey:      secretKey,
		SecretHash:     secretHash,
		RedirectURL:    redirectURL,
		PaymentOptions: strings.TrimSpace(paymentOptions),
		httpClient:     &http.Client{Timeout: flutterwaveRequestTimeout},
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

	// PaymentOptions overrides the client default for this one checkout. Leave
	// it empty to use the client's configured methods.
	PaymentOptions string
}

type initializePaymentPayload struct {
	TxRef          string           `json:"tx_ref"`
	Amount         string           `json:"amount"`
	Currency       string           `json:"currency"`
	RedirectURL    string           `json:"redirect_url"`
	PaymentOptions string           `json:"payment_options,omitempty"`
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
	// Which funding methods the checkout offers. Without this the methods are
	// whatever the Flutterwave dashboard happens to be set to, which is not
	// something this codebase can see or version.
	paymentOptions := strings.TrimSpace(req.PaymentOptions)
	if paymentOptions == "" {
		paymentOptions = c.PaymentOptions
	}

	payload := initializePaymentPayload{
		TxRef:          req.Reference,
		Amount:         req.Amount,
		Currency:       req.Currency,
		RedirectURL:    c.RedirectURL,
		PaymentOptions: paymentOptions,
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
	id, err := strconv.ParseInt(strings.TrimSpace(transactionID), 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("invalid Flutterwave transaction ID")
	}
	url := fmt.Sprintf("%s/transactions/%d/verify", flutterwaveBaseURL, id)

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

	if parsed.Status != "success" {
		return nil, fmt.Errorf("Flutterwave did not verify transaction: %s", parsed.Status)
	}
	if parsed.Data.ID != id {
		return nil, fmt.Errorf("verified transaction ID does not match request")
	}
	if strings.TrimSpace(parsed.Data.TxRef) == "" ||
		strings.TrimSpace(parsed.Data.Status) == "" ||
		strings.TrimSpace(parsed.Data.Currency) == "" ||
		parsed.Data.Amount <= 0 {
		return nil, fmt.Errorf("Flutterwave verification response is incomplete")
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

// ErrTransferNotCreated means the provider refused the transfer outright, so no
// payout exists at Flutterwave for it.
//
// It is deliberately distinct from the other errors CreateTransfer returns, and
// the distinction is about money rather than wording. A caller that has already
// reserved the farmer's balance can safely hand the request back for rejection
// only when it knows no transfer was created. A timeout, a 429 or a 500 may well
// have created one, and treating those as a refusal would refund the farmer
// while the provider also pays them - the same money out twice.
//
// So this is raised only where the provider's own answer rules out a transfer
// having been made, and everything ambiguous stays an ordinary error.
var ErrTransferNotCreated = errors.New("the provider refused the transfer")

// transferRefused reports whether a non-2xx answer proves the transfer was never
// created.
//
// Only 401 and 403 qualify: a rejected credential stops the request before
// Flutterwave does any work with it. Everything else stays an ordinary error on
// purpose, including answers that look like refusals:
//
//   - 400 carries both real validation failures and the provider's catch-all
//     "please try again", and the two are not reliably told apart from the
//     message. The resolve endpoint taught the same lesson - see notFoundMessage.
//   - 408 and 429 say nothing about whether the request was processed.
//   - 5xx is a server that failed while handling the call, which may have
//     created the transfer first.
//
// Guessing wrong here refunds money the provider may also be paying out, so the
// unknown answer is left unknown and a failed payout with no transfer id gets
// resolved by an admin through RevertWithdrawalToPending instead.
func transferRefused(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
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
		// A request that got no answer may still have been processed, so this
		// stays an ordinary error: the caller must not read it as "no transfer
		// exists".
		return "", fmt.Errorf("call flutterwave transfer: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readFlutterwaveResponse(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		if transferRefused(resp.StatusCode) {
			return "", fmt.Errorf("%w: %s", ErrTransferNotCreated, http.StatusText(resp.StatusCode))
		}
		return "", fmt.Errorf("flutterwave transfer returned %d: %s", resp.StatusCode, string(respBody))
	}
	var parsed transferResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		// A 2xx we could not parse is not a refusal - the transfer may exist and
		// simply be unreadable from here.
		return "", fmt.Errorf("unmarshal transfer response: %w", err)
	}
	if parsed.Status != "success" {
		// The provider answered with a rejection object rather than a created
		// transfer, which is it saying no.
		return "", fmt.Errorf("%w: %s", ErrTransferNotCreated, parsed.Message)
	}
	if parsed.Data.ID <= 0 {
		// Contradictory rather than a refusal: a success carrying no id may
		// describe a transfer that exists and cannot be named from this
		// response, so it must not be read as "nothing was created".
		return "", fmt.Errorf("flutterwave transfer response is missing its id")
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

// ErrAccountNotResolved means the bank answered and does not hold this account
// number.
//
// It is deliberately distinct from every other error ResolveAccount returns. A
// caller checking whether payout details are correct has to tell "this account
// does not exist at this bank" - so the account's owner is wrong - apart from
// "we could not ask", which is a dead key, a timeout or a rate limit. Reading
// the second as the first would tell a farmer their own account details are
// wrong because our credentials expired.
var ErrAccountNotResolved = errors.New("account not found at this bank")

// notFoundMessage reports whether a provider message says the account does not
// exist, as opposed to the request merely failing.
//
// The wording is observed rather than guessed: against the live API with the
// test key, a number the provider does not hold answers with HTTP 400 and
// "invalid account". The two other failures seen on the same endpoint answer
// differently - a rejected key is 401 "Invalid authorization key", and a missing
// bank code is 400 "destbankcode is not allowed to be empty" - so this message
// is specific to the account, and the other two stay infrastructure errors.
// That distinction is what keeps a credential problem from reading as every
// farmer having the wrong account number.
//
// Flutterwave's catch-all "cannot resolve account number, please try again" is
// deliberately NOT treated as a miss: a provider hiccup produces the same words,
// and calling it a miss would hard-block a legitimate payout on a bad network
// minute. Anything unrecognised falls through to a plain error, which callers
// read as "could not verify" - the safe direction, since it never accuses a
// farmer of wrong details.
func notFoundMessage(message string) bool {
	lowered := strings.ToLower(strings.TrimSpace(message))
	if lowered == "" {
		return false
	}
	if strings.Contains(lowered, "invalid account") {
		return true
	}
	return strings.Contains(lowered, "not found") || strings.Contains(lowered, "does not exist")
}

// providerMessage extracts the human-readable "message" from a provider body, so
// a failure can be classified by what Flutterwave said rather than by the status
// code alone - the not-found case arrives as a 400, not the 404 the
// documentation implies. An unparseable body yields "".
func providerMessage(body []byte) string {
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Message)
}

type ResolvedAccount struct {
	AccountNumber string
	AccountName   string
}

type resolveAccountResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		AccountNumber string `json:"account_number"`
		AccountName   string `json:"account_name"`
	} `json:"data"`
}

// ResolveAccount asks Flutterwave for the registered holder of one bank
// account.
//
// The provider offers no reverse lookup: a bank code is always required, and
// the only thing it answers with is the holder's name. Mapping an account
// number to its bank is therefore done by calling this against several banks
// and keeping the ones that resolve — see WalletService.LookupAccountBanks.
//
// An account that does not exist at the given bank is reported as
// ErrAccountNotResolved, which callers doing a sweep treat as "not this bank".
// Every other error means the question could not be asked, and must not be read
// as an answer about the account.
//
// The method holds no state, so callers may run it concurrently.
func (c *FlutterwaveClient) ResolveAccount(accountNumber, bankCode string) (*ResolvedAccount, error) {
	accountNumber = strings.TrimSpace(accountNumber)
	bankCode = strings.TrimSpace(bankCode)
	if accountNumber == "" || bankCode == "" {
		return nil, fmt.Errorf("account number and bank code are required")
	}

	payload := struct {
		AccountNumber string `json:"account_number"`
		AccountBank   string `json:"account_bank"`
	}{AccountNumber: accountNumber, AccountBank: bankCode}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal resolve payload: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, flutterwaveBaseURL+"/accounts/resolve", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build resolve request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call flutterwave resolve: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readFlutterwaveResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// The not-found answer arrives as 400 "invalid account", not the 404 the
		// API documentation implies, so the body decides. A 404 with no readable
		// message is the same answer by status.
		detail := providerMessage(respBody)
		if resp.StatusCode == http.StatusNotFound || notFoundMessage(detail) {
			if detail == "" {
				detail = fmt.Sprintf("provider returned %d", resp.StatusCode)
			}
			return nil, fmt.Errorf("%w: %s", ErrAccountNotResolved, detail)
		}
		return nil, fmt.Errorf("flutterwave resolve returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed resolveAccountResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal resolve response: %w", err)
	}
	if parsed.Status != "success" {
		if notFoundMessage(parsed.Message) {
			return nil, fmt.Errorf("%w: %s", ErrAccountNotResolved, parsed.Message)
		}
		return nil, fmt.Errorf("flutterwave could not resolve account: %s", parsed.Message)
	}
	accountName := strings.TrimSpace(parsed.Data.AccountName)
	if accountName == "" {
		// A success with no name is not proof the account exists, so it counts
		// as no match rather than a match with a blank name. It is not a
		// ErrAccountNotResolved either: the provider said the request
		// succeeded, so this is an unusable answer, not a "no such account".
		return nil, fmt.Errorf("flutterwave resolve response is missing the account name")
	}

	return &ResolvedAccount{AccountNumber: accountNumber, AccountName: accountName}, nil
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
