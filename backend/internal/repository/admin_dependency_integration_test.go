package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/database"
	"backend/internal/models"
)

func TestAdminDisputeReviewAccessAndConcurrentRevocation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	root := adminFixtureUser(t, db, "Root")
	reviewer := adminFixtureUser(t, db, "Reviewer")
	wallet := adminFixtureUser(t, db, "Wallet approver")
	adminExec(t, db, `UPDATE farmers SET admin_role='administrator' WHERE id=$1`, reviewer.ID)
	adminExec(t, db, `UPDATE farmers SET is_admin=true WHERE id=$1`, wallet.ID)
	governance := NewAdminRepository(db, []int{root.ID})
	access := NewDisputeReviewAccess(governance)
	if !access.IsAdmin(root.ID) || !access.IsAdmin(reviewer.ID) || access.IsAdmin(wallet.ID) || NewDisputeReviewAccess(nil).IsAdmin(root.ID) {
		t.Fatal("dispute permission differs from governance")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := access.AuthorizeTx(ctx, tx, reviewer.ID); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(started)
		finished <- governance.ChangeUser(ctx, root.ID, models.AdminChange{TargetID: reviewer.ID, Action: "role_none", Reason: "staff_assignment"})
	}()
	<-started
	select {
	case err := <-finished:
		t.Fatalf("permission changed during authorized transaction: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("role revocation did not finish")
	}
	if access.IsAdmin(reviewer.ID) {
		t.Fatal("revoked reviewer retained access")
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := access.AuthorizeTx(ctx, tx, reviewer.ID); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("direct transaction accepted a revoked reviewer")
	}
}

func TestAdminDisputeAuditBridgeIsAtomicAndAppendOnly(t *testing.T) {
	db := testDB(t)
	actor := adminFixtureUser(t, db, "Review administrator")
	buyer := adminFixtureUser(t, db, "Buyer")
	seller := adminFixtureUser(t, db, "Seller")
	crop := adminFixtureCrop(t, db, seller.ID)
	order := &models.Order{BuyerID: buyer.ID, CropID: crop.ID, Quantity: 1, TotalPrice: 1000, Status: "paid"}
	if err := NewOrderRepository(db).Create(order); err != nil {
		t.Fatal(err)
	}
	var disputeID int64
	if err := db.QueryRow(`INSERT INTO order_disputes(order_id,buyer_id,seller_id,reason_code,description,requested_amount) VALUES($1,$2,$3,'missing_quantity','Synthetic complaint',100) RETURNING id`, order.ID, buyer.ID, seller.ID).Scan(&disputeID); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO order_dispute_events(dispute_id,actor_id,actor_role,event_type,from_status,to_status,note) VALUES($1,$2,'admin','admin_review_started','submitted','under_review','private fixture text')`
	adminExec(t, db, insert, disputeID, actor.ID)
	var recordedActor int
	var action, reason string
	if err := db.QueryRow(`SELECT actor_id,action,reason FROM admin_audit_events WHERE resource='order_disputes' AND target_id=$1`, disputeID).Scan(&recordedActor, &action, &reason); err != nil || recordedActor != actor.ID || action != "dispute.review" || reason != "admin_review_started" {
		t.Fatal("dispute event was not mirrored into attributable governance audit")
	}
	for _, query := range []string{`UPDATE order_dispute_events SET note='changed'`, `DELETE FROM order_dispute_events`, `TRUNCATE order_dispute_events`} {
		if _, err := db.Exec(query); err == nil {
			t.Fatal("dispute history could be modified")
		}
	}
	if _, err := db.Exec(insert, disputeID, nil); err == nil {
		t.Fatal("administrative event had no actor")
	}
	if err := database.RunAdminMigration(db); err != nil {
		t.Fatal(err)
	}
	if adminAuditCount(t, db) != 1 {
		t.Fatal("repeat migration duplicated history")
	}
	adminExec(t, db, `CREATE FUNCTION test_fail_dispute_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='order_disputes' THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_fail_dispute_audit BEFORE INSERT ON admin_audit_events FOR EACH ROW EXECUTE FUNCTION test_fail_dispute_audit()`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE order_disputes SET status='under_review' WHERE id=$1`, disputeID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(insert, disputeID, actor.ID); err == nil {
		t.Fatal("decision succeeded without its governance audit")
	}
	_ = tx.Rollback()
	var status string
	if err := db.QueryRow(`SELECT status FROM order_disputes WHERE id=$1`, disputeID).Scan(&status); err != nil || status != "submitted" || adminAuditCount(t, db) != 1 {
		t.Fatal("audit failure did not roll back the decision")
	}
}

