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
	"regexp"
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
		return "", providerHTTPError("flutterwave", resp.StatusCode, respBody)
	}

	var parsed initializePaymentResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if parsed.Status != "success" {
		return "", fmt.Errorf("flutterwave rejected the request: %s", safeProviderMessage(parsed.Message))
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
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
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
		return nil, providerHTTPError("flutterwave", resp.StatusCode, respBody)
	}

	var parsed verifyTransactionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if parsed.Status != "success" {
		return nil, fmt.Errorf("Flutterwave did not verify transaction: %s", safeProviderMessage(parsed.Status))
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

// ErrTransactionNotFound means the provider answered and has no transaction
// for the reference.
//
// Like ErrAccountNotResolved, this is separated from every other failure on
// purpose. Only this answer may close a deposit attempt; a timeout, a rejected
// key or a 5xx proves nothing about the payment and must leave the attempt
// pending for the next pass.
var ErrTransactionNotFound = errors.New("no transaction exists for this reference")

// VerifyTransactionByReference asks Flutterwave whether any transaction was
// ever created for a reference, which is the question the deposit reconciliation
// needs: the numeric id is only known once a payment has been made, so a deposit
// whose webhook never arrived can only be checked by the reference we stored.
func (c *FlutterwaveClient) VerifyTransactionByReference(reference string) (*VerifiedTransaction, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("transaction reference is required")
	}
	endpoint := fmt.Sprintf("%s/transactions/verify_by_reference?tx_ref=%s",
		flutterwaveBaseURL, url.QueryEscape(reference))

	httpReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build reference verification request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call flutterwave reference verification: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readFlutterwaveResponse(resp)
	if err != nil {
		return nil, err
	}
	// The not-found answer arrives as a 400 carrying "No transaction found"
	// rather than as a 404, so the body decides, exactly as ResolveAccount does.
	if resp.StatusCode != http.StatusOK {
		detail := safeProviderMessage(providerMessage(respBody))
		if resp.StatusCode == http.StatusNotFound || notFoundMessage(detail) {
			if detail == "" {
				detail = fmt.Sprintf("provider returned %d", resp.StatusCode)
			}
			return nil, fmt.Errorf("%w: %s", ErrTransactionNotFound, detail)
		}
		return nil, providerHTTPError("flutterwave reference verification", resp.StatusCode, respBody)
	}

	var parsed verifyTransactionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal reference verification: %w", err)
	}
	if parsed.Status != "success" {
		if notFoundMessage(parsed.Message) {
			return nil, fmt.Errorf("%w: %s", ErrTransactionNotFound, safeProviderMessage(parsed.Message))
		}
		return nil, fmt.Errorf("flutterwave could not verify the reference: %s", safeProviderMessage(parsed.Message))
	}
	if strings.TrimSpace(parsed.Data.TxRef) == "" {
		// A success with no transaction is contradictory, not an answer. Reading
		// it as "nothing exists" would expire an attempt on a provider quirk.
		return nil, fmt.Errorf("Flutterwave reference verification response is incomplete")
	}
	if parsed.Data.TxRef != reference {
		return nil, fmt.Errorf("verified reference does not match request")
	}
	if strings.TrimSpace(parsed.Data.Status) == "" ||
		strings.TrimSpace(parsed.Data.Currency) == "" ||
		parsed.Data.Amount <= 0 {
		return nil, fmt.Errorf("Flutterwave reference verification response is incomplete")
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
		return "", providerHTTPError("flutterwave transfer", resp.StatusCode, respBody)
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
		return "", fmt.Errorf("%w: %s", ErrTransferNotCreated, safeProviderMessage(parsed.Message))
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
		return nil, providerHTTPError("flutterwave bank list", resp.StatusCode, body)
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
		return nil, fmt.Errorf("flutterwave bank list failed: %s", safeProviderMessage(parsed.Message))
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
	// A bank code the provider does not recognise cannot hold an account in
	// this directory. Treat it as a negative answer for that bank so one stale
	// shortlist entry does not make every otherwise-complete sweep look broken.
	if strings.Contains(lowered, "unknown bank code") || strings.Contains(lowered, "invalid bank code") {
		return true
	}
	// The verify-by-reference endpoint answers a reference it never issued with
	// HTTP 400 "No transaction was found for this id" - observed against the
	// test key. Without this, an abandoned checkout is reported as an
	// infrastructure failure on every pass and can never be closed.
	if strings.Contains(lowered, "no transaction") {
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

// safeProviderMessage keeps provider diagnostics useful without placing a raw
// response body in an error string or log. Provider bodies can contain request
// echoes, URLs, or account fragments; only the parsed "message" field is kept,
// with control characters stripped and a hard length cap.
var providerLongDigitRun = regexp.MustCompile(`\d{6,}`)

func safeProviderMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range message {
		if b.Len() >= 200 {
			break
		}
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	// Account numbers, BVNs, and phone numbers are runs of digits. They are
	// not needed to diagnose a provider failure, so redact long runs while
	// preserving the descriptive words around them.
	return strings.TrimSpace(providerLongDigitRun.ReplaceAllString(b.String(), "[redacted]"))
}

func providerHTTPError(prefix string, status int, body []byte) error {
	detail := safeProviderMessage(providerMessage(body))
	if detail == "" {
		return fmt.Errorf("%s returned HTTP %d", prefix, status)
	}
	return fmt.Errorf("%s returned HTTP %d: %s", prefix, status, detail)
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
		detail := safeProviderMessage(providerMessage(respBody))
		if resp.StatusCode == http.StatusNotFound || notFoundMessage(detail) {
			if detail == "" {
				detail = fmt.Sprintf("provider returned %d", resp.StatusCode)
			}
			return nil, fmt.Errorf("%w: %s", ErrAccountNotResolved, detail)
		}
		return nil, providerHTTPError("flutterwave resolve", resp.StatusCode, respBody)
	}

	var parsed resolveAccountResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal resolve response: %w", err)
	}
	if parsed.Status != "success" {
		if notFoundMessage(parsed.Message) {
			return nil, fmt.Errorf("%w: %s", ErrAccountNotResolved, safeProviderMessage(parsed.Message))
		}
		return nil, fmt.Errorf("flutterwave could not resolve account: %s", safeProviderMessage(parsed.Message))
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

// ErrVirtualAccountRefused means Flutterwave answered and refused to open the
// account, so no account exists for this attempt.
//
// Only answers that rule an account out qualify. A timeout, a 429 or a 5xx may
// have created one, and retrying those with a second reference would be how a
// farmer ends up with two accounts. Those stay ordinary errors, and the
// deterministic tx_ref makes a retry safe.
var ErrVirtualAccountRefused = errors.New("the provider refused the virtual account request")

// ErrVirtualAccountAccessDenied means the provider rejected the credentials or
// did not grant this merchant access to static virtual accounts. It is distinct
// from a refusal of the farmer's details: retrying the same identity form cannot
// fix it, and the page must not imply that the BVN or NIN was wrong.
var ErrVirtualAccountAccessDenied = errors.New("the provider did not permit virtual account creation")

// ErrVirtualAccountNotFound means the provider answered and has no virtual
// account for the reference. It is an answer, not a failure: the caller may
// safely try to create the account again with the same reference.
var ErrVirtualAccountNotFound = errors.New("no virtual account exists for this reference")

// CreateVirtualAccountRequest describes a permanent NGN virtual account.
//
// FirstName, LastName, Email and Phone come from the authenticated farmer's
// account, never from the browser. IdentityKind is "bvn" or "nin" and
// IdentityNumber is that document's number; it is sent to Flutterwave and is
// never persisted by Agro-Shield.
type CreateVirtualAccountRequest struct {
	Email          string
	TxRef          string
	Phone          string
	FirstName      string
	LastName       string
	Narration      string
	BankCode       string
	IdentityKind   string
	IdentityNumber string
}

// VirtualAccountDetails is the provider's confirmed account, reduced to the
// fields the wallet displays. Nothing else from the response body is kept.
type VirtualAccountDetails struct {
	TxRef         string
	OrderRef      string
	ProviderRef   string
	AccountNumber string
	BankName      string
	AccountName   string
	Currency      string
	Status        string
}

type virtualAccountResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		ResponseCode    string `json:"response_code"`
		ResponseMessage string `json:"response_message"`
		FLWRef          string `json:"flw_ref"`
		OrderRef        string `json:"order_ref"`
		AccountNumber   string `json:"account_number"`
		AccountName     string `json:"account_name"`
		BankName        string `json:"bank_name"`
		Currency        string `json:"currency"`
		AccountStatus   string `json:"account_status"`
	} `json:"data"`
}

