package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/session"
)

type onboardingFarmerReader struct{ farmer *models.Farmer }

func (f onboardingFarmerReader) GetByID(int) (*models.Farmer, error) { return f.farmer, nil }

type onboardingSessionValidator map[string]int

func (v onboardingSessionValidator) Validate(_ context.Context, token string) (int, error) {
	farmerID, ok := v[token]
	if !ok {
		return 0, session.ErrNotFound
	}
	return farmerID, nil
}

func TestRequireAuthEnforcesAllOnboardingSteps(t *testing.T) {
	done := sql.NullTime{Time: time.Now(), Valid: true}
	for _, role := range []string{"farmer", "buyer"} {
		for _, tc := range []struct {
			name          string
			selfie, phone sql.NullTime
			pin           bool
			path, want    string
		}{
			{"existing uploaded photo", sql.NullTime{}, sql.NullTime{}, false, "/dashboard", "/complete-profile"},
			{"PIN and verification cannot skip selfie", sql.NullTime{}, done, true, "/marketplace", "/complete-profile"},
			{"PIN cannot skip phone verification", done, sql.NullTime{}, true, "/wallet", "/verify-phone"},
			{"selfie only", done, sql.NullTime{}, false, "/dashboard", "/verify-phone"},
			{"verified phone needs wallet PIN", done, done, false, "/wallet/transfer", "/create-transaction-pin"},
			{"completed", done, done, true, "/dashboard", ""},
			{"selfie page available", sql.NullTime{}, sql.NullTime{}, false, "/complete-profile", ""},
			{"phone page delegated to handler", done, sql.NullTime{}, false, "/verify-phone", ""},
			{"PIN page delegated to handler", done, done, false, "/create-transaction-pin", ""},
		} {
			t.Run(role+"/"+tc.name, func(t *testing.T) {
				farmer := &models.Farmer{ID: 42, Role: role, PhotoURL: "/static/uploads/farmers/old.jpg", SelfieCapturedAt: tc.selfie, PhoneVerifiedAt: tc.phone, HasTransactionPIN: tc.pin}
				token := "onboarding-session-token"
				authenticator := NewAuthenticator(
					onboardingFarmerReader{farmer},
					onboardingSessionValidator{token: farmer.ID},
				)
				r := httptest.NewRequest(http.MethodGet, tc.path, nil)
				r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
				w := httptest.NewRecorder()
				called := false
				authenticator.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
					called = true
					if current, ok := FarmerFromContext(r); !ok || current.ID != farmer.ID {
						t.Fatal("authenticated account missing")
					}
					w.WriteHeader(http.StatusNoContent)
				})(w, r)
				if tc.want != "" {
					if called || w.Code != http.StatusSeeOther || w.Header().Get("Location") != tc.want {
						t.Fatalf("status=%d location=%q called=%v", w.Code, w.Header().Get("Location"), called)
					}
				} else if !called || w.Code != http.StatusNoContent {
					t.Fatalf("handler blocked: status=%d", w.Code)
				}
			})
		}
	}
}

func TestRequireAuthOnboardingStillRequiresSession(t *testing.T) {
	authenticator := NewAuthenticator(onboardingFarmerReader{}, onboardingSessionValidator{})
	for _, tc := range []struct {
		name        string
		cookie      *http.Cookie
		wantCleared bool
	}{
		{"missing cookie", nil, false},
		{"invalid token", &http.Cookie{Name: sessionCookieName, Value: "unknown-token"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/complete-profile", "/verify-phone", "/create-transaction-pin"} {
				r := httptest.NewRequest("GET", path, nil)
				if tc.cookie != nil {
					r.AddCookie(tc.cookie)
				}
				w := httptest.NewRecorder()
				authenticator.RequireAuth(func(http.ResponseWriter, *http.Request) {
					t.Fatal("unauthenticated handler called")
				})(w, r)
				if w.Header().Get("Location") != "/login" {
					t.Fatal("missing login redirect")
				}
				if tc.wantCleared {
					cookies := w.Result().Cookies()
					if len(cookies) != 1 || cookies[0].MaxAge != -1 {
						t.Fatal("invalid session cookie was not cleared")
					}
				}
			}
		})
	}
}
