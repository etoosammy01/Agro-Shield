package routes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/handlers"
	app "backend/internal"
	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/internal/testdb"
)

// Selecting a missing wallet must only fill the form. Repair remains a
// separately authorized POST, even when the target ID comes from the directory.
func TestWalletRepairSelectionIsReadOnlyAndRequiresWalletAccess(t *testing.T) {
	db := testdb.Open(t)
	farmers := repository.NewFarmerRepository(db)
	create := func(name, phone string) *models.Farmer {
		t.Helper()
		f := &models.Farmer{FullName: name, Phone: phone, Role: "farmer", Location: "Kano", State: "Kano", Country: "Nigeria", PasswordHash: "fixture", PhotoURL: "/fixture.jpg"}
		if err := farmers.Create(f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	admin := create("Repair administrator", "2348000000201")
	target := create("Repair target", "2348000000202")
	if _, err := db.Exec(`UPDATE farmers SET admin_role='administrator',is_admin=true,selfie_captured_at=CURRENT_TIMESTAMP,phone_verified_at=CURRENT_TIMESTAMP WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE wallets SET transaction_pin_hash='fixture' WHERE farmer_id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM wallets WHERE farmer_id=$1`, target.ID); err != nil {
		t.Fatal(err)
	}
	wallet := services.NewWalletService(repository.NewWalletRepository(db), farmers, nil, nil, nil, 0, nil)
	old := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	t.Cleanup(func() { http.DefaultServeMux = old })
	sessions := services.NewSessionService(repository.NewSessionRepository(db))
	registerAdminRoutes(&app.Container{FarmerRepo: farmers, Sessions: sessions, Admin: repository.NewAdminRepository(db, nil)}, handlers.NewWalletHandler(wallet, nil, nil))
	token, _, err := sessions.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/admin/wallet-withdrawals"+query, nil)
		r.AddCookie(&http.Cookie{Name: "agroshield_session", Value: token})
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, r)
		return w
	}
	w := request(fmt.Sprintf("?user_id=%d", target.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), fmt.Sprintf(`value="%d"`, target.ID)) || !strings.Contains(w.Body.String(), fmt.Sprintf("Account ID #%d selected.", target.ID)) {
		t.Fatal("directory selection did not fill the repair form")
	}
	var wallets int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wallets WHERE farmer_id=$1`, target.ID).Scan(&wallets); err != nil || wallets != 0 {
		t.Fatal("selecting a member repaired their wallet before submission")
	}
	for _, query := range []string{"?user_id=0", "?user_id=-1", "?user_id=invalid", "?user_id=99999999999999999999999999999"} {
		if request(query).Code != http.StatusBadRequest {
			t.Fatalf("invalid selection %q was accepted", query)
		}
	}
	if _, err := db.Exec(`UPDATE farmers SET is_admin=false WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if request(fmt.Sprintf("?user_id=%d", target.ID)).Code != http.StatusForbidden {
		t.Fatal("community administrator without wallet access could select a wallet for repair")
	}
}
