package services

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeTransport stands in for the network so the Flutterwave client can be
// exercised without leaving the process. Tests live in package services, which
// gives access to the client's unexported httpClient, so this needs no
// production hook - only the URL host is ignored, since flutterwaveBaseURL is
// a fixed const.
type fakeTransport struct {
	status int
	body   string

	// err, when set, stands in for a request that got no answer at all - a
	// timeout or a dropped connection.
	err error

	// captured is filled in by RoundTrip so a test can assert on what was
	// actually sent, not just on what came back.
	captured *capturedRequest
}

type capturedRequest struct {
	method string
	path   string
	auth   string

	// body holds the decoded request fields. It is map[string]any rather than
	// map[string]string because payloads mix types - a transfer sends its amount
	// as a number - and decoding into strings would fail on the whole document,
	// leaving this empty and quietly passing assertions about fields that were
	// never decoded.
	body map[string]any
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.captured != nil {
		captured := &capturedRequest{
			method: req.Method,
			path:   req.URL.Path,
			auth:   req.Header.Get("Authorization"),
		}
		if req.Body != nil {
			raw, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			// Only the string fields matter here; a failure to parse is left as
			// an empty map so the request assertions fail loudly rather than
			// the comparison panicking.
			_ = json.Unmarshal(raw, &captured.body)
		}
		*f.captured = *captured
	}

	body := f.body
	if body == "" {
		body = "{}"
	}
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func clientWithTransport(f *fakeTransport) *FlutterwaveClient {
	client := NewFlutterwaveClient("test-secret-key", "test-hash", "http://localhost/callback", "card")
	client.httpClient = &http.Client{Transport: f}
	return client
}

func TestResolveAccountReturnsTheRegisteredName(t *testing.T) {
	captured := &capturedRequest{}
	client := clientWithTransport(&fakeTransport{
		status: http.StatusOK,
		body: `{"status":"success","message":"Account resolved","data":{
			"account_number":"0690000032","account_name":"  ADAEZE OKONKWO  "}}`,
		captured: captured,
	})

	resolved, err := client.ResolveAccount("0690000032", "044")
	if err != nil {
		t.Fatalf("ResolveAccount() error = %v", err)
	}
	if resolved.AccountName != "ADAEZE OKONKWO" {
		t.Errorf("AccountName = %q, want the trimmed %q", resolved.AccountName, "ADAEZE OKONKWO")
	}
	if resolved.AccountNumber != "0690000032" {
		t.Errorf("AccountNumber = %q, want %q", resolved.AccountNumber, "0690000032")
	}

	if captured.method != http.MethodPost {
		t.Errorf("method = %q, want POST", captured.method)
	}
	if captured.path != "/v3/accounts/resolve" {
		t.Errorf("path = %q, want /v3/accounts/resolve", captured.path)
	}
	if captured.auth != "Bearer test-secret-key" {
		t.Errorf("Authorization = %q, want the bearer secret key", captured.auth)
	}
	// The bank code is what makes the call meaningful: without it the provider
	// has nothing to resolve against, so it must be on the wire under the name
	// Flutterwave expects.
	if captured.body["account_number"] != "0690000032" || captured.body["account_bank"] != "044" {
		t.Errorf("body = %v, want account_number and account_bank", captured.body)
	}
}

// Every failure below must be an error, because the fan-out reads a nil error
// as "this bank holds the account". A failure that slipped through as success
// would invent a bank match and show the user a name that is not theirs.
func TestResolveAccountRejectsFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "non-200 status",
			status: http.StatusUnauthorized,
			body:   `{"status":"error","message":"Invalid authorization key"}`,
		},
		{
			name:   "provider reports an error with a 200",
			status: http.StatusOK,
			body:   `{"status":"error","message":"Account not found"}`,
		},
		{
			// A success with no name is not proof the account exists. Treating it
			// as a match would list a bank with a blank holder.
			name:   "success without an account name",
			status: http.StatusOK,
			body:   `{"status":"success","message":"ok","data":{"account_number":"0690000032","account_name":""}}`,
		},
		{
			name:   "success with only whitespace for a name",
			status: http.StatusOK,
			body:   `{"status":"success","message":"ok","data":{"account_number":"0690000032","account_name":"   "}}`,
		},
		{
			name:   "malformed JSON",
			status: http.StatusOK,
			body:   `{"status":"success","data":`,
		},
		{
			name:   "empty body",
			status: http.StatusOK,
			body:   ` `,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientWithTransport(&fakeTransport{status: test.status, body: test.body})
			resolved, err := client.ResolveAccount("0690000032", "044")
			if err == nil {
				t.Fatalf("ResolveAccount() = %+v, want an error", resolved)
			}
			if resolved != nil {
				t.Errorf("ResolveAccount() returned %+v alongside an error", resolved)
			}
		})
	}
}

