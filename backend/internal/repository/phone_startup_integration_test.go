package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"backend/internal/database"

	"github.com/jackc/pgx/v5/pgconn"
)

func phoneAccountSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot string
	if err := db.QueryRow(`SELECT jsonb_build_object(
 'farmers',(SELECT jsonb_agg(to_jsonb(f) ORDER BY id) FROM farmers f),
 'wallets',(SELECT jsonb_agg(to_jsonb(w) ORDER BY id) FROM wallets w),
 'deposits',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM wallet_deposits d),
 'ledger',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM wallet_transactions l),
 'funding',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM wallet_virtual_accounts v),
 'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s)
 )::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestStartupMigrationPreservesAmbiguousPhoneAccounts(t *testing.T) {
	db, _, _, targets := duplicateFixture(t)
	unrelated := adminFixtureUser(t, db, "Unrelated member")
	adminExec(t, db, `UPDATE farmers SET phone='+234 (803) 000-0202' WHERE id=$1`, unrelated.ID)
	adminExec(t, db, `UPDATE wallets SET balance=123.45 WHERE farmer_id=$1`, targets[0])
	// Reproduce the first deployment of phone normalization, not just a rerun
	// against a database where the lookup function already happens to exist.
	adminExec(t, db, `DROP FUNCTION normalized_nigerian_phone(TEXT)`)
	before := phoneAccountSnapshot(t, db)
	for range 2 {
		if err := database.RunMigration(db); err != nil {
			t.Fatalf("startup failed on existing duplicate phones: %v", err)
		}
		if got := phoneAccountSnapshot(t, db); got != before {
			t.Fatal("startup changed account ownership, credentials, sessions or financial records")
		}
		if f, err := NewFarmerRepository(db).GetByPhone("08030000101"); f != nil || !errors.Is(err, ErrAmbiguousPhone) {
			t.Fatalf("ambiguous phone selected an account: %v", err)
		}
		if f, err := NewFarmerRepository(db).GetByPhone("08030000202"); err != nil || f == nil || f.ID != unrelated.ID {
			t.Fatalf("unrelated member cannot sign in by formatted phone: %v", err)
		}
	}
	if adminAuditCount(t, db) != 0 {
		t.Fatal("deferred migration claimed it changed account data")
	}
	if err := database.RunPhoneMigration(db); err == nil {
		t.Fatal("strict repair migration silently accepted ambiguous ownership")
	}
}

func TestStartupPhoneReservationsProtectOwnershipAndReleaseUnusedPhones(t *testing.T) {
	db, _, _, _ := duplicateFixture(t)
	if err := database.RunMigration(db); err != nil {
		t.Fatal(err)
	}
	f := adminFixtureUser(t, db, "New member")
	for _, raw := range []string{"2348030000101", "08030000101", "+2348030000101"} {
		copy := *f
		copy.Phone = raw
		if err := NewFarmerRepository(db).Create(&copy); !errors.Is(err, ErrPhoneExists) {
			t.Fatalf("new signup claimed a legacy phone: %v", err)
		}
		if _, err := db.Exec(`UPDATE farmers SET phone=$1 WHERE id=$2`, raw, f.ID); !isUniqueViolation(err) {
			t.Fatalf("phone change claimed a legacy phone: %v", err)
		}
	}
	adminExec(t, db, `UPDATE farmers SET phone='0803 000 0203' WHERE id=$1`, f.ID)
	adminExec(t, db, `UPDATE farmers SET phone='+2348030000203' WHERE id=$1`, f.ID)
	var owners int
	if err := db.QueryRow(`SELECT owner_count FROM farmer_phone_ownership WHERE phone='2348030000203'`).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("format-only phone edit changed ownership: %d %v", owners, err)
	}
	// Soft deletion frees a number; reactivation cannot reclaim it from its
	// new owner. A failed reactivation must leave the account closed.
	adminExec(t, db, `UPDATE farmers SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1`, f.ID)
	newOwner := *f
	newOwner.Phone = "08030000203"
	if err := NewFarmerRepository(db).Create(&newOwner); err != nil {
		t.Fatalf("unused phone was not released: %v", err)
	}
	if _, err := db.Exec(`UPDATE farmers SET deleted_at=NULL WHERE id=$1`, f.ID); !isUniqueViolation(err) {
		t.Fatalf("reactivation stole a phone: %v", err)
	}
	if stored, err := NewFarmerRepository(db).GetByID(f.ID); err != nil || stored != nil {
		t.Fatal("failed reactivation changed account state")
	}
	stored, err := NewFarmerRepository(db).GetByPhone("+2348030000203")
	if err != nil || stored == nil || stored.ID != newOwner.ID {
		t.Fatalf("reactivation changed the new owner's identity: %v", err)
	}
}

func TestStartupPhoneReservationsSerializeConcurrentClaims(t *testing.T) {
	for _, level := range []sql.IsolationLevel{sql.LevelReadCommitted, sql.LevelRepeatableRead, sql.LevelSerializable} {
		t.Run(level.String(), func(t *testing.T) {
			db, _, _, _ := duplicateFixture(t)
			if err := database.RunMigration(db); err != nil {
				t.Fatal(err)
			}
			first := adminFixtureUser(t, db, "First claimant")
			second := adminFixtureUser(t, db, "Second claimant")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx1, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: level})
			if err != nil {
				t.Fatal(err)
			}
			defer tx1.Rollback()
			tx2, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: level})
			if err != nil {
				t.Fatal(err)
			}
			defer tx2.Rollback()
			var pid int
			if err := tx2.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM farmers LIMIT 1`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			if _, err := tx1.ExecContext(ctx, `UPDATE farmers SET phone='0803 000 0303' WHERE id=$1`, first.ID); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := tx2.ExecContext(ctx, `UPDATE farmers SET phone='+2348030000303' WHERE id=$1`, second.ID)
				result <- err
			}()
			// Prove that the second claim waits for the first transaction; merely
			// starting two goroutines would not guarantee overlapping claims.
			for {
				var waiting bool
				if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE pid=$1 AND NOT granted AND locktype='transactionid')`, pid).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("second claim did not wait: %v", err)
				case <-ctx.Done():
					t.Fatal("second claim never reached the uniqueness lock")
				case <-time.After(10 * time.Millisecond):
				}
			}
			if err := tx1.Commit(); err != nil {
				t.Fatal(err)
			}
			claimErr := <-result
			var pgErr *pgconn.PgError
			serialized := level == sql.LevelSerializable && errors.As(claimErr, &pgErr) && pgErr.Code == "40001"
			if !isUniqueViolation(claimErr) && !serialized {
				t.Fatalf("second differently formatted claim was not rejected: %v", claimErr)
			}
			_ = tx2.Rollback()
			if _, err := db.Exec(`UPDATE farmers SET phone='+2348030000303' WHERE id=$1`, second.ID); !isUniqueViolation(err) {
				t.Fatalf("retry claimed the committed phone: %v", err)
			}
			stored, err := NewFarmerRepository(db).GetByID(second.ID)
			if err != nil || stored == nil || stored.Phone != second.Phone {
				t.Fatal("failed claim changed the second account")
			}
			var owners int
			if err := db.QueryRow(`SELECT owner_count FROM farmer_phone_ownership WHERE phone=$1`, second.Phone).Scan(&owners); err != nil || owners != 1 {
				t.Fatal("failed claim released the second account's original phone")
			}
			owner, err := NewFarmerRepository(db).GetByPhone("08030000303")
			if err != nil || owner == nil || owner.ID != first.ID {
				t.Fatal("concurrent claims produced ambiguous ownership")
			}
		})
	}
}

func TestStartupPhoneReservationsUpgradeToUniqueIndexAfterResolution(t *testing.T) {
	db, r, keeper, targets := duplicateFixture(t)
	if err := database.RunMigration(db); err != nil {
		t.Fatal(err)
	}
	before := phoneAccountSnapshot(t, db)
	if _, err := r.CleanupTestDuplicates(context.Background(), keeper, targets, true, false); err != nil {
		t.Fatal(err)
	}
	if phoneAccountSnapshot(t, db) != before {
		t.Fatal("dry-run repair changed accounts")
	}
	var owners int
	if err := db.QueryRow(`SELECT owner_count FROM farmer_phone_ownership WHERE phone='2348030000101'`).Scan(&owners); err != nil || owners != 3 {
		t.Fatal("dry-run repair changed phone reservations")
	}
	if _, err := r.CleanupTestDuplicates(context.Background(), keeper, targets, true, true); err != nil {
		t.Fatal(err)
	}
	if err := database.RunMigration(db); err != nil {
		t.Fatal(err)
	}
	var upgraded bool
	if err := db.QueryRow(`SELECT to_regclass('farmer_phone_ownership') IS NULL
 AND EXISTS(SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('idx_farmers_normalized_phone') AND indisunique)
 AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='farmers'::regclass AND tgname='farmers_legacy_phone_ownership')`).Scan(&upgraded); err != nil || !upgraded {
		t.Fatal("resolved ownership did not automatically restore the full uniqueness index")
	}
}

func TestStartupPhoneMigrationStillFailsOnDatabaseErrors(t *testing.T) {
	db, _, _, _ := duplicateFixture(t)
	adminExec(t, db, `CREATE VIEW farmer_phone_ownership AS SELECT phone,1::bigint AS owner_count FROM farmers`)
	before := phoneAccountSnapshot(t, db)
	if err := database.RunMigration(db); err == nil {
		t.Fatal("startup ignored failure to install phone ownership protection")
	}
	if phoneAccountSnapshot(t, db) != before {
		t.Fatal("failed phone migration changed account or financial data")
	}
}
