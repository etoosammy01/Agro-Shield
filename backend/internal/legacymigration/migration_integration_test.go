package legacymigration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/legacymigration"
	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/internal/testdb"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func fixture(t *testing.T) (*sql.DB, legacymigration.Options) {
	t.Helper()
	db := testdb.Open(t)
	var target string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	source := target + "_legacy"
	if _, err := db.Exec("CREATE SCHEMA " + pgx.Identifier{source}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA " + pgx.Identifier{source}.Sanitize() + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	ddl, err := os.ReadFile("testdata/normalized_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(strings.ReplaceAll(string(ddl), "{{source}}", pgx.Identifier{source}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	o := legacymigration.Options{Source: source, Target: target}
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-source-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, o, `INSERT INTO {{source}}.states(id,name) VALUES(1,'Benue'); INSERT INTO {{source}}.lgas(id,state_id,name) VALUES(1,1,'Makurdi'); INSERT INTO {{source}}.communities(id,lga_id,name) VALUES(1,1,'Test community')`)
	for _, u := range []struct {
		id          int
		phone, role string
		admin       bool
	}{{3, "2348030000003", "farmer", false}, {17, "2348030000017", "buyer", true}} {
		query := replace(o, `INSERT INTO {{source}}.users(id,full_name,phone,password_hash,role,country,location,state_id,lga_id,community_id,is_admin) VALUES($1,'Synthetic source account',$2,$3,$4,'Nigeria','Test community',1,1,1,$5)`)
		if _, err := db.Exec(query, u.id, u.phone, string(hash), u.role, u.admin); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, db, o, `
CREATE TABLE {{source}}.payments(id INTEGER PRIMARY KEY,order_id INTEGER NOT NULL,reference TEXT NOT NULL,transaction_id TEXT,amount NUMERIC NOT NULL,currency TEXT NOT NULL,status TEXT NOT NULL,provider TEXT NOT NULL,created_at TIMESTAMP NOT NULL DEFAULT '2026-01-01',updated_at TIMESTAMP NOT NULL DEFAULT '2026-01-01');
INSERT INTO {{source}}.wallets(id,owner_type,owner_id,balance,currency,wallet_number) VALUES(99,'user',3,250.00,'NGN','8000000003'),(104,'user',17,1250.00,'NGN','8000000017');
INSERT INTO {{source}}.user_bank_accounts(user_id,bank_name,bank_code,account_name,account_number) VALUES(3,'Synthetic bank','999','Synthetic source account','0123456789');
INSERT INTO {{source}}.products(id,owner_type,owner_id,name,quantity,unit,price,status,state_id,lga_id,community_id) VALUES(42,'user',3,'Synthetic maize',9,'kg',250.00,'active',1,1,1);
INSERT INTO {{source}}.product_images(id,product_id,image_url,is_primary,sort_order) VALUES(5,42,'/fixture-primary.jpg',true,0),(6,42,'/fixture-secondary.jpg',false,1);
INSERT INTO {{source}}.product_price_history(id,product_id,price,unit,source) VALUES(12,42,250.00,'kg','Synthetic market');
INSERT INTO {{source}}.inventory_history(id,product_id,quantity_before,quantity_change,quantity_after,reason) VALUES(13,42,10,-1,9,'order');
INSERT INTO {{source}}.orders(id,buyer_id,status,total_amount,checkout_key,currency) VALUES(81,17,'delivered',250.00,'synthetic-old-checkout','NGN');
INSERT INTO {{source}}.order_items(id,order_id,product_id,quantity,unit_price,total_price) VALUES(77,81,42,1,250.00,250.00);
INSERT INTO {{source}}.payments(id,order_id,reference,transaction_id,amount,currency,status,provider) VALUES(82,81,'OLD-PAYMENT','OLD-PAYMENT-PROVIDER',250,'NGN','successful','flutterwave');
INSERT INTO {{source}}.wallet_withdrawals(id,wallet_id,user_id,reference,amount,currency,bank_name,bank_code,account_name,account_number,status) VALUES(61,99,3,'OLD-WITHDRAWAL',50,'NGN','Synthetic bank','999','Synthetic source account','0123456789','pending');
INSERT INTO {{source}}.wallet_transfers(id,sender_wallet_id,sender_id,recipient_wallet_id,recipient_id,recipient_phone,reference,amount,currency,status) VALUES(71,104,17,99,3,'2348030000003','OLD-TRANSFER',25,'NGN','pending');
INSERT INTO {{source}}.wallet_deposits(id,wallet_id,provider,reference,provider_transaction_id,amount,currency,verified_amount,verified_currency,status) VALUES(31,104,'flutterwave','OLD-001','OLD-PROVIDER-001',1500,'NGN',1500,'NGN','successful'),(32,104,'flutterwave','OLD-002','OLD-PROVIDER-002',100,'NGN',100,'NGN','reversed');
INSERT INTO {{source}}.wallet_deposits(id,wallet_id,provider,reference,amount,currency,status) VALUES(33,104,'flutterwave','OLD-PENDING',500,'NGN','pending');
INSERT INTO {{source}}.wallet_virtual_accounts(id,user_id,provider,tx_ref,provider_ref,account_number,bank_name,account_name,currency,status,identity_kind,consent_version) VALUES(41,3,'flutterwave','OLD-VA','OLD-VA-PROVIDER','0123456789','Synthetic bank','Synthetic source account','NGN','active','nin','synthetic-consent-v1');
INSERT INTO {{source}}.wallet_transactions(id,wallet_id,type,amount,reference) VALUES(51,104,'deposit',1500,'deposit:OLD-001'),(52,104,'deposit',100,'deposit:OLD-002'),(53,104,'deposit_reversal',100,'deposit-reversal:OLD-002'),(54,104,'order_payment',250,'old-order:81'),(55,99,'sale_credit',250,'old-sale:81');
INSERT INTO {{source}}.market_events(id,event_type,product_id,user_id,metadata) VALUES(91,'listing_view',42,17,'{}'),(92,'checkout_completed',42,17,'{"quantity":1}');
INSERT INTO {{source}}.negotiations(id,product_id,buyer_id,quantity,initial_offer_price,current_offer_price,max_rounds,status) VALUES(20,42,17,1,200,250,5,'accepted');
INSERT INTO {{source}}.negotiation_messages(id,negotiation_id,sender_id,message_type,message,offer_price,offer_status) VALUES(21,20,17,'offer','Synthetic offer',250,'accepted');
INSERT INTO {{source}}.diagnoses(id,user_id,category,symptoms,result) VALUES(7,3,'crop','Synthetic symptoms','Synthetic result');
INSERT INTO {{source}}.notifications(id,user_id,notification_type,title,message) VALUES(8,17,'order','Synthetic order','Synthetic message');
INSERT INTO {{source}}.conversations(id,conversation_type,title,created_by) VALUES(11,'private','Synthetic conversation',3);
INSERT INTO {{source}}.conversation_members(id,conversation_id,user_id,left_at) VALUES(15,11,3,NULL),(16,11,17,NULL),(18,11,17,'2026-01-02');
INSERT INTO {{source}}.chat_messages(id,conversation_id,sender_id,message) VALUES(19,11,17,'Synthetic chat');
INSERT INTO {{source}}.user_sessions(token_hash,user_id,expires_at) VALUES('synthetic-old-token',17,'2027-01-01T00:00:00Z');
`)
	return db, o
}

func replace(o legacymigration.Options, q string) string {
	return strings.NewReplacer("{{source}}", pgx.Identifier{o.Source}.Sanitize(), "{{target}}", pgx.Identifier{o.Target}.Sanitize()).Replace(q)
}
func exec(t *testing.T, db *sql.DB, o legacymigration.Options, q string) {
	t.Helper()
	if _, err := db.Exec(replace(o, q)); err != nil {
		t.Fatal(err)
	}
}
func count(t *testing.T, db *sql.DB, o legacymigration.Options, q string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(replace(o, q)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLegacyMigrationPreservesRecordsAndRole6Boundaries(t *testing.T) {
	db, o := fixture(t)
	ctx := context.Background()
	plan, err := legacymigration.Plan(ctx, db, o)
	if err != nil || len(plan.Issues) > 0 {
		t.Fatalf("preflight: %+v %v", plan.Issues, err)
	}
	if count(t, db, o, `SELECT count(*) FROM {{target}}.farmers`) != 0 {
		t.Fatal("read-only preflight copied accounts")
	}
	o.ExpectedFingerprint = plan.Fingerprint
	report, err := legacymigration.Apply(ctx, db, o)
	if err != nil || !report.Committed {
		t.Fatalf("apply: %+v %v", report, err)
	}
	if report.Copied["farmers"] != 2 || report.Copied["wallets"] != 2 || report.Copied["crops"] != 1 || report.Copied["orders"] != 1 || report.Copied["wallet_transactions"] != 5 || report.Copied["conversation_members"] != 2 {
		t.Fatalf("wrong import counts: %+v", report.Copied)
	}
	for name, passed := range report.Checks {
		if !passed {
			t.Fatalf("verification failed: %s", name)
		}
	}
	if count(t, db, o, `SELECT count(*) FROM {{target}}.admin_audit_events WHERE action='system.legacy_import' AND actor_id IS NULL AND outcome='committed'`) != 1 {
		t.Fatal("missing attributable import event")
	}
	if count(t, db, o, `SELECT count(*) FROM {{target}}.legacy_import_mappings WHERE source_table='order_items' AND source_id=77 AND target_id=81`) != 1 {
		t.Fatal("order-item mapping missing")
	}
	if count(t, db, o, `SELECT count(*) FROM {{target}}.sessions`) != 0 {
		t.Fatal("old source sessions became valid")
	}
	farmers := repository.NewFarmerRepository(db)
	buyer, err := services.NewAuthService(farmers).Login("2348030000017", "synthetic-source-password")
	if err != nil || buyer.ID != 17 {
		t.Fatalf("existing credentials lost: %v", err)
	}
	if buyer.HasTransactionPIN || buyer.PhoneVerifiedAt.Valid || buyer.SelfieCapturedAt.Valid {
		t.Fatal("import invented onboarding evidence")
	}
	access, err := repository.NewAdminRepository(db, nil).Access(ctx, 17)
	if err != nil || !access.Can(models.AdminManageWallet) || access.Can(models.AdminReadAudit) || access.Can(models.AdminManageUsers) {
		t.Fatalf("legacy wallet approver escalated: %+v %v", access, err)
	}
	root := &models.Farmer{FullName: "Synthetic new administrator", Phone: "2348030000018", Role: "farmer", Country: "Nigeria", Location: "Test community", PasswordHash: "synthetic-hash", PhotoURL: "/fixture.jpg"}
	if err := farmers.Create(root); err != nil {
		t.Fatal(err)
	}
	if root.ID <= 17 {
		t.Fatal("account identity sequence was not advanced")
	}
	admin := repository.NewAdminRepository(db, []int{root.ID})
	users, err := admin.Users(ctx, root.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(users)
	if strings.Contains(string(encoded), "2348030000017") || strings.Contains(string(encoded), "password_hash") {
		t.Fatal("import exposed source credentials in the admin directory")
	}
	change := models.AdminChange{TargetID: 42, Version: 0, Action: "block", Reason: "incorrect_listing"}
	if err := admin.ChangeListing(ctx, root.ID, change); err != nil {
		t.Fatal(err)
	}
	exec(t, db, o, `UPDATE {{target}}.crops SET quantity=8 WHERE id=42`)
	change.Action = "restore"
	change.Version = 1
	if err := admin.ChangeListing(ctx, root.ID, change); !errors.Is(err, repository.ErrAdminConflict) {
		t.Fatalf("imported listing accepted stale review: %v", err)
	}
	change.Version = 2
	if err := admin.ChangeListing(ctx, root.ID, change); err != nil {
		t.Fatal(err)
	}
	sessions := repository.NewSessionRepository(db)
	if err := sessions.Create(ctx, "fresh-post-import", 3, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := admin.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: 3, Action: "suspend", Reason: "policy_violation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.FindValid(ctx, "fresh-post-import"); !errors.Is(err, repository.ErrSessionNotFound) {
		t.Fatal("suspending an imported account did not revoke its session")
	}
	if _, err := db.Exec(replace(o, `UPDATE {{target}}.admin_audit_events SET reason='tampered'`)); err == nil {
		t.Fatal("import weakened append-only audit protection")
	}
	// The source remains byte-for-byte equivalent to the reviewed snapshot.
	after, err := legacymigration.Plan(ctx, db, o)
	if err != nil || after.Fingerprint != plan.Fingerprint || !after.AlreadyApplied {
		t.Fatalf("source altered: %+v %v", after, err)
	}
	again, err := legacymigration.Apply(ctx, db, o)
	if err != nil || !again.AlreadyApplied {
		t.Fatalf("idempotent rerun: %+v %v", again, err)
	}
	if count(t, db, o, `SELECT count(*) FROM {{target}}.wallet_transactions`) != 5 {
		t.Fatal("rerun replayed financial activity")
	}
}

func TestLegacyMigrationRollsBackAndCanRetry(t *testing.T) {
	db, o := fixture(t)
	ctx := context.Background()
	plan, err := legacymigration.Plan(ctx, db, o)
	if err != nil {
		t.Fatal(err)
	}
	o.ExpectedFingerprint = plan.Fingerprint
	exec(t, db, o, `CREATE FUNCTION {{target}}.reject_import_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$; CREATE TRIGGER reject_import_audit BEFORE INSERT ON {{target}}.admin_audit_events FOR EACH ROW EXECUTE FUNCTION {{target}}.reject_import_audit()`)
	if report, err := legacymigration.Apply(ctx, db, o); err == nil || report.Committed {
		t.Fatal("import committed without its audit event")
	}
	for _, table := range []string{"farmers", "crops", "orders", "wallets", "wallet_transactions", "legacy_import_mappings", "legacy_wallet_numbers", "legacy_import_runs", "admin_audit_events"} {
		if count(t, db, o, "SELECT count(*) FROM {{target}}."+table) != 0 {
			t.Fatalf("rollback left rows in %s", table)
		}
	}
	exec(t, db, o, `DROP TRIGGER reject_import_audit ON {{target}}.admin_audit_events; DROP FUNCTION {{target}}.reject_import_audit()`)
	if report, err := legacymigration.Apply(ctx, db, o); err != nil || !report.Committed {
		t.Fatalf("retry after repair: %+v %v", report, err)
	}
	if count(t, db, o, `SELECT count(*) FROM {{target}}.wallet_transactions`) != 5 {
		t.Fatal("retry duplicated the ledger")
	}
}

func TestLegacyMigrationRejectsAmbiguityAndSnapshotChanges(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"organization ownership", `UPDATE {{source}}.products SET owner_type='organization' WHERE id=42`},
		{"unknown owner", `UPDATE {{source}}.products SET owner_id=999 WHERE id=42`},
		{"multi-item order", `INSERT INTO {{source}}.order_items(order_id,product_id,quantity,unit_price,total_price) VALUES(81,42,1,250,250)`},
		{"unrepresentable balance", `UPDATE {{source}}.wallets SET balance=1250.001 WHERE id=104`},
		{"ambiguous bank account", `INSERT INTO {{source}}.user_bank_accounts(user_id,bank_name,bank_code,account_name,account_number) VALUES(3,'Another bank','998','Synthetic account','1234567890')`},
		{"generic ledger type", `UPDATE {{source}}.wallet_transactions SET type='credit' WHERE id=51`},
		{"unrepresentable payment", `UPDATE {{source}}.payments SET amount=250.001 WHERE id=82`},
		{"unrepresentable negotiation price", `UPDATE {{source}}.negotiations SET current_offer_price=250.001 WHERE id=20`},
		{"generated number collides with preserved number", `UPDATE {{source}}.wallets SET wallet_number='OLD-USER-3' WHERE id=99; UPDATE {{source}}.wallets SET wallet_number='8000000003' WHERE id=104`},
		{"duplicate preserved wallet numbers", `UPDATE {{source}}.wallets SET wallet_number='8000000003' WHERE id=104`},
		{"destination already used", `INSERT INTO {{target}}.farmers(full_name,phone,password_hash,location,country) VALUES('Existing destination account','2348030999999','synthetic-hash','Test','Nigeria')`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, o := fixture(t)
			exec(t, db, o, tc.mutation)
			report, err := legacymigration.Plan(context.Background(), db, o)
			if err != nil || len(report.Issues) == 0 {
				t.Fatalf("unsafe preflight passed: %+v %v", report, err)
			}
			if _, err := legacymigration.Apply(context.Background(), db, o); !errors.Is(err, legacymigration.ErrUnsafeMigration) {
				t.Fatalf("unsafe import accepted: %v", err)
			}
		})
	}
	t.Run("reviewed fingerprint required", func(t *testing.T) {
		db, o := fixture(t)
		if _, err := legacymigration.Apply(context.Background(), db, o); !errors.Is(err, legacymigration.ErrUnsafeMigration) {
			t.Fatalf("import without reviewed snapshot accepted: %v", err)
		}
		if count(t, db, o, `SELECT count(*) FROM {{target}}.farmers`) != 0 {
			t.Fatal("unreviewed import copied accounts")
		}
	})
	t.Run("source changes after review", func(t *testing.T) {
		db, o := fixture(t)
		plan, err := legacymigration.Plan(context.Background(), db, o)
		if err != nil {
			t.Fatal(err)
		}
		o.ExpectedFingerprint = plan.Fingerprint
		exec(t, db, o, `UPDATE {{source}}.products SET price=275 WHERE id=42`)
		if _, err := legacymigration.Apply(context.Background(), db, o); !errors.Is(err, legacymigration.ErrUnsafeMigration) {
			t.Fatalf("changed snapshot accepted: %v", err)
		}
		if count(t, db, o, `SELECT count(*) FROM {{target}}.farmers`) != 0 {
			t.Fatal("changed snapshot partially imported")
		}
	})
}

func TestLegacyWalletNumberConversionPreservesOwnership(t *testing.T) {
	for _, tc := range []struct{ name, expression string }{
		{"prefixed identifier", `'AGW-U-20261116-ABC'`},
		{"empty identifier", `''`},
		{"missing identifier", `NULL`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, o := fixture(t)
			// A valid non-generated number must survive unchanged, including its
			// leading zero; digits inside an old identifier are never an alias.
			if tc.expression == "NULL" {
				exec(t, db, o, `ALTER TABLE {{source}}.wallets ALTER COLUMN wallet_number DROP NOT NULL`)
			}
			exec(t, db, o, `UPDATE {{source}}.wallets SET wallet_number=`+tc.expression+` WHERE id=99; UPDATE {{source}}.wallets SET wallet_number='0123456789' WHERE id=104`)
			plan, err := legacymigration.Plan(context.Background(), db, o)
			if err != nil || len(plan.Issues) > 0 {
				t.Fatalf("preflight rejected explicit conversion: %+v %v", plan.Issues, err)
			}
			o.ExpectedFingerprint = plan.Fingerprint
			report, err := legacymigration.Apply(context.Background(), db, o)
			if err != nil || !report.Committed {
				t.Fatalf("conversion failed: %+v %v", report, err)
			}
			if count(t, db, o, `SELECT count(*) FROM {{target}}.legacy_wallet_numbers n JOIN {{source}}.wallets w ON w.id=n.source_wallet_id JOIN {{target}}.farmers f ON f.id=n.target_user_id JOIN {{target}}.wallets d ON d.farmer_id=f.id WHERE n.legacy_number IS NOT DISTINCT FROM w.wallet_number AND n.source_user_id=w.owner_id AND d.id=w.id AND d.balance=w.balance AND ((f.id=3 AND f.wallet_number='8000000003' AND n.wallet_number=f.wallet_number AND n.conversion='generated') OR (f.id=17 AND f.wallet_number='0123456789' AND n.wallet_number=f.wallet_number AND n.conversion='preserved'))`) != 2 {
				t.Fatal("conversion lost original identifiers, leading zeros, or ownership")
			}
			after, err := legacymigration.Plan(context.Background(), db, o)
			if err != nil || after.Fingerprint != plan.Fingerprint {
				t.Fatal("conversion modified the source records")
			}
		})
	}
}

func TestLegacyMigrationSerializesConcurrentImports(t *testing.T) {
	db, o := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	plan, err := legacymigration.Plan(ctx, db, o)
	if err != nil || len(plan.Issues) > 0 {
		t.Fatalf("preflight: %+v %v", plan.Issues, err)
	}
	o.ExpectedFingerprint = plan.Fingerprint
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			r, err := legacymigration.Apply(ctx, db, o)
			if err == nil && !r.Committed {
				err = errors.New("concurrent import did not observe completion")
			}
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent import: %v", err)
		}
	}
	for _, q := range []string{
		`SELECT count(*) FROM {{target}}.legacy_import_runs`,
		`SELECT count(*) FROM {{target}}.admin_audit_events WHERE action='system.legacy_import'`,
	} {
		if count(t, db, o, q) != 1 {
			t.Fatal("simultaneous import duplicated the completion marker or audit")
		}
	}
	if count(t, db, o, `SELECT count(*) FROM {{target}}.wallet_transactions`) != 5 || count(t, db, o, `SELECT count(*) FROM {{target}}.legacy_wallet_numbers`) != 2 {
		t.Fatal("simultaneous import duplicated records")
	}
}
