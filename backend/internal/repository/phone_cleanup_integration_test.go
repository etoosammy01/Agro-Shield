package repository

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"backend/internal/database"
	"backend/internal/phone"
)

func duplicateFixture(t *testing.T) (*sql.DB, *AdminRepository, int, []int) {
	t.Helper()
	db := testDB(t)
	keeper := adminFixtureUser(t, db, "Administrator keeper")
	a := adminFixtureUser(t, db, "First test duplicate")
	b := adminFixtureUser(t, db, "Second test duplicate")
	// Reproduce a legacy group that predates the canonical uniqueness index.
	adminExec(t, db, `DROP INDEX idx_farmers_normalized_phone`)
	for i, id := range []int{keeper.ID, a.ID, b.ID} {
		adminExec(t, db, `UPDATE farmers SET phone=$1 WHERE id=$2`, []string{"+234 (803) 000-0101", "0803 000 0101", "08030000101"}[i], id)
	}
	adminExec(t, db, `INSERT INTO wallet_deposits(wallet_id,reference,amount,currency) SELECT id,'TEST-CLEANUP',100,'NGN' FROM wallets WHERE farmer_id=$1`, a.ID)
	adminExec(t, db, `INSERT INTO wallet_transactions(wallet_id,reference,type,amount) SELECT id,'TEST-CLEANUP-LEDGER','deposit',100 FROM wallets WHERE farmer_id=$1`, a.ID)
	adminExec(t, db, `INSERT INTO wallet_virtual_accounts(farmer_id,tx_ref) VALUES($1,'TEST-CLEANUP-VA')`, a.ID)
	if err := NewSessionRepository(db).Create(context.Background(), "duplicate-session", a.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	adminFixtureCrop(t, db, a.ID)
	adminFixtureCrop(t, db, keeper.ID)
	return db, NewAdminRepository(db, []int{keeper.ID}), keeper.ID, []int{a.ID, b.ID}
}

func TestTestDuplicateCleanupPreservesAdminAndClosesOnlyDuplicates(t *testing.T) {
	db, r, keeper, targets := duplicateFixture(t)
	ctx := context.Background()
	before, err := NewFarmerRepository(db).GetByID(keeper)
	if err != nil {
		t.Fatal(err)
	}
	dry, err := r.CleanupTestDuplicates(ctx, keeper, targets, true, false)
	if err != nil || dry.Committed || len(dry.ClosedIDs) != 2 {
		t.Fatalf("dry-run: %+v %v", dry, err)
	}
	if adminAuditCount(t, db) != 0 {
		t.Fatal("dry-run persisted audit")
	}
	if _, err := NewSessionRepository(db).FindValid(ctx, "duplicate-session"); err != nil {
		t.Fatal("dry-run changed sessions")
	}
	report, err := r.CleanupTestDuplicates(ctx, keeper, targets, true, true)
	if err != nil || !report.Committed || report.DepositsRemoved != 1 || report.LedgerRowsRemoved != 1 || report.FundingAccountsRemoved != 1 {
		t.Fatalf("cleanup: %+v %v", report, err)
	}
	for _, id := range targets {
		f, err := NewFarmerRepository(db).GetByID(id)
		if err != nil || f != nil {
			t.Fatal("duplicate remains active")
		}
	}
	if _, err := NewSessionRepository(db).FindValid(ctx, "duplicate-session"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("duplicate session remains valid")
	}
	access, err := r.Access(ctx, keeper)
	if err != nil || access.Role != "administrator" {
		t.Fatal("administrator lost access")
	}
	after, err := NewFarmerRepository(db).GetByID(keeper)
	if err != nil || before.PasswordHash != after.PasswordHash || before.PhotoURL != after.PhotoURL || phone.Normalize(before.Phone) != after.Phone || before.WalletNumber != after.WalletNumber {
		t.Fatal("keeper profile changed")
	}
	var listings int
	if err := db.QueryRow(`SELECT count(*) FROM crops WHERE farmer_id=$1`, keeper).Scan(&listings); err != nil || listings != 1 {
		t.Fatal("keeper listing changed")
	}
	events, err := r.Audit(ctx, keeper, 0)
	if err != nil || len(events) != 3 {
		t.Fatal("cleanup audit missing")
	}
	for _, e := range events {
		if e.Action == "system.phone_standardization" {
			continue
		}
		if e.ActorID != keeper || e.Action != "user.close_test_duplicate" || e.Reason != "owner_confirmed_test_duplicates" {
			t.Fatal("cleanup audit attribution lost")
		}
	}
	if err := database.RunPhoneMigration(db); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"0803 000 0101", "08030000101", "+2348030000101", "2348030000101"} {
		f, err := NewFarmerRepository(db).GetByPhone(raw)
		if err != nil || f == nil || f.ID != keeper || f.Phone != "2348030000101" {
			t.Fatal("canonical lookup did not return administrator")
		}
		users, err := r.Users(ctx, keeper, raw, 0)
		if err != nil || len(users) != 1 || users[0].ID != keeper {
			t.Fatal("formatted admin search did not return the retained account")
		}
	}
	count := adminAuditCount(t, db)
	if err := database.RunPhoneMigration(db); err != nil || adminAuditCount(t, db) != count {
		t.Fatal("phone migration is not repeatable")
	}
}

