package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/database"
	"backend/internal/models"
)

func adminFixtureUser(t *testing.T, db *sql.DB, name string) *models.Farmer {
	t.Helper()
	f := &models.Farmer{FullName: name, Phone: fmt.Sprintf("23480%08d", time.Now().UnixNano()%100000000), Role: "farmer", Location: "Kano", State: "Kano", Country: "Nigeria", PasswordHash: "synthetic-admin-password", PhotoURL: "/fixture.jpg"}
	if err := NewFarmerRepository(db).Create(f); err != nil {
		t.Fatal(err)
	}
	return f
}
func adminFixtureCrop(t *testing.T, db *sql.DB, sellerID int) *models.Crop {
	t.Helper()
	c := &models.Crop{FarmerID: sellerID, Name: "Review Maize", Quantity: 10, Unit: "kg", Location: "Kano", LGA: "Kano", State: "Kano", Country: "Nigeria", Latitude: 12.0022, Longitude: 8.5919, LocationAccuracy: 10, PricePerUnit: 1000, ListedForSale: true, ImageURL: "/fixture.jpg"}
	if err := NewCropRepository(db).Create(c); err != nil {
		t.Fatal(err)
	}
	return c
}
func adminExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func adminAuditCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM admin_audit_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdminGovernancePermissionsAndProtectedAccounts(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Configured administrator")
	mod := adminFixtureUser(t, db, "Moderator")
	wallet := adminFixtureUser(t, db, "Wallet approver")
	member := adminFixtureUser(t, db, "Member")
	adminExec(t, db, `UPDATE farmers SET admin_role='moderator' WHERE id=$1`, mod.ID)
	adminExec(t, db, `UPDATE farmers SET is_admin=true WHERE id=$1`, wallet.ID)
	r := NewAdminRepository(db, []int{root.ID})
	for _, id := range []int{mod.ID, wallet.ID, member.ID} {
		if err := r.ChangeUser(ctx, id, models.AdminChange{TargetID: member.ID, Action: "role_administrator", Reason: "staff_assignment"}); !errors.Is(err, ErrAdminForbidden) {
			t.Fatalf("actor %d escalated access: %v", id, err)
		}
		if _, err := r.Audit(ctx, id, 0); !errors.Is(err, ErrAdminForbidden) {
			t.Fatalf("actor %d read audit: %v", id, err)
		}
	}
	for _, target := range []int{root.ID, wallet.ID} {
		if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: target, Action: "suspend", Reason: "policy_violation"}); !errors.Is(err, ErrAdminProtected) {
			t.Fatalf("protected account %d changed: %v", target, err)
		}
	}
	if adminAuditCount(t, db) != 0 {
		t.Fatal("denied changes produced committed audit records")
	}
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: member.ID, Action: "role_administrator", Reason: "staff_assignment"}); err != nil {
		t.Fatal(err)
	}
	access, err := r.Access(ctx, member.ID)
	if err != nil || !access.Can(models.AdminManageRoles) || access.Can(models.AdminManageWallet) {
		t.Fatalf("governance and wallet roles were not separated: %+v %v", access, err)
	}
	if err := r.ChangeUser(ctx, member.ID, models.AdminChange{TargetID: root.ID, Action: "role_none", Reason: "staff_assignment"}); !errors.Is(err, ErrAdminProtected) {
		t.Fatal("configured administrator was demoted")
	}
	adminExec(t, db, `UPDATE farmers SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1`, root.ID)
	if _, err := r.Access(ctx, root.ID); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("configuration restored a deleted account's permissions")
	}
}