// virtualAccountRefused reports whether a provider answer proves no account was
// created. Credential failures and validation answers do; anything mentioning a
// duplicate reference deliberately does not, because that is the provider
// saying an account may already exist under this tx_ref.
func virtualAccountRefused(status int, message string) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		lowered := strings.ToLower(message)
		for _, marker := range []string{"duplicate", "already exist", "already been used", "has been used"} {
			if strings.Contains(lowered, marker) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func parseVirtualAccount(response virtualAccountResponse) (*VirtualAccountDetails, error) {
	if response.Status != "success" {
		return nil, fmt.Errorf("flutterwave virtual account request failed: %s", safeProviderMessage(response.Message))
	}
	switch strings.TrimSpace(response.Data.ResponseCode) {
	case "02", "00":
	case "":
		// Some responses omit response_code but still carry a full account. A
		// missing number below is what actually proves nothing was created.
	default:
		return nil, fmt.Errorf("flutterwave virtual account request was not accepted: %s",
			safeProviderMessage(response.Data.ResponseMessage))
	}
	accountNumber := strings.TrimSpace(response.Data.AccountNumber)
	if accountNumber == "" {
		return nil, fmt.Errorf("flutterwave virtual account response is missing the account number")
	}
	return &VirtualAccountDetails{
		OrderRef:      strings.TrimSpace(response.Data.OrderRef),
		ProviderRef:   strings.TrimSpace(response.Data.FLWRef),
		AccountNumber: accountNumber,
		BankName:      strings.TrimSpace(response.Data.BankName),
		AccountName:   strings.TrimSpace(response.Data.AccountName),
		Currency:      strings.TrimSpace(response.Data.Currency),
		Status:        strings.TrimSpace(response.Data.AccountStatus),
	}, nil
}

// CreatePermanentVirtualAccount opens a static NGN virtual account. Static
// accounts require either a BVN or a NIN, and the tx_ref is the reference every
// incoming transfer will carry.
func (c *FlutterwaveClient) CreatePermanentVirtualAccount(req CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
	req.Email = strings.TrimSpace(req.Email)
	req.TxRef = strings.TrimSpace(req.TxRef)
	req.IdentityKind = strings.ToLower(strings.TrimSpace(req.IdentityKind))
	req.IdentityNumber = strings.TrimSpace(req.IdentityNumber)
	if req.Email == "" || req.TxRef == "" {
		return nil, fmt.Errorf("virtual account email and reference are required")
	}
	if req.IdentityKind != "bvn" && req.IdentityKind != "nin" {
		return nil, fmt.Errorf("virtual account requires a BVN or NIN")
	}
	if req.IdentityNumber == "" {
		return nil, fmt.Errorf("virtual account identity number is required")
	}

	payload := map[string]any{
		"email":          req.Email,
		"tx_ref":         req.TxRef,
		"is_permanent":   true,
		"narration":      req.Narration,
		req.IdentityKind: req.IdentityNumber,
	}
	if strings.TrimSpace(req.Phone) != "" {
		payload["phonenumber"] = strings.TrimSpace(req.Phone)
	}
	if strings.TrimSpace(req.FirstName) != "" {
		payload["firstname"] = strings.TrimSpace(req.FirstName)
	}
	if strings.TrimSpace(req.LastName) != "" {
		payload["lastname"] = strings.TrimSpace(req.LastName)
	}
	if strings.TrimSpace(req.BankCode) != "" {
		payload["bank_code"] = strings.TrimSpace(req.BankCode)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal virtual account request: %w", err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, flutterwaveBaseURL+"/virtual-account-numbers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build virtual account request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// No answer says nothing about whether an account was opened.
		return nil, fmt.Errorf("call flutterwave virtual account: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readFlutterwaveResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		detail := safeProviderMessage(providerMessage(respBody))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			if detail == "" {
				detail = http.StatusText(resp.StatusCode)
			}
			return nil, fmt.Errorf("%w: %s", ErrVirtualAccountAccessDenied, detail)
		}
		if virtualAccountRefused(resp.StatusCode, detail) {
			if detail == "" {
				detail = http.StatusText(resp.StatusCode)
			}
			return nil, fmt.Errorf("%w: %s", ErrVirtualAccountRefused, detail)
		}
		return nil, providerHTTPError("flutterwave virtual account", resp.StatusCode, respBody)
	}

	var parsed virtualAccountResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		// A 2xx we cannot read may still describe a created account.
		return nil, fmt.Errorf("unmarshal virtual account response: %w", err)
	}
	if parsed.Status != "success" {
		detail := safeProviderMessage(parsed.Message)
		if virtualAccountRefused(http.StatusBadRequest, detail) {
			return nil, fmt.Errorf("%w: %s", ErrVirtualAccountRefused, detail)
		}
		return nil, fmt.Errorf("flutterwave rejected the virtual account request: %s", detail)
	}
	details, err := parseVirtualAccount(parsed)
	if err != nil {
		return nil, err
	}
	// The account is found by the tx_ref we sent, not by whatever order_ref the
	// provider chose to echo, because that is the reference its transfer
	// webhooks carry.
	details.TxRef = req.TxRef
	if details.OrderRef == "" {
		details.OrderRef = req.TxRef
	}
	if details.Currency == "" {
		details.Currency = "NGN"
	}
	return details, nil
}

