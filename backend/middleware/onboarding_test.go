package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/models"
)

type onboardingFarmerReader struct{ farmer *models.Farmer }

func (f onboardingFarmerReader) GetByID(int) (*models.Farmer, error) { return f.farmer, nil }

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
				session := CreateSession(farmer.ID)
				t.Cleanup(func() { DeleteSession(session) })
				r := httptest.NewRequest(http.MethodGet, tc.path, nil)
				r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
				w := httptest.NewRecorder()
				called := false
				RequireAuth(onboardingFarmerReader{farmer}, func(w http.ResponseWriter, r *http.Request) {
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
	for _, path := range []string{"/complete-profile", "/verify-phone", "/create-transaction-pin"} {
		w := httptest.NewRecorder()
		RequireAuth(onboardingFarmerReader{}, func(http.ResponseWriter, *http.Request) { t.Fatal("unauthenticated handler called") })(w, httptest.NewRequest("GET", path, nil))
		if w.Header().Get("Location") != "/login" {
			t.Fatal("missing login redirect")
		}
	}
}
