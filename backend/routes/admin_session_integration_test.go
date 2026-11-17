package routes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	app "backend/internal"
	"backend/internal/models"
	"backend/internal/otp"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/internal/testdb"
	"golang.org/x/crypto/bcrypt"
)

func TestAdminPersistentLoginAcrossInstancesAndRevocation(t *testing.T) {
	db := testdb.Open(t)
	farmers := repository.NewFarmerRepository(db)
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	create := func(name, phone string) *models.Farmer {
		t.Helper()
		f := &models.Farmer{FullName: name, Phone: phone, Role: "farmer", Location: "Kano", State: "Kano", Country: "Nigeria", PhotoURL: "/fixture.jpg", PasswordHash: string(hash)}
		if err := farmers.Create(f); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE farmers SET selfie_captured_at=CURRENT_TIMESTAMP,phone_verified_at=CURRENT_TIMESTAMP WHERE id=$1`, f.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE wallets SET transaction_pin_hash='fixture' WHERE farmer_id=$1`, f.ID); err != nil {
			t.Fatal(err)
		}
		return f
	}
	admin := create("Lifecycle administrator", "2348000000301")
	member := create("Lifecycle member", "2348000000302")
	old := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = old })
	instance := func() *http.ServeMux {
		http.DefaultServeMux = http.NewServeMux()
		RegisterRoutes(&app.Container{
			FarmerRepo: repository.NewFarmerRepository(db), Auth: services.NewAuthService(repository.NewFarmerRepository(db)),
			Sessions: services.NewSessionService(repository.NewSessionRepository(db)), Admin: repository.NewAdminRepository(db, []int{admin.ID}),
		}, nil)
		return http.DefaultServeMux
	}
	a, b := instance(), instance()
	request := func(server *http.ServeMux, method, path string, body url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://example.com")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	login := func(server *http.ServeMux, f *models.Farmer) *http.Cookie {
		t.Helper()
		w := request(server, "POST", "/login", url.Values{"phone": {f.Phone}, "password": {"synthetic-password"}}, nil)
		if w.Code != 303 || w.Header().Get("Location") != "/dashboard" || len(w.Result().Cookies()) != 1 {
			t.Fatal("live router login failed")
		}
		return w.Result().Cookies()[0]
	}
	cookie := login(a, admin)
	for _, server := range []*http.ServeMux{a, b, instance()} {
		if request(server, "GET", "/api/admin/users", nil, cookie).Code != 200 {
			t.Fatal("persistent login failed across independent/reconstructed application instances")
		}
	}
	sum := sha256.Sum256([]byte(cookie.Value))
	tokenHash := hex.EncodeToString(sum[:])
	var stored string
	if err := db.QueryRow(`SELECT session_token_hash FROM sessions WHERE session_token_hash=$1`, tokenHash).Scan(&stored); err != nil || stored == cookie.Value {
		t.Fatal("session was not stored as a hash")
	}
	memberCookie := login(a, member)
	if request(b, "GET", "/api/admin/users", nil, memberCookie).Code != 403 {
		t.Fatal("member acquired admin access")
	}
	change := func(action string, version int) {
		t.Helper()
		w := request(a, "POST", "/admin/users/change", url.Values{"target_id": {fmt.Sprint(member.ID)}, "version": {fmt.Sprint(version)}, "action": {action}, "reason": {"review_cleared"}}, cookie)
		if w.Code != 303 {
			t.Fatalf("%s failed: %d", action, w.Code)
		}
	}
	change("suspend", 0)
	change("reactivate", 1)
	if request(b, "GET", "/api/admin/users", nil, memberCookie).Code != 303 {
		t.Fatal("reactivation restored an old session")
	}
	change("role_administrator", 2)
	reviewerCookie := login(b, member)
	if request(a, "GET", "/api/admin/users", nil, reviewerCookie).Code != 200 {
		t.Fatal("fresh administrator session rejected")
	}
	change("role_none", 3)
	if request(a, "GET", "/api/admin/users", nil, reviewerCookie).Code != 403 {
		t.Fatal("removed staff role retained access")
	}
	expired := login(a, admin)
	expiredSum := sha256.Sum256([]byte(expired.Value))
	if _, err := db.Exec(`UPDATE sessions SET expires_at_utc=CURRENT_TIMESTAMP-interval '1 second' WHERE session_token_hash=$1`, hex.EncodeToString(expiredSum[:])); err != nil {
		t.Fatal(err)
	}
	if request(b, "GET", "/api/admin/users", nil, expired).Code != 303 {
		t.Fatal("server accepted an expired session")
	}
	if request(b, "GET", "/logout", nil, cookie).Code != 303 || request(a, "GET", "/api/admin/users", nil, cookie).Code != 303 {
		t.Fatal("logout was not revoked across instances")
	}
}

