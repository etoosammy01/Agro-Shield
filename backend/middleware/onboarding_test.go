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

func TestRequireAuthEnforcesBothOnboardingSteps(t *testing.T) {
	done := sql.NullTime{Time: time.Now(), Valid: true}
	for _, role := range []string{"farmer", "buyer"} {
		for _, tc := range []struct {
			name          string
			selfie, phone sql.NullTime
			path, want    string
		}{
			{"existing uploaded photo", sql.NullTime{}, sql.NullTime{}, "/dashboard", "/complete-profile"},
			{"verification alone cannot skip selfie", sql.NullTime{}, done, "/marketplace", "/complete-profile"},
			{"selfie only", done, sql.NullTime{}, "/dashboard", "/verify-phone"},
			{"completed", done, done, "/dashboard", ""},
			{"selfie page available", sql.NullTime{}, sql.NullTime{}, "/complete-profile", ""},
			{"phone page delegated to handler", done, sql.NullTime{}, "/verify-phone", ""},
		} {
			t.Run(role+"/"+tc.name, func(t *testing.T) {
				farmer := &models.Farmer{ID: 42, Role: role, PhotoURL: "/static/uploads/farmers/old.jpg", SelfieCapturedAt: tc.selfie, PhoneVerifiedAt: tc.phone}
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
	for _, path := range []string{"/complete-profile", "/verify-phone"} {
		w := httptest.NewRecorder()
		RequireAuth(onboardingFarmerReader{}, func(http.ResponseWriter, *http.Request) { t.Fatal("unauthenticated handler called") })(w, httptest.NewRequest("GET", path, nil))
		if w.Header().Get("Location") != "/login" {
			t.Fatal("missing login redirect")
		}
	}
}
