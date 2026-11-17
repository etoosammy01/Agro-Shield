package routes

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/handlers"
	app "backend/internal"
	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/internal/testdb"
	"backend/middleware"
)

type disputeRouteFixture struct {
	db                                                   *sql.DB
	container                                            *app.Container
	buyer, seller, admin, moderator, walletStaff, member *models.Farmer
	tokens                                               map[int]string
	order                                                int64
	root                                                 string
}

func newDisputeRouteFixture(t *testing.T) *disputeRouteFixture {
	t.Helper()
	f := &disputeRouteFixture{db: testdb.Open(t), tokens: make(map[int]string), root: t.TempDir()}
	farmers := repository.NewFarmerRepository(f.db)
	create := func(name string, n int) *models.Farmer {
		p := &models.Farmer{FullName: name, Phone: fmt.Sprintf("234800001%04d", n), Role: "farmer", Location: "Kano", State: "Kano", Country: "Nigeria", PhotoURL: "/fixture.jpg", PasswordHash: "synthetic"}
		if err := farmers.Create(p); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(`UPDATE farmers SET selfie_captured_at=CURRENT_TIMESTAMP,phone_verified_at=CURRENT_TIMESTAMP WHERE id=$1`, p.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(`UPDATE wallets SET transaction_pin_hash='synthetic' WHERE farmer_id=$1`, p.ID); err != nil {
			t.Fatal(err)
		}
		return p
	}
	f.buyer = create("Buyer", 1)
	f.seller = create("Seller", 2)
	f.admin = create("Reviewer", 3)
	f.moderator = create("Moderator", 4)
	f.walletStaff = create("Wallet staff", 5)
	f.member = create("Unrelated member", 6)
	if _, err := f.db.Exec(`UPDATE farmers SET admin_role='administrator' WHERE id=$1;`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE farmers SET admin_role='moderator' WHERE id=$1`, f.moderator.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE farmers SET is_admin=true WHERE id=$1`, f.walletStaff.ID); err != nil {
		t.Fatal(err)
	}
	wallet := repository.NewWalletRepository(f.db)
	governance := repository.NewAdminRepository(f.db, nil)
	access := repository.NewDisputeReviewAccess(governance)
	disputes := services.NewOrderDisputeService(repository.NewOrderDisputeRepository(f.db), access)
	disputes.ConfigureReview(access)
	wallet.ConfigureReviewAccess(access)
	disputes.SetSettler(wallet)
	sessions := services.NewSessionService(repository.NewSessionRepository(f.db))
	for _, p := range []*models.Farmer{f.buyer, f.seller, f.admin, f.moderator, f.walletStaff, f.member} {
		token, _, err := sessions.Create(context.Background(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.tokens[p.ID] = token
	}
	crops := repository.NewCropRepository(f.db)
	crop := &models.Crop{FarmerID: f.seller.ID, Name: "Synthetic maize", Quantity: 10, Unit: "kg", Location: "Kano", LGA: "Kano", State: "Kano", Country: "Nigeria", Latitude: 12, Longitude: 8, LocationAccuracy: 10, PricePerUnit: 1000, ListedForSale: true, ImageURL: "/fixture.jpg"}
	if err := crops.Create(crop); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE wallets SET balance=5000 WHERE farmer_id=$1`, f.buyer.ID); err != nil {
		t.Fatal(err)
	}
	order, _, err := wallet.PayForOrder(context.Background(), f.buyer.ID, crop.ID, 1, "route-checkout", 500)
	if err != nil {
		t.Fatal(err)
	}
	f.order = order
	f.container = &app.Container{FarmerRepo: farmers, Sessions: sessions, Admin: governance, Wallet: services.NewWalletService(wallet, farmers, nil, nil, nil, 500, nil), Order: services.NewOrderService(repository.NewOrderRepository(f.db), crops), OrderDispute: disputes}
	t.Setenv("DISPUTE_EVIDENCE_DIR", f.root)
	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	t.Cleanup(func() { http.DefaultServeMux = previous })
	registerDisputeRoutes(f.container)
	orderHandler := handlers.NewOrderHandler(f.container.Order, nil)
	orderHandler.ConfigureDisputes(f.container.Wallet, disputes)
	http.Handle("/order", middleware.CSRFProtect(middleware.OnlyPath("/order", middleware.NewAuthenticator(farmers, sessions).RequireAuth(orderHandler.Detail))))
	return f
}

func (f *disputeRouteFixture) request(t *testing.T, actor int, method, path string, body io.Reader, contentType, origin string, html bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://example.com"+path, body)
	if actor > 0 {
		r.AddCookie(&http.Cookie{Name: "agroshield_session", Value: f.tokens[actor]})
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if !html {
		r.Header.Set("Accept", "application/json")
	}
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, r)
	return w
}

func (f *disputeRouteFixture) post(t *testing.T, actor int, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return f.request(t, actor, "POST", path, bytes.NewReader(data), "application/json", "http://example.com", false)
}

func (f *disputeRouteFixture) create(t *testing.T, reason string) *models.OrderDispute {
	t.Helper()
	w := f.post(t, f.buyer.ID, "/api/disputes/create", map[string]any{"order_id": f.order, "reason_code": reason, "description": "Synthetic issue for review", "requested_amount": "250.00"})
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Dispute *models.OrderDispute `json:"dispute"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Dispute == nil {
		t.Fatal("invalid created dispute", err)
	}
	return result.Dispute
}

func TestDisputeRoutesEnforcePermissionsAndVersionedSettlement(t *testing.T) {
	f := newDisputeRouteFixture(t)
	d := f.create(t, "missing_quantity")
	for _, actor := range []int{f.member.ID, f.buyer.ID, f.seller.ID, f.moderator.ID, f.walletStaff.ID} {
		for _, path := range []string{"/admin/disputes", "/api/admin/disputes", "/admin/disputes/view?id=1", "/admin/payment-exceptions"} {
			if w := f.request(t, actor, "GET", path, nil, "", "", false); w.Code != 403 {
				t.Fatalf("actor %d %s = %d", actor, path, w.Code)
			}
		}
		for _, action := range []string{"review", "reject", "approve", "settle"} {
			if w := f.post(t, actor, "/api/admin/disputes/"+action, map[string]any{"dispute_id": d.ID, "version": 0, "reviewed": true}); w.Code != 403 {
				t.Fatalf("actor %d action %s = %d", actor, action, w.Code)
			}
		}
	}
	for _, path := range []string{"/disputes/view?id=1", "/api/disputes/view?id=1", "/disputes/evidence?dispute_id=1&id=1"} {
		if w := f.request(t, f.member.ID, "GET", path, nil, "", "", false); w.Code != 404 {
			t.Fatalf("private %s = %d", path, w.Code)
		}
	}
	if w := f.request(t, 0, "GET", "/admin/disputes", nil, "", "", false); w.Code != 303 {
		t.Fatal("anonymous review", w.Code)
	}
	for _, origin := range []string{"", "https://attacker.invalid"} {
		if w := f.request(t, f.admin.ID, "POST", "/admin/disputes/review", strings.NewReader("dispute_id=1&version=0&reviewed=1"), "application/x-www-form-urlencoded", origin, true); w.Code != 403 {
			t.Fatal("CSRF accepted", w.Code)
		}
	}
	if w := f.post(t, f.admin.ID, "/admin/disputes/review", map[string]any{"dispute_id": d.ID, "reviewed": true}); w.Code != 400 {
		t.Fatal("unversioned review accepted", w.Code)
	}
	if w := f.request(t, f.admin.ID, "GET", "/admin/disputes/review", nil, "", "", false); w.Code != 405 {
		t.Fatal("wrong method", w.Code)
	}
	if w := f.request(t, f.admin.ID, "GET", "/admin/disputes/view/extra", nil, "", "", false); w.Code != 404 {
		t.Fatal("non-exact route", w.Code)
	}
	if w := f.post(t, f.seller.ID, "/orders/disputes/respond", map[string]any{"dispute_id": d.ID, "response": "Synthetic seller response"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.post(t, f.admin.ID, "/admin/disputes/review", map[string]any{"dispute_id": d.ID, "version": 0, "reviewed": true}); w.Code != 409 {
		t.Fatal("stale review", w.Code)
	}
	if w := f.post(t, f.admin.ID, "/admin/disputes/review", map[string]any{"dispute_id": d.ID, "version": 1, "reviewed": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.request(t, f.admin.ID, "GET", "/admin/disputes/view?id=1", nil, "", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), "Approve and settle refund") {
		t.Fatal("detail template", w.Code, w.Body.String())
	}
	w := f.post(t, f.admin.ID, "/admin/disputes/approve", map[string]any{"dispute_id": d.ID, "version": 2, "reviewed": true, "approved_amount": "250.00", "approval_note": "Reviewed evidence"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var refund models.Refund
	if err := json.Unmarshal(w.Body.Bytes(), &refund); err != nil || refund.Status != models.RefundSucceeded {
		t.Fatal("refund not settled", err, w.Body.String())
	}
	if w := f.post(t, f.admin.ID, "/admin/disputes/settle", map[string]any{"refund_id": refund.ID, "version": 2, "reviewed": true}); w.Code != 200 {
		t.Fatal("idempotent retry", w.Code, w.Body.String())
	}
	var balance string
	if err := f.db.QueryRow(`SELECT balance::text FROM wallets WHERE farmer_id=$1`, f.buyer.ID).Scan(&balance); err != nil || balance != "4250.00" {
		t.Fatal("buyer credit", balance, err)
	}
	if _, err := f.db.Exec(`UPDATE farmers SET admin_role='' WHERE id=$1`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	if w := f.post(t, f.admin.ID, "/admin/disputes/settle", map[string]any{"refund_id": refund.ID, "version": 3, "reviewed": true}); w.Code != 403 {
		t.Fatal("revoked review", w.Code)
	}
}

func (f *disputeRouteFixture) upload(t *testing.T, actor int, id int64, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("dispute_id", fmt.Sprint(id)); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("evidence", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return f.request(t, actor, "POST", "/disputes/evidence/upload", &body, writer.FormDataContentType(), "http://example.com", false)
}

func TestDisputeEvidenceIsPrivateValidatedAndTamperChecked(t *testing.T) {
	f := newDisputeRouteFixture(t)
	d := f.create(t, "quality_issue")
	if w := f.upload(t, f.buyer.ID, d.ID, "video.mp4", []byte("<script>alert(1)</script>")); w.Code != 400 {
		t.Fatal("forged video accepted", w.Code)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if w := f.upload(t, f.member.ID, d.ID, "photo.png", data.Bytes()); w.Code != 404 {
		t.Fatal("unrelated upload", w.Code)
	}
	w := f.upload(t, f.buyer.ID, d.ID, "photo.png", data.Bytes())
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "StorageKey") || strings.Contains(w.Body.String(), "ContentHash") {
		t.Fatal("private storage metadata exposed")
	}
	var key string
	var id int64
	if err := f.db.QueryRow(`SELECT id,storage_key FROM order_dispute_evidence WHERE dispute_id=$1`, d.ID).Scan(&id, &key); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/disputes/evidence?dispute_id=%d&id=%d", d.ID, id)
	for _, actor := range []int{f.buyer.ID, f.seller.ID, f.admin.ID} {
		w := f.request(t, actor, "GET", path, nil, "", "", false)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data.Bytes()) || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal("private download", actor, w.Code)
		}
	}
	for _, actor := range []int{f.member.ID, f.moderator.ID, f.walletStaff.ID} {
		if w := f.request(t, actor, "GET", path, nil, "", "", false); w.Code != 404 {
			t.Fatal("evidence disclosure", actor, w.Code)
		}
	}
	if w := f.post(t, f.admin.ID, "/admin/disputes/review", map[string]any{"dispute_id": d.ID, "version": 1, "reviewed": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.post(t, f.admin.ID, "/admin/disputes/approve", map[string]any{"dispute_id": d.ID, "version": 2, "reviewed": true, "approved_amount": "100.00", "approval_note": "Photo only"}); w.Code != 400 {
		t.Fatal("quality refund without video", w.Code)
	}
	// Changing even one byte, while retaining the size, prevents delivery.
	tampered := append([]byte(nil), data.Bytes()...)
	tampered[len(tampered)-1] ^= 1
	if err := os.WriteFile(filepath.Join(f.root, key), tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if w := f.request(t, f.admin.ID, "GET", path, nil, "", "", false); w.Code != 404 {
		t.Fatal("tampered evidence served", w.Code)
	}
	if _, err := f.db.Exec(`UPDATE farmers SET admin_role='' WHERE id=$1`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	if w := f.request(t, f.admin.ID, "GET", path, nil, "", "", false); w.Code != 404 {
		t.Fatal("revoked evidence access", w.Code)
	}
}

func TestDisputeFormsSupportWithdrawalAndReceiptConfirmation(t *testing.T) {
	f := newDisputeRouteFixture(t)
	if w := f.request(t, f.buyer.ID, "GET", "/order?id=1", nil, "", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), "Confirm receipt") || !strings.Contains(w.Body.String(), "Report an issue") {
		t.Fatal("order support controls", w.Code, w.Body.String())
	}
	for _, path := range []string{"/disputes", "/disputes/new?order_id=1", "/admin/disputes", "/admin/payment-exceptions"} {
		actor := f.buyer.ID
		if strings.HasPrefix(path, "/admin/") {
			actor = f.admin.ID
		}
		if w := f.request(t, actor, "GET", path, nil, "", "", true); w.Code != 200 {
			t.Fatal("template", path, w.Code, w.Body.String())
		}
	}
	d := f.create(t, "missing_quantity")
	if w := f.request(t, f.buyer.ID, "GET", "/order?id=1", nil, "", "", true); w.Code != 200 || strings.Contains(w.Body.String(), "Confirm receipt") || !strings.Contains(w.Body.String(), "View case and evidence") {
		t.Fatal("disputed order controls", w.Code, w.Body.String())
	}
	if w := f.request(t, f.buyer.ID, "GET", "/disputes/view?id=1", nil, "", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), "Withdraw dispute") {
		t.Fatal("buyer detail template", w.Code, w.Body.String())
	}
	values := url.Values{"dispute_id": {fmt.Sprint(d.ID)}, "version": {"0"}, "reviewed": {"1"}}
	if w := f.request(t, f.buyer.ID, "POST", "/disputes/withdraw", strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", "http://example.com", true); w.Code != 303 {
		t.Fatal("withdraw form", w.Code, w.Body.String())
	}
	values = url.Values{"order_id": {fmt.Sprint(f.order)}, "reviewed": {"1"}}
	if w := f.request(t, f.buyer.ID, "POST", "/orders/confirm-receipt", strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", "http://example.com", true); w.Code != 303 {
		t.Fatal("receipt form", w.Code, w.Body.String())
	}
	var held string
	if err := f.db.QueryRow(`SELECT held_balance::text FROM wallets WHERE farmer_id=$1`, f.seller.ID).Scan(&held); err != nil || held != "0.00" {
		t.Fatal("receipt did not release funds", held, err)
	}
}
