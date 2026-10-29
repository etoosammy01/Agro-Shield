package otp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"backend/internal/models"
)

const testOTPDevTemplate = "a724614a-3a7c-4625-930e-2a8b45141474"

func testOTPDevProvider(t *testing.T, handler http.HandlerFunc) *OTPDevProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	provider, err := NewOTPDevProvider(OTPDevConfig{
		APIKey: "test-api-key", Sender: "OTP Dev", TemplateID: testOTPDevTemplate, BaseURL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestOTPDevSendsAgrilinksPayloadAndAcceptsNestedCreatedResponse(t *testing.T) {
	provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/verifications" ||
			r.Header.Get("X-OTP-Key") != "test-api-key" ||
			r.Header.Get("Content-Type") != "application/json" ||
			r.Header.Get("Accept") != "application/json" {
			t.Error("incorrect OTP.dev request method, endpoint, or headers")
		}
		var request struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		for name, expected := range map[string]string{
			"channel": "sms", "phone": "2348012345678", "sender": "OTP Dev",
			"template": testOTPDevTemplate, "code": "012345",
		} {
			if request.Data[name] != expected {
				t.Errorf("data.%s = %v, want %s", name, request.Data[name], expected)
			}
		}
		if _, ok := request.Data["code_length"]; ok {
			t.Error("OTP.dev rejects requests containing both code and code_length")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"data": otpDevConfirmation{MessageID: "message-id", Phone: "2348012345678"},
		})
	})
	if err := provider.Send(context.Background(), "+234 801-234-5678", "012345", models.OTPPurposePhoneVerification); err != nil {
		t.Fatal(err)
	}
}

func TestOTPDevAcceptsDocumentedAndLiveConfirmationFormats(t *testing.T) {
	for _, test := range []struct {
		name string
		status int
		body any
	}{
		{"live nested 201", http.StatusCreated, map[string]any{"data": otpDevConfirmation{"message-id", "2348012345678"}}},
		{"nested 200", http.StatusOK, map[string]any{"data": otpDevConfirmation{"message-id", "2348012345678"}}},
		{"documented flat 200", http.StatusOK, otpDevConfirmation{"message-id", "2348012345678"}},
		{"formatted confirmation phone", http.StatusCreated, map[string]any{"data": otpDevConfirmation{"message-id", "+234 801 234 5678"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				json.NewEncoder(w).Encode(test.body)
			})
			if err := provider.Send(context.Background(), "08012345678", "012345", models.OTPPurposePayment); err != nil {
				t.Fatalf("Send() = %v", err)
			}
		})
	}
}

func TestOTPDevRejectsInvalidAndFailedAPIResponses(t *testing.T) {
	for _, test := range []struct {
		name string
		status int
		body string
		want error
	}{
		{"unauthorized", 401, "{\"secret\":\"test-api-key\",\"code\":\"012345\"}", nil},
		{"server failure", 503, "012345 test-api-key", nil},
		{"rate limited", 429, "rate limited", ErrRateLimited},
		{"malformed JSON", 201, "not JSON", nil},
		{"missing confirmation", 201, "{}", nil},
		{"null data", 201, "{\"data\":null}", nil},
		{"array instead of confirmation", 201, "{\"data\":[]}", nil},
		{"missing message ID", 201, "{\"data\":{\"phone\":\"2348012345678\"}}", nil},
		{"different phone", 201, "{\"data\":{\"message_id\":\"id\",\"phone\":\"2348099999999\"}}", nil},
		{"echoed OTP as error code", 400, "{\"errors\":[{\"code\":\"012345\"}]}", nil},
		{"unsafe error code", 400, "{\"errors\":[{\"code\":\"012345 test-api-key\"}]}", nil},
		{"oversized response", 201, strings.Repeat(" ", otpDevResponseLimit+1), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				w.Write([]byte(test.body))
			})
			err := provider.Send(context.Background(), "08012345678", "012345", models.OTPPurposeTransfer)
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("Send() = %v, want failure %v", err, test.want)
			}
			if strings.Contains(err.Error(), "012345") || strings.Contains(err.Error(), "test-api-key") {
				t.Fatal("provider response exposed a code or API key")
			}
		})
	}
}

