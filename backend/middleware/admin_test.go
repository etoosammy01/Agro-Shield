package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/models"
)

func TestSuspensionAndReactivationDoNotRestoreOldSessions(t *testing.T) {
	done := sql.NullTime{Time: time.Now(), Valid: true}
	f := &models.Farmer{ID: 8102, Role: "farmer", PhotoURL: "/fixture.jpg", SelfieCapturedAt: done, PhoneVerifiedAt: done, HasTransactionPIN: true}
	old := CreateSession(f.ID)
	t.Cleanup(func() { DeleteFarmerSessions(f.ID) })
	check := func(token string, want int) {
		t.Helper()
		req := httptest.NewRequest("GET", "/dashboard", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		w := httptest.NewRecorder()
		RequireAuth(onboardingFarmerReader{f}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })(w, req)
		if w.Code != want {
			t.Fatalf("status=%d, want %d", w.Code, want)
		}
	}
	check(old, 204)
	f.SuspendedAt = done
	f.SessionsRevokedAt = done
	check(old, http.StatusForbidden)
	// A login that read the account before suspension may finish afterwards.
	// Its old revision must remain invalid even after reactivation.
	racing := CreateSession(f.ID)
	f.SuspendedAt = sql.NullTime{}
	check(racing, http.StatusForbidden)
	newSession := CreateSession(f.ID, f.SessionsRevokedAt.Time)
	check(newSession, 204)
	f.SuspendedAt = sql.NullTime{Time: time.Now(), Valid: true}
	check(newSession, http.StatusForbidden)
}