// GetPermanentVirtualAccount looks up a static account by its order reference.
// It is used before retrying a setup whose provider answer was lost, so a
// second account is not opened for the same farmer.
func (c *FlutterwaveClient) GetPermanentVirtualAccount(orderRef string) (*VirtualAccountDetails, error) {
	orderRef = strings.TrimSpace(orderRef)
	if orderRef == "" {
		return nil, fmt.Errorf("virtual account reference is required")
	}
	endpoint := fmt.Sprintf("%s/virtual-account-numbers/%s", flutterwaveBaseURL, url.PathEscape(orderRef))
	httpReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build virtual account lookup request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call flutterwave virtual account lookup: %w", err)
	}
	defer resp.Body.Close()
	body, err := readFlutterwaveResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := safeProviderMessage(providerMessage(body))
		if resp.StatusCode == http.StatusNotFound || notFoundMessage(detail) {
			if detail == "" {
				detail = fmt.Sprintf("provider returned %d", resp.StatusCode)
			}
			return nil, fmt.Errorf("%w: %s", ErrVirtualAccountNotFound, detail)
		}
		return nil, providerHTTPError("flutterwave virtual account lookup", resp.StatusCode, body)
	}
	var parsed virtualAccountResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal virtual account lookup: %w", err)
	}
	if parsed.Status != "success" {
		detail := safeProviderMessage(parsed.Message)
		if notFoundMessage(detail) {
			return nil, fmt.Errorf("%w: %s", ErrVirtualAccountNotFound, detail)
		}
		return nil, fmt.Errorf("flutterwave virtual account lookup failed: %s", detail)
	}
	details, err := parseVirtualAccount(parsed)
	if err != nil {
		return nil, err
	}
	if details.Currency == "" {
		details.Currency = "NGN"
	}
	return details, nil
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
		return nil, providerHTTPError("flutterwave transfer verification", resp.StatusCode, body)
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