func TestOTPDevReportsOnlyNumericAPIErrorCodes(t *testing.T) {
	for _, body := range []string{
		"{\"errors\":[{\"code\":\"1136\",\"message\":\"test-api-key 012345\"}]}",
		"{\"errors\":[{\"code\":1136,\"message\":\"test-api-key 012345\"}]}",
	} {
		provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(body))
		})
		err := provider.Send(context.Background(), "08012345678", "012345", models.OTPPurposePayment)
		if err == nil || !strings.Contains(err.Error(), "HTTP 401 (code 1136)") ||
			strings.Contains(err.Error(), "012345") || strings.Contains(err.Error(), "test-api-key") {
			t.Fatalf("unsafe or missing provider diagnostic: %v", err)
		}
	}
}

func TestOTPDevDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Store(true) }))
	defer target.Close()
	provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if err := provider.Send(context.Background(), "08012345678", "012345", models.OTPPurposePayment); err == nil || redirected.Load() {
		t.Fatal("OTP.dev redirect was followed with sensitive request data")
	}
}

type failingOTPTransport struct{}

func (failingOTPTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, &url.Error{Op: "Post", URL: r.URL.String(), Err: errors.New("network unavailable")}
}

func TestOTPDevTransportErrorsDoNotExposeCodes(t *testing.T) {
	provider, err := NewOTPDevProvider(OTPDevConfig{APIKey: "test-api-key", Sender: "OTP Dev", TemplateID: testOTPDevTemplate})
	if err != nil {
		t.Fatal(err)
	}
	provider.client.Transport = failingOTPTransport{}
	err = provider.Send(context.Background(), "08012345678", "012345", models.OTPPurposePayment)
	if err == nil || strings.Contains(err.Error(), "012345") || strings.Contains(err.Error(), "2348012345678") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}

func TestOTPDevHonorsRequestCancellation(t *testing.T) {
	var called atomic.Bool
	provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) { called.Store(true) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := provider.Send(ctx, "08012345678", "012345", models.OTPPurposeWithdrawal); !errors.Is(err, context.Canceled) || called.Load() {
		t.Fatalf("canceled SMS request = %v, provider called = %v", err, called.Load())
	}
}

func TestOTPDevServiceVerifiesLocallyAndPreventsReplay(t *testing.T) {
	for _, purpose := range []string{
		models.OTPPurposePhoneVerification, models.OTPPurposeWithdrawal, models.OTPPurposeTransfer,
		models.OTPPurposePasswordReset, models.OTPPurposePayment,
	} {
		t.Run(purpose, func(t *testing.T) {
			var deliveredCode atomic.Value
			var calls atomic.Int32
			provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || calls.Load() > 1 {
					http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
					return
				}
				var request struct {
					Data otpDevSMSData `json:"data"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				deliveredCode.Store(request.Data.Code)
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]any{
					"data": otpDevConfirmation{MessageID: "message-id", Phone: request.Data.Phone},
				})
			})
			store := &mockStore{}
			service := NewService(store, provider)
			ctx := context.Background()
			if err := service.Issue(ctx, 10, "08012345678", purpose); err != nil {
				t.Fatal(err)
			}
			code := deliveredCode.Load().(string)
			verify := func() error {
				if purpose == models.OTPPurposePhoneVerification {
					return service.VerifyPhone(ctx, 10, "08012345678", code)
				}
				return service.Verify(ctx, 10, purpose, code)
			}
			if err := verify(); err != nil {
				t.Fatal(err)
			}
			if !store.active.ConsumedAt.Valid || (purpose == models.OTPPurposePhoneVerification && !store.verified) {
				t.Fatal("successful verification was not persisted")
			}
			if err := verify(); !errors.Is(err, ErrUnavailable) || calls.Load() != 1 {
				t.Fatalf("used code was replayed or verification called the provider: %v, API calls = %d", err, calls.Load())
			}
		})
	}
}

func TestOTPDevFailedDeliveryInvalidatesCode(t *testing.T) {
	var rejectedCode atomic.Value
	provider := testOTPDevProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Data otpDevSMSData `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&request)
		rejectedCode.Store(request.Data.Code)
		http.Error(w, "delivery unavailable", http.StatusServiceUnavailable)
	})
	store := &mockStore{}
	service := NewService(store, provider)
	ctx := context.Background()
	if err := service.Issue(ctx, 10, "08012345678", models.OTPPurposePayment); err == nil {
		t.Fatal("failed SMS delivery reported success")
	}
	if store.active == nil || !store.active.ConsumedAt.Valid {
		t.Fatal("undelivered code remained active")
	}
	if err := service.Verify(ctx, 10, models.OTPPurposePayment, rejectedCode.Load().(string)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("undelivered code approved a payment: %v", err)
	}
}
