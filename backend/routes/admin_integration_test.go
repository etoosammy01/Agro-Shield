package routes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"backend/handlers"
	app "backend/internal"
	"backend/internal/models"
	"backend/internal/onboarding"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/internal/testdb"
	"backend/middleware"

	"golang.org/x/crypto/bcrypt"
)

func TestDevelopmentOnboardingLoginReachesDashboardAndKeepsAdminPermissions(t *testing.T) {
	db := testdb.Open(t)
	farmers := repository.NewFarmerRepository(db)
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	create := func(name, phone string) *models.Farmer {
		t.Helper()
		f := &models.Farmer{FullName: name, Phone: phone, Role: "farmer", Location: "Kano", State: "Kano", Country: "Nigeria", PasswordHash: string(hash), PhotoURL: "/fixture.jpg"}
		if err := farmers.Create(f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	admin := create("Development administrator", "2348000000102")
	member := create("Development member", "2348000000103")
	onboarding.Configure(true, []int{admin.ID, member.ID})
	t.Cleanup(func() { onboarding.Configure(false, nil) })
	sessions := services.NewSessionService(repository.NewSessionRepository(db))
	login := handlers.NewLoginHandler(services.NewAuthService(farmers), sessions)
	signIn := func(f *models.Farmer, password string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/login", strings.NewReader((url.Values{"phone": {f.Phone}, "password": {password}}).Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		login.LoginHandler(w, r)
		return w
	}
	adminLogin := signIn(admin, "synthetic-password")
	if adminLogin.Code != 303 || adminLogin.Header().Get("Location") != "/dashboard" {
		t.Fatal("login did not go directly to dashboard")
	}
	cookies := adminLogin.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("login did not create a session")
	}
	old := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	t.Cleanup(func() { http.DefaultServeMux = old })
	registerAdminRoutes(&app.Container{FarmerRepo: farmers, Sessions: sessions, Admin: repository.NewAdminRepository(db, []int{admin.ID})}, handlers.NewWalletHandler(nil, nil, nil))
	request := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		if path == "/dashboard" {
			middleware.NewAuthenticator(farmers, sessions).RequireAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })(w, r)
		} else {
			http.DefaultServeMux.ServeHTTP(w, r)
		}
		return w
	}
	if request("/dashboard", cookies[0]).Code != 204 || request("/api/admin/users", cookies[0]).Code != 200 {
		t.Fatal("authenticated development administrator was blocked")
	}
	memberLogin := signIn(member, "synthetic-password")
	memberCookies := memberLogin.Result().Cookies()
	if memberLogin.Header().Get("Location") != "/dashboard" || len(memberCookies) != 1 {
		t.Fatal("selected development member could not log in")
	}
	if request("/api/admin/users", memberCookies[0]).Code != 403 {
		t.Fatal("onboarding skip granted administrative permissions")
	}
	if wrong := signIn(admin, "wrong-password"); wrong.Header().Get("Location") == "/dashboard" || len(wrong.Result().Cookies()) != 0 {
		t.Fatal("onboarding skip bypassed password verification")
	}
	stored, err := farmers.GetByID(admin.ID)
	if err != nil || stored.SelfieCapturedAt.Valid || stored.PhoneVerifiedAt.Valid || stored.HasTransactionPIN {
		t.Fatal("login skip fabricated onboarding evidence")
	}
	onboarding.Configure(false, nil)
	if request("/dashboard", cookies[0]).Header().Get("Location") != "/complete-profile" {
		t.Fatal("normal onboarding could not be restored")
	}
	onboarding.Configure(true, []int{admin.ID})
	if _, err := db.Exec(`UPDATE farmers SET suspended_at=CURRENT_TIMESTAMP WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if suspended := signIn(admin, "synthetic-password"); suspended.Header().Get("Location") == "/dashboard" || len(suspended.Result().Cookies()) != 0 {
		t.Fatal("onboarding skip allowed a suspended account to log in")
	}
}

func TestAdminRoutesRejectUnprivilegedAndCrossSiteRequests(t *testing.T) {
	db := testdb.Open(t)
	farmers := repository.NewFarmerRepository(db)
	f := &models.Farmer{FullName: "Route fixture", Phone: "2348000000101", Role: "farmer", Location: "Kano", State: "Kano", Country: "Nigeria", PasswordHash: "fixture", PhotoURL: "/fixture.jpg"}
	if err := farmers.Create(f); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE farmers SET selfie_captured_at=CURRENT_TIMESTAMP,phone_verified_at=CURRENT_TIMESTAMP WHERE id=$1`, f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE wallets SET transaction_pin_hash='fixture' WHERE farmer_id=$1`, f.ID); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	t.Cleanup(func() { http.DefaultServeMux = old })
	sessions := services.NewSessionService(repository.NewSessionRepository(db))
	registerAdminRoutes(&app.Container{FarmerRepo: farmers, Sessions: sessions, Admin: repository.NewAdminRepository(db, nil)}, handlers.NewWalletHandler(nil, nil, nil))
	token, _, err := sessions.Create(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, origin string, loggedIn bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(fmt.Sprintf("target_id=%d&version=0&action=role_administrator&reason=staff_assignment", f.ID)))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if loggedIn {
			r.AddCookie(&http.Cookie{Name: "agroshield_session", Value: token})
		}
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/admin", "/admin/users", "/admin/listings", "/admin/audit", "/api/admin/overview", "/api/admin/users", "/api/admin/listings", "/api/admin/audit", "/admin/wallet-withdrawals"} {
		if w := request("GET", path, "", true); w.Code != 403 {
			t.Fatalf("ordinary user GET %s = %d", path, w.Code)
		}
		if w := request("GET", path, "", false); w.Code != 303 || w.Header().Get("Location") != "/login" {
			t.Fatalf("anonymous admin route %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/admin/users/change", "/admin/listings/change", "/api/admin/users/change", "/api/admin/listings/change", "/admin/wallet-admins", "/admin/wallet-withdrawals/approve", "/admin/wallet-withdrawals/reject", "/admin/wallet-withdrawals/reconcile", "/admin/wallet-withdrawals/return-to-queue", "/admin/wallet-withdrawals/record-transfer", "/admin/wallet-transfers/approve", "/admin/wallet-transfers/reject", "/admin/wallets/repair"} {
		if w := request("POST", path, "http://example.com", true); w.Code != 403 {
			t.Fatalf("ordinary user POST %s = %d", path, w.Code)
		}
	}
	if _, err := db.Exec(`UPDATE farmers SET admin_role='administrator' WHERE id=$1`, f.ID); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "https://attacker.invalid"} {
		if w := request("POST", "/admin/users/change", origin, true); w.Code != 403 {
			t.Fatal("admin mutation accepted without same-origin evidence")
		}
	}
	if w := request("GET", "/admin/users", "", true); w.Code != 200 {
		t.Fatalf("authorized directory: %d %s", w.Code, w.Body.String())
	}
	if _, err := db.Exec(`UPDATE farmers SET admin_role='',is_admin=true WHERE id=$1`, f.ID); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/admin/wallet-admins", "http://example.com", true); w.Code != 403 {
		t.Fatal("wallet-only approver used legacy route to escalate")
	}
	if w := request("GET", "/admin", "", true); w.Code != 303 || w.Header().Get("Location") != "/admin/wallet-withdrawals" {
		t.Fatal("wallet-only console entry did not reach approvals")
	}
}