func TestAdminSuspensionRevokesSessionsAndRequiresFreshLogin(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Admin")
	member := adminFixtureUser(t, db, "Member")
	r := NewAdminRepository(db, []int{root.ID})
	sessions := NewSessionRepository(db)
	if err := sessions.Create(ctx, "old-session", member.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	c := models.AdminChange{TargetID: member.ID, Action: "suspend", Reason: "policy_violation"}
	if err := r.ChangeUser(ctx, root.ID, c); err != nil {
		t.Fatal(err)
	}
	f, err := NewFarmerRepository(db).GetByID(member.ID)
	if err != nil || !f.SuspendedAt.Valid || !f.SessionsRevokedAt.Valid {
		t.Fatalf("suspension not persisted: %v", err)
	}
	if _, err := sessions.FindValid(ctx, "old-session"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("suspended session authenticated")
	}
	if err := sessions.Create(ctx, "during-suspension", member.ID, time.Now().Add(time.Hour)); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session created during suspension: %v", err)
	}
	if err := r.ChangeUser(ctx, root.ID, c); !errors.Is(err, ErrAdminConflict) {
		t.Fatalf("stale action accepted: %v", err)
	}
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: member.ID, Version: 1, Action: "reactivate", Reason: "review_cleared"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.FindValid(ctx, "old-session"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("reactivation restored old session")
	}
	if err := sessions.Create(ctx, "new-session", member.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if id, err := sessions.FindValid(ctx, "new-session"); err != nil || id != member.ID {
		t.Fatalf("fresh login failed: %d %v", id, err)
	}
	events, err := r.Audit(ctx, root.ID, 0)
	if err != nil || len(events) != 2 {
		t.Fatalf("audit=%+v err=%v", events, err)
	}
	if events[1].ActorID != root.ID || events[1].Reason != "policy_violation" || events[1].Before == events[1].After || events[0].Action != "user.reactivate" {
		t.Fatal("audit lost actor, reason or transition")
	}
}

func TestAdminChangesRollBackWhenAuditCannotBeWritten(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Admin")
	member := adminFixtureUser(t, db, "Member")
	crop := adminFixtureCrop(t, db, member.ID)
	r := NewAdminRepository(db, []int{root.ID})
	if err := NewSessionRepository(db).Create(ctx, "still-valid", member.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	adminExec(t, db, `CREATE FUNCTION fail_fixture_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit unavailable'; END $$; CREATE TRIGGER fixture_audit_failure BEFORE INSERT ON admin_audit_events FOR EACH ROW EXECUTE FUNCTION fail_fixture_audit()`)
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: member.ID, Action: "suspend", Reason: "policy_violation"}); err == nil {
		t.Fatal("suspension committed without audit")
	}
	if err := r.ChangeListing(ctx, root.ID, models.AdminChange{TargetID: crop.ID, Action: "block", Reason: "incorrect_listing"}); err == nil {
		t.Fatal("moderation committed without audit")
	}
	var suspended, blocked bool
	var version int
	if err := db.QueryRow(`SELECT suspended_at IS NOT NULL,admin_version FROM farmers WHERE id=$1`, member.ID).Scan(&suspended, &version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT moderation_blocked FROM crops WHERE id=$1`, crop.ID).Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	if suspended || blocked || version != 0 || adminAuditCount(t, db) != 0 {
		t.Fatal("failed audit left a partial administrative change")
	}
	if _, err := NewSessionRepository(db).FindValid(ctx, "still-valid"); err != nil {
		t.Fatal("rolled-back suspension revoked session")
	}
}

func TestAdminAuditIsAppendOnlyAndMigrationIsRepeatable(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Admin")
	member := adminFixtureUser(t, db, "Member")
	r := NewAdminRepository(db, []int{root.ID})
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: member.ID, Action: "role_moderator", Reason: "staff_assignment"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE admin_audit_events SET reason='tampered'`, `DELETE FROM admin_audit_events`, `TRUNCATE admin_audit_events`} {
		if _, err := db.Exec(q); err == nil {
			t.Fatalf("audit mutation allowed: %s", q)
		}
	}
	for range 2 {
		if err := database.RunAdminMigration(db); err != nil {
			t.Fatal(err)
		}
	}
	events, err := r.Audit(ctx, root.ID, 0)
	if err != nil || len(events) != 1 || events[0].Reason != "staff_assignment" {
		t.Fatalf("audit altered across mutation or migration: %+v %v", events, err)
	}
	access, err := r.Access(ctx, member.ID)
	if err != nil || access.Role != "moderator" {
		t.Fatal("migration changed existing permissions")
	}
}

