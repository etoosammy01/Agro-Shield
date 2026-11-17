package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/onboarding"
)

type developmentSessionValidator struct{ userID int }

func (v developmentSessionValidator) Validate(context.Context, string) (int, error) {
	return v.userID, nil
}

func TestDevelopmentOnboardingSkipKeepsAuthenticationAndAccountRestrictions(t *testing.T) {
	t.Cleanup(func() { onboarding.Configure(false, nil) })
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "process session", true: "persistent session"}[persistent], func(t *testing.T) {
			farmer := &models.Farmer{ID: 14, Role: "farmer"}
			token := CreateSession(farmer.ID)
			t.Cleanup(func() { DeleteSession(token) })
			wrap := func(next http.HandlerFunc) http.HandlerFunc {
				if persistent {
					return NewAuthenticator(onboardingFarmerReader{farmer}, developmentSessionValidator{farmer.ID}).RequireAuth(next)
				}
				return RequireAuth(onboardingFarmerReader{farmer}, next)
			}
			request := func(authenticated bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/dashboard", nil)
				if authenticated {
					r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
				}
				w := httptest.NewRecorder()
				wrap(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })(w, r)
				return w
			}
			onboarding.Configure(true, []int{farmer.ID})
			if w := request(true); w.Code != 204 {
				t.Fatalf("development account could not reach dashboard: %d", w.Code)
			}
			if w := request(false); w.Code != 303 || w.Header().Get("Location") != "/login" {
				t.Fatal("skip allowed access without a session")
			}
			onboarding.Configure(true, []int{15})
			if w := request(true); w.Header().Get("Location") != "/complete-profile" {
				t.Fatal("skip applied to an unlisted account")
			}
			onboarding.Configure(true, nil)
			if w := request(true); w.Code != 204 {
				t.Fatal("all-account development skip was not applied")
			}
			onboarding.Configure(false, nil)
			if w := request(true); w.Header().Get("Location") != "/complete-profile" {
				t.Fatal("disabling skip did not restore onboarding")
			}
			if farmer.SelfieCapturedAt.Valid || farmer.PhoneVerifiedAt.Valid || farmer.HasTransactionPIN {
				t.Fatal("skip fabricated stored onboarding evidence")
			}
			onboarding.Configure(true, []int{farmer.ID})
			farmer.SuspendedAt = sql.NullTime{Valid: true, Time: time.Now()}
			if w := request(true); w.Code != 403 {
				t.Fatal("skip restored access for a suspended account")
			}
		})
	}
}