func TestTestDuplicateCleanupRollsBackAndProtectsAccounts(t *testing.T) {
	for _, scenario := range []string{"no_attestation", "keeper_is_target", "target_is_admin", "different_phone", "balance", "audit_failure", "second_target_failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, r, keeper, targets := duplicateFixture(t)
			attested := true
			switch scenario {
			case "no_attestation":
				attested = false
			case "keeper_is_target":
				targets = append(targets, keeper)
			case "target_is_admin":
				adminExec(t, db, `UPDATE farmers SET admin_role='administrator' WHERE id=$1`, targets[0])
			case "different_phone":
				adminExec(t, db, `UPDATE farmers SET phone='2348030000102' WHERE id=$1`, targets[0])
			case "balance":
				adminExec(t, db, `UPDATE wallets SET balance=1 WHERE farmer_id=$1`, targets[0])
			case "audit_failure":
				adminExec(t, db, `CREATE FUNCTION reject_cleanup_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_cleanup_audit BEFORE INSERT ON admin_audit_events FOR EACH ROW EXECUTE FUNCTION reject_cleanup_audit()`)
			case "second_target_failure":
				adminExec(t, db, `UPDATE farmers SET admin_role='moderator' WHERE id=$1`, targets[1])
			}
			if _, err := r.CleanupTestDuplicates(context.Background(), keeper, targets, attested, true); err == nil {
				t.Fatal("unsafe cleanup succeeded")
			}
			var closed, records int
			if err := db.QueryRow(`SELECT count(*) FROM farmers WHERE deleted_at IS NOT NULL`).Scan(&closed); err != nil || closed != 0 {
				t.Fatal("failed cleanup closed an account")
			}
			if err := db.QueryRow(`SELECT count(*) FROM wallet_deposits`).Scan(&records); err != nil || records != 1 {
				t.Fatal("failed cleanup removed test records")
			}
			if adminAuditCount(t, db) != 0 {
				t.Fatal("failed cleanup persisted committed audit")
			}
		})
	}
}

func TestPhoneStandardMigrationRefusesAmbiguityAndMatchesNormalizer(t *testing.T) {
	db, _, _, _ := duplicateFixture(t)
	if f, err := NewFarmerRepository(db).GetByPhone("08030000101"); f != nil || !errors.Is(err, ErrAmbiguousPhone) {
		t.Fatal("ambiguous lookup chose an account")
	}
	if err := database.RunPhoneMigration(db); err == nil {
		t.Fatal("migration silently chose a duplicate")
	}
	var formatted int
	if err := db.QueryRow(`SELECT count(*) FROM farmers WHERE phone LIKE '% %'`).Scan(&formatted); err != nil || formatted != 2 {
		t.Fatal("refused migration rewrote contacts")
	}
	for _, raw := range []string{"08012345678", "+234 (801) 234-5678", "002348012345678", "+23408012345678", "8012345678", "2348012345678", "abc08012345678", "08012345678@", "+18012345678", "++2348012345678", "12345", "", "0801\t234\n5678", "0801\u00a02345678", "\u20038012345678"} {
		var got sql.NullString
		if err := db.QueryRow(`SELECT normalized_nigerian_phone($1)`, raw).Scan(&got); err != nil || got.String != phone.Normalize(raw) {
			t.Fatalf("normalization mismatch for %q: %q %v", raw, got.String, err)
		}
	}
}

func TestCanonicalPhoneConcurrentRegistrationAndLegacyUniqueness(t *testing.T) {
	db := testDB(t)
	a := adminFixtureUser(t, db, "Phone fixture")
	adminExec(t, db, `UPDATE farmers SET phone='0803 000 0101' WHERE id=$1`, a.ID)
	if err := database.RunPhoneMigration(db); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"08030000101", "+2348030000101", "234 803 000 0101"} {
		copy := *a
		copy.ID = 0
		copy.Phone = raw
		if err := NewFarmerRepository(db).Create(&copy); !errors.Is(err, ErrPhoneExists) {
			t.Fatalf("format created another account: %v", err)
		}
	}
	if _, err := db.Exec(`UPDATE farmers SET phone='abc08030000101' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.RunPhoneMigration(db); err != nil {
		t.Fatal(err)
	}
	var invalid string
	if err := db.QueryRow(`SELECT phone FROM farmers WHERE id=$1`, a.ID).Scan(&invalid); err != nil || invalid != "abc08030000101" {
		t.Fatal("invalid legacy number was invented")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, raw := range []string{"08031234567", "+234 (803) 123-4567"} {
		wg.Add(1)
		go func(raw string) {
			defer wg.Done()
			copy := *a
			copy.ID = 0
			copy.Phone = raw
			results <- NewFarmerRepository(db).Create(&copy)
		}(raw)
	}
	wg.Wait()
	close(results)
	ok, duplicate := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrPhoneExists) {
			duplicate++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || duplicate != 1 {
		t.Fatal("concurrent formats did not produce exactly one account")
	}
}
