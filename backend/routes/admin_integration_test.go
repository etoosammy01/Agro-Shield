package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/handlers"
	app "backend/internal"
	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/testdb"
	"backend/middleware"
)

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
	registerAdminRoutes(&app.Container{FarmerRepo: farmers, Admin: repository.NewAdminRepository(db, nil)}, handlers.NewWalletHandler(nil, nil, nil))
	token := middleware.CreateSession(f.ID)
	t.Cleanup(func() { middleware.DeleteSession(token) })
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