func TestAdminModerationAndSuspensionBlockPurchaseEntryPoints(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Admin")
	seller := adminFixtureUser(t, db, "Seller")
	buyer := adminFixtureUser(t, db, "Buyer")
	crop := adminFixtureCrop(t, db, seller.ID)
	r := NewAdminRepository(db, []int{root.ID})
	wallet := NewWalletRepository(db)
	fundWallet(t, db, buyer.ID, "5000.00")
	order := &models.Order{BuyerID: buyer.ID, CropID: crop.ID, Quantity: 1, TotalPrice: 1000, Status: "pending_payment"}
	if err := NewOrderRepository(db).Create(order); err != nil {
		t.Fatalf("initial external order: %v", err)
	}
	assertBlocked := func(hideListing bool) {
		t.Helper()
		if _, _, err := wallet.PayForOrder(ctx, buyer.ID, crop.ID, 1, uniqueRef("BLOCKED"), 0); err == nil {
			t.Fatal("wallet checkout bypassed moderation/suspension")
		}
		copyOrder := *order
		if err := NewOrderRepository(db).Create(&copyOrder); err == nil {
			t.Fatal("legacy order creation bypassed moderation/suspension")
		}
		if _, err := NewPaymentRepository(db).Create(ctx, int64(order.ID), uniqueRef("EXTERNAL"), "1000.00", "NGN", models.ProviderFlutterwave); err == nil {
			t.Fatal("external payment initiated for blocked purchase")
		}
		if walletBalanceOf(t, db, buyer.ID) != "5000.00" {
			t.Fatal("blocked checkout debited buyer")
		}
		if hideListing {
			items, err := NewCropRepository(db).ListAvailable()
			if err != nil || len(items) != 0 {
				t.Fatalf("blocked listing in marketplace: %v", err)
			}
			items, err = NewCropRepository(db).SearchAvailable("Maize")
			if err != nil || len(items) != 0 {
				t.Fatal("blocked listing in search")
			}
			found, err := NewCropRepository(db).GetByID(crop.ID)
			if err != nil || found.ListedForSale {
				t.Fatal("direct product lookup bypassed moderation")
			}
		}
	}
	if err := r.ChangeListing(ctx, root.ID, models.AdminChange{TargetID: crop.ID, Action: "block", Reason: "incorrect_listing"}); err != nil {
		t.Fatal(err)
	}
	assertBlocked(true)
	// The seller can still edit their inventory, but cannot clear moderation.
	adminExec(t, db, `UPDATE crops SET listed_for_sale=true WHERE id=$1`, crop.ID)
	assertBlocked(true)
	if err := r.ChangeListing(ctx, root.ID, models.AdminChange{TargetID: crop.ID, Version: 1, Action: "restore", Reason: "review_cleared"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: seller.ID, Action: "suspend", Reason: "policy_violation"}); err != nil {
		t.Fatal(err)
	}
	assertBlocked(true)
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: seller.ID, Version: 1, Action: "reactivate", Reason: "review_cleared"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: buyer.ID, Action: "suspend", Reason: "suspected_fraud"}); err != nil {
		t.Fatal(err)
	}
	assertBlocked(false)
	if err := r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: buyer.ID, Version: 1, Action: "reactivate", Reason: "review_cleared"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPaymentRepository(db).Create(ctx, int64(order.ID), uniqueRef("ALLOWED"), "1000.00", "NGN", models.ProviderFlutterwave); err != nil {
		t.Fatalf("restored external checkout unavailable: %v", err)
	}
	if _, _, err := wallet.PayForOrder(ctx, buyer.ID, crop.ID, 1, uniqueRef("ALLOWED"), 0); err != nil {
		t.Fatalf("restored wallet checkout unavailable: %v", err)
	}
	if got := walletBalanceOf(t, db, buyer.ID); got != "4000.00" {
		t.Fatalf("restored checkout balance=%s", got)
	}
}

func TestAdminConcurrentDecisionsProduceOneChangeAndAudit(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Admin")
	member := adminFixtureUser(t, db, "Member")
	r := NewAdminRepository(db, []int{root.ID})
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = r.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: member.ID, Action: "suspend", Reason: "policy_violation"})
		}(i)
	}
	close(start)
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrAdminConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 || adminAuditCount(t, db) != 1 {
		t.Fatalf("concurrent decisions: success=%d conflict=%d audits=%d", ok, conflict, adminAuditCount(t, db))
	}
}