func TestSessionLoginRevisionAndTimezoneIndependentExpiry(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	user := adminFixtureUser(t, db, "Session member")
	sessions := NewSessionRepository(db)
	ctx := context.Background()
	adminExec(t, db, `SET TIME ZONE 'America/Los_Angeles'`)
	if err := sessions.CreateForRevision(ctx, "original", user.ID, time.Now().Add(time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	adminExec(t, db, `UPDATE farmers SET sessions_revoked_at=clock_timestamp() WHERE id=$1`, user.ID)
	if _, err := sessions.FindValid(ctx, "original"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("revision change retained an old session")
	}
	if err := sessions.CreateForRevision(ctx, "stale", user.ID, time.Now().Add(time.Hour), time.Time{}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("stale password-check revision minted a session")
	}
	f, err := NewFarmerRepository(db).GetByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.CreateForRevision(ctx, "fresh", user.ID, time.Now().Add(time.Hour), f.SessionsRevokedAt.Time); err != nil {
		t.Fatal(err)
	}
	adminExec(t, db, `SET TIME ZONE 'Asia/Tokyo'`)
	if _, err := sessions.FindValid(ctx, "fresh"); err != nil {
		t.Fatal("timezone change expired a live session")
	}
	if err := sessions.Create(ctx, "expired", user.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.FindValid(ctx, "expired"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("timezone change revived an expired session")
	}
	adminExec(t, db, `INSERT INTO sessions(session_token_hash,farmer_id,expires_at) VALUES('historical-unknown',$1,CURRENT_TIMESTAMP+interval '1 day')`, user.ID)
	if _, err := sessions.FindValid(ctx, "historical-unknown"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("session without a known UTC expiry was accepted")
	}
}

func TestPasswordResetAtomicallyRevokesSessions(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	farmers := NewFarmerRepository(db)
	user := adminFixtureUser(t, db, "Password recovery member")
	other := adminFixtureUser(t, db, "Unaffected member")
	sessions := NewSessionRepository(db)
	expires := time.Now().Add(time.Hour)
	for token, id := range map[string]int{"first": user.ID, "second": user.ID, "other": other.ID} {
		if err := sessions.CreateForRevision(ctx, token, id, expires, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := farmers.GetByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A failed session write must leave both the password and logins untouched.
	adminExec(t, db, `CREATE FUNCTION reject_reset_revocation() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN RAISE EXCEPTION 'synthetic revocation failure'; END $$;
        CREATE TRIGGER reject_reset_revocation BEFORE UPDATE ON sessions
        FOR EACH STATEMENT EXECUTE FUNCTION reject_reset_revocation()`)
	if err := farmers.UpdatePassword(user.ID, "replacement-hash"); err == nil {
		t.Fatal("password reset succeeded without session revocation")
	}
	after, err := farmers.GetByID(user.ID)
	if err != nil || after.PasswordHash != before.PasswordHash || after.SessionsRevokedAt != before.SessionsRevokedAt {
		t.Fatal("failed reset changed password or authentication revision")
	}
	adminExec(t, db, `DROP TRIGGER reject_reset_revocation ON sessions`)
	for _, token := range []string{"first", "second", "other"} {
		if _, err := sessions.FindValid(ctx, token); err != nil {
			t.Fatal("failed reset invalidated a login")
		}
	}
	if err := farmers.UpdatePassword(user.ID, "replacement-hash"); err != nil {
		t.Fatal(err)
	}
	after, err = farmers.GetByID(user.ID)
	if err != nil || after.PasswordHash != "replacement-hash" || !after.SessionsRevokedAt.Valid {
		t.Fatal("successful reset did not update password and revision")
	}
	for _, token := range []string{"first", "second"} {
		if _, err := sessions.FindValid(ctx, token); !errors.Is(err, ErrSessionNotFound) {
			t.Fatal("password reset retained an old login")
		}
	}
	if _, err := sessions.FindValid(ctx, "other"); err != nil {
		t.Fatal("password reset invalidated another account's login")
	}
	if err := sessions.CreateForRevision(ctx, "stale-password", user.ID, expires, before.SessionsRevokedAt.Time); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("password authentication from before reset minted a new login")
	}
	if err := sessions.CreateForRevision(ctx, "fresh-password", user.ID, expires, after.SessionsRevokedAt.Time); err != nil {
		t.Fatal(err)
	}
	previousRevision := after.SessionsRevokedAt.Time
	if err := farmers.UpdatePassword(user.ID, "second-replacement-hash"); err != nil {
		t.Fatal(err)
	}
	after, err = farmers.GetByID(user.ID)
	if err != nil || !after.SessionsRevokedAt.Time.After(previousRevision) {
		t.Fatal("a second reset did not advance authentication revision")
	}
	if err := sessions.CreateForRevision(ctx, "stale-second-password", user.ID, expires, previousRevision); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("second reset accepted stale password authentication")
	}
	if _, err := sessions.FindValid(ctx, "fresh-password"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("second reset retained the previous login")
	}
	if err := farmers.UpdatePassword(-1, "unused"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatal("reset of missing account reported success")
	}
}