type adminRecoveryDelivery struct {
	code string
}

func (p *adminRecoveryDelivery) Send(_ context.Context, _, code, _ string) error {
	p.code = code
	return nil
}

func TestAdminPasswordResetRevokesLoginsAcrossInstances(t *testing.T) {
	db := testdb.Open(t)
	farmers := repository.NewFarmerRepository(db)
	hash, err := bcrypt.GenerateFromPassword([]byte("original-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	admin := &models.Farmer{FullName: "Recovery administrator", Phone: "2348000000303", Role: "farmer", Location: "Kano", State: "Kano", Country: "Nigeria", PhotoURL: "/fixture.jpg", PasswordHash: string(hash)}
	if err := farmers.Create(admin); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE farmers SET selfie_captured_at=CURRENT_TIMESTAMP,phone_verified_at=CURRENT_TIMESTAMP WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE wallets SET transaction_pin_hash='fixture' WHERE farmer_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	delivery := &adminRecoveryDelivery{}
	old := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = old })
	instance := func() *http.ServeMux {
		http.DefaultServeMux = http.NewServeMux()
		RegisterRoutes(&app.Container{
			FarmerRepo: repository.NewFarmerRepository(db), Auth: services.NewAuthService(repository.NewFarmerRepository(db)),
			Sessions: services.NewSessionService(repository.NewSessionRepository(db)), Admin: repository.NewAdminRepository(db, []int{admin.ID}),
			OTP: otp.NewService(repository.NewOTPRepository(db), delivery),
		}, nil)
		return http.DefaultServeMux
	}
	a, b := instance(), instance()
	request := func(server *http.ServeMux, method, path string, body url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://example.com")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	login := func(server *http.ServeMux, password string) *http.Cookie {
		t.Helper()
		w := request(server, "POST", "/login", url.Values{"phone": {admin.Phone}, "password": {password}}, nil)
		if w.Code != 303 || w.Header().Get("Location") != "/dashboard" || len(w.Result().Cookies()) != 1 {
			t.Fatal("password recovery fixture login failed")
		}
		return w.Result().Cookies()[0]
	}
	first, second := login(a, "original-password"), login(b, "original-password")
	w := request(a, "POST", "/forgot-password", url.Values{"phone": {admin.Phone}, "action": {"send_code"}}, nil)
	if w.Code != 200 || delivery.code == "" {
		t.Fatal("recovery code was not issued through the synthetic transport")
	}
	reset := url.Values{"phone": {admin.Phone}, "code": {"000000"}, "new_password": {"replacement-password"}, "confirm_password": {"replacement-password"}}
	if delivery.code == "000000" {
		reset.Set("code", "111111")
	}
	if request(a, "POST", "/forgot-password", reset, nil).Code != 200 {
		t.Fatal("invalid-code recovery response failed")
	}
	if request(b, "GET", "/api/admin/users", nil, first).Code != 200 {
		t.Fatal("unsuccessful reset invalidated an existing login")
	}
	reset.Set("code", delivery.code)
	w = request(a, "POST", "/forgot-password", reset, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Password updated") {
		t.Fatal("verified password reset failed")
	}
	for _, cookie := range []*http.Cookie{first, second} {
		for _, server := range []*http.ServeMux{a, b, instance()} {
			if request(server, "GET", "/api/admin/users", nil, cookie).Code != 303 {
				t.Fatal("old administrator login survived password reset")
			}
		}
	}
	if w := request(b, "POST", "/login", url.Values{"phone": {admin.Phone}, "password": {"original-password"}}, nil); len(w.Result().Cookies()) != 0 {
		t.Fatal("old password acquired a login after reset")
	}
	fresh := login(b, "replacement-password")
	if request(a, "GET", "/api/admin/users", nil, fresh).Code != 200 {
		t.Fatal("new password did not restore authorized admin access")
	}
	if request(a, "POST", "/forgot-password", reset, nil).Code != 200 || request(b, "GET", "/api/admin/users", nil, fresh).Code != 200 {
		t.Fatal("reusing the consumed recovery code invalidated the new login")
	}
}