func TestAdminWalletAuditTracksRequestsAndCommittedDecisions(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Admin")
	sender := adminFixtureUser(t, db, "Sender")
	recipient := adminFixtureUser(t, db, "Recipient")
	r := NewAdminRepository(db, []int{root.ID})
	wallet := NewWalletRepository(db)
	fundWallet(t, db, sender.ID, "5000.00")
	if err := wallet.CreateTransfer(ctx, sender.ID, recipient.ID, "audit-transfer", "500.00", "fixture"); err != nil {
		t.Fatal(err)
	}
	transfers, err := wallet.ListPendingTransfers(ctx)
	if err != nil || len(transfers) != 1 {
		t.Fatalf("transfer fixture: %v", err)
	}
	id := transfers[0].ID
	e := models.AdminAuditEvent{ActorID: root.ID, Action: "wallet.transfer.approve", Resource: "wallet_transfers", TargetID: id, Outcome: "attempted", RequestID: "fixture-request"}
	if err := r.RecordWalletAttempt(ctx, e); err != nil {
		t.Fatal(err)
	}
	if _, err := wallet.ApproveTransfer(ctx, id, root.ID); err != nil {
		t.Fatal(err)
	}
	e.Outcome = "returned"
	if err := r.RecordWalletResult(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.RequestID = "forged-request"
	if err := r.RecordWalletResult(ctx, e); !errors.Is(err, ErrAdminInvalid) {
		t.Fatal("completion was recorded without matching intent")
	}
	e.RequestID = "fixture-request"
	if err := r.RecordWalletResult(ctx, e); err == nil {
		t.Fatal("duplicate completion was accepted")
	}
	events, err := r.Audit(ctx, root.ID, 0)
	if err != nil || len(events) != 3 {
		t.Fatalf("wallet audit=%+v err=%v", events, err)
	}
	transition := events[1]
	if transition.Action != "wallet.status" || transition.ActorID != root.ID || transition.Before != "pending" || transition.After != "completed" || transition.Outcome != "committed" {
		t.Fatalf("incorrect committed wallet transition: %+v", transition)
	}
	if walletBalanceOf(t, db, recipient.ID) != "500.00" {
		t.Fatal("audit integration changed transfer settlement")
	}
	if err := wallet.CreateTransfer(ctx, sender.ID, recipient.ID, "audit-reject", "200.00", "fixture"); err != nil {
		t.Fatal(err)
	}
	transfers, err = wallet.ListPendingTransfers(ctx)
	if err != nil || len(transfers) != 1 {
		t.Fatal("missing rejection fixture")
	}
	if _, err := wallet.RejectTransfer(ctx, transfers[0].ID, root.ID); err != nil {
		t.Fatal(err)
	}
	events, err = r.Audit(ctx, root.ID, 0)
	if err != nil || events[0].After != "rejected" || events[0].ActorID != root.ID {
		t.Fatal("wallet rejection not attributed")
	}
}

func TestAdminDirectoryPaginationAndSensitiveDataMinimization(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Admin")
	r := NewAdminRepository(db, []int{root.ID})
	var last *models.Farmer
	for i := 0; i < 28; i++ {
		last = adminFixtureUser(t, db, fmt.Sprintf("Directory member %02d", i))
	}
	first, err := r.Users(ctx, root.ID, "", 0)
	if err != nil || len(first) != 25 {
		t.Fatalf("first page=%d err=%v", len(first), err)
	}
	second, err := r.Users(ctx, root.ID, "", int64(first[24].ID))
	if err != nil || len(second) != 4 {
		t.Fatalf("next page=%d err=%v", len(second), err)
	}
	seen := map[int]bool{}
	for _, u := range append(first, second...) {
		if seen[u.ID] {
			t.Fatal("pagination repeated a member")
		}
		seen[u.ID] = true
	}
	exact, err := r.Users(ctx, root.ID, last.Phone, 0)
	if err != nil || len(exact) != 1 || exact[0].ID != last.ID {
		t.Fatal("exact phone lookup failed")
	}
	body, err := json.Marshal(exact)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{last.Phone, last.PasswordHash, "password_hash", "bank_name", "account_number"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("directory exposed %q", secret)
		}
	}
	if !strings.HasSuffix(exact[0].PhoneHint, last.Phone[len(last.Phone)-4:]) {
		t.Fatal("masked phone hint incorrect")
	}
	if _, err := r.Users(ctx, root.ID, strings.Repeat("x", 101), 0); !errors.Is(err, ErrAdminInvalid) {
		t.Fatal("unbounded search accepted")
	}
	if _, err := r.Users(ctx, root.ID, "", -1); !errors.Is(err, ErrAdminInvalid) {
		t.Fatal("negative cursor accepted")
	}
	injected, err := r.Users(ctx, root.ID, "'; DROP TABLE farmers; --", 0)
	if err != nil || len(injected) != 0 {
		t.Fatal("search was not treated as a literal parameter")
	}
}