// The sweep relies on this: a bank that does not hold the number answers with
// an error, so the client must refuse to build a request it cannot make.
func TestResolveAccountRequiresBothInputs(t *testing.T) {
	client := clientWithTransport(&fakeTransport{status: http.StatusOK})

	for _, test := range []struct{ name, number, code string }{
		{name: "no account number", number: "", code: "044"},
		{name: "no bank code", number: "0690000032", code: ""},
		{name: "neither", number: "", code: ""},
		{name: "whitespace only", number: "   ", code: "  "},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.ResolveAccount(test.number, test.code); err == nil {
				t.Fatal("ResolveAccount() succeeded without both inputs")
			}
		})
	}
}

// A response larger than the cap must be refused rather than buffered whole.
func TestResolveAccountRejectsAnOversizedResponse(t *testing.T) {
	client := clientWithTransport(&fakeTransport{
		status: http.StatusOK,
		body:   `{"status":"success","data":{"account_name":"` + strings.Repeat("A", maxFlutterwaveResponseBytes) + `"}}`,
	})
	if _, err := client.ResolveAccount("0690000032", "044"); err == nil {
		t.Fatal("ResolveAccount() accepted a response over the size cap")
	}
}

// The two answers a verification turns on, pinned to what the live API actually
// sends. A number it does not hold comes back as 400 with "invalid account" -
// not the 404 the documentation implies - and a bad key comes back as 401.
// Reading the second as the first would tell every farmer their account number
// is wrong because our credential expired.
func TestResolveAccountSeparatesAMissingAccountFromAFailedRequest(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		notHeld bool
	}{
		{
			name:    "the account is not at this bank",
			status:  http.StatusBadRequest,
			body:    `{"status":"error","message":"invalid account","data":null}`,
			notHeld: true,
		},
		{
			name:    "a 404 with nothing readable in the body",
			status:  http.StatusNotFound,
			body:    ``,
			notHeld: true,
		},
		{
			name:    "the account is not at this bank, reported with a 200",
			status:  http.StatusOK,
			body:    `{"status":"error","message":"Account not found","data":null}`,
			notHeld: true,
		},
		{
			name:   "the key is rejected",
			status: http.StatusUnauthorized,
			body:   `{"status":"error","message":"Invalid authorization key","data":null}`,
		},
		{
			name:   "the request is missing its bank code",
			status: http.StatusBadRequest,
			body:   `{"status":"error","message":"destbankcode is not allowed to be empty","data":null}`,
		},
		{
			// The provider's own catch-all. A hiccup says this as readily as a
			// missing account does, so it must never be read as a "no".
			name:   "the provider could not answer",
			status: http.StatusBadRequest,
			body:   `{"status":"error","message":"cannot resolve account number, please try again","data":null}`,
		},
		{
			name:   "the provider is rate limiting",
			status: http.StatusTooManyRequests,
			body:   `{"status":"error","message":"too many requests","data":null}`,
		},
		{
			name:   "the provider is broken",
			status: http.StatusInternalServerError,
			body:   `{"status":"error","message":"internal server error","data":null}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientWithTransport(&fakeTransport{status: test.status, body: test.body})

			_, err := client.ResolveAccount("0690000032", "044")
			if err == nil {
				t.Fatal("ResolveAccount() accepted a failure as an answer")
			}
			if got := errors.Is(err, ErrAccountNotResolved); got != test.notHeld {
				t.Errorf("errors.Is(err, ErrAccountNotResolved) = %v, want %v (err = %v)", got, test.notHeld, err)
			}
		})
	}
}

// The sweep treats ErrAccountNotResolved as "not this bank" and everything else
// as "could not ask", so the classifier is the single point where the two are
// told apart. Pinned directly so the wording cannot drift unnoticed.
func TestNotFoundMessageRecognisesOnlyAnswersAboutTheAccount(t *testing.T) {
	notHeld := []string{
		"invalid account",
		"Invalid Account",
		"  invalid account  ",
		"Account not found",
		"account does not exist",
	}
	failed := []string{
		"",
		"   ",
		"Invalid authorization key",
		"destbankcode is not allowed to be empty",
		"cannot resolve account number, please try again",
		"too many requests",
		"internal server error",
	}

	for _, message := range notHeld {
		if !notFoundMessage(message) {
			t.Errorf("notFoundMessage(%q) = false, want true", message)
		}
	}
	for _, message := range failed {
		if notFoundMessage(message) {
			t.Errorf("notFoundMessage(%q) = true, want false - this is a failure to ask, not an answer", message)
		}
	}
}

// Whether a failed transfer was created is a question about money, so the
// classifier is pinned to the answers that actually settle it. Reading an
// unknown fate as "refused" makes the caller refund a farmer whose payment
// Flutterwave may also be sending, so only answers that rule a transfer out may
// count.
func TestCreateTransferSeparatesARefusalFromAnUnknownFate(t *testing.T) {
	const created = `{"status":"success","message":"Transfer Queued Successfully","data":{"id":12345}}`

	tests := []struct {
		name    string
		status  int
		body    string
		err     error
		refused bool
	}{
		{
			name:    "the key is rejected",
			status:  http.StatusUnauthorized,
			body:    `{"status":"error","message":"Invalid authorization key","data":null}`,
			refused: true,
		},
		{
			name:    "the key is not allowed to transfer",
			status:  http.StatusForbidden,
			body:    `{"status":"error","message":"Transfer not permitted","data":null}`,
			refused: true,
		},
		{
			name:    "the provider answered with a rejection",
			status:  http.StatusOK,
			body:    `{"status":"error","message":"Insufficient balance","data":null}`,
			refused: true,
		},
		{
			// The provider's catch-all. A hiccup says this as readily as a real
			// refusal does, and the two must not be read the same way.
			name:   "the provider could not process the request",
			status: http.StatusBadRequest,
			body:   `{"status":"error","message":"cannot process transfer, please try again","data":null}`,
		},
		{
			// Also ambiguous: a 400 is where validation failures live, but the
			// message alone does not prove which kind this was.
			name:   "a validation failure",
			status: http.StatusBadRequest,
			body:   `{"status":"error","message":"account_bank is required","data":null}`,
		},
		{
			name:   "the provider is rate limiting",
			status: http.StatusTooManyRequests,
			body:   `{"status":"error","message":"too many requests","data":null}`,
		},
		{
			// A server that failed mid-call may have created the transfer first.
			name:   "the provider is broken",
			status: http.StatusInternalServerError,
			body:   `{"status":"error","message":"internal server error","data":null}`,
		},
		{
			// A success with no id may describe a transfer that exists and
			// cannot be named from this response.
			name:   "a success with no transfer id",
			status: http.StatusOK,
			body:   `{"status":"success","message":"ok","data":{"id":0}}`,
		},
		{
			name:   "a success that cannot be parsed",
			status: http.StatusOK,
			body:   `{"status":"success","data":`,
		},
		{
			name:   "the request got no answer",
			status: http.StatusOK,
			err:    errors.New("dial tcp: i/o timeout"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientWithTransport(&fakeTransport{status: test.status, body: test.body, err: test.err})

			transferID, err := client.CreateTransfer(CreateTransferRequest{
				BankCode: "044", AccountNumber: "0690000032",
				Amount: 500000, Currency: "NGN", Reference: "WITHDRAW-7-1",
			})
			if err == nil {
				t.Fatalf("CreateTransfer() = %q, want an error", transferID)
			}
			if got := errors.Is(err, ErrTransferNotCreated); got != test.refused {
				t.Errorf("errors.Is(err, ErrTransferNotCreated) = %v, want %v (err = %v)", got, test.refused, err)
			}
		})
	}

	t.Run("a created transfer is not an error", func(t *testing.T) {
		client := clientWithTransport(&fakeTransport{status: http.StatusOK, body: created})
		transferID, err := client.CreateTransfer(CreateTransferRequest{
			BankCode: "044", AccountNumber: "0690000032",
			Amount: 500000, Currency: "NGN", Reference: "WITHDRAW-7-1",
		})
		if err != nil {
			t.Fatalf("CreateTransfer() error = %v", err)
		}
		if transferID != "12345" {
			t.Errorf("transferID = %q, want %q", transferID, "12345")
		}
	})

	// The amount and beneficiary must still reach the provider, since this is
	// the call that moves the money.
	t.Run("the payout details are sent", func(t *testing.T) {
		captured := &capturedRequest{}
		client := clientWithTransport(&fakeTransport{status: http.StatusOK, body: created, captured: captured})
		if _, err := client.CreateTransfer(CreateTransferRequest{
			BankCode: "044", AccountNumber: "0690000032", AccountName: "Adaeze Okonkwo",
			Amount: 500000, Currency: "NGN", Reference: "WITHDRAW-7-1",
		}); err != nil {
			t.Fatalf("CreateTransfer() error = %v", err)
		}
		if captured.path != "/v3/transfers" || captured.method != http.MethodPost {
			t.Errorf("request = %s %s, want POST /v3/transfers", captured.method, captured.path)
		}
		if captured.body["reference"] != "WITHDRAW-7-1" {
			t.Errorf("reference = %q, want the withdrawal reference", captured.body["reference"])
		}
	})
}
