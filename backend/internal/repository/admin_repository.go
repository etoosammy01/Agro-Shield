package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"backend/internal/models"
)

var (
	ErrAdminForbidden = errors.New("permission denied")
	ErrAdminConflict  = errors.New("this record changed; refresh and review it again")
	ErrAdminInvalid   = errors.New("invalid admin change")
	ErrAdminProtected = errors.New("you cannot change your own access or suspend an administrator; configured access is managed in server settings")
)

type AdminRepository struct {
	db         *sql.DB
	configured map[int]bool
}

func NewAdminRepository(db *sql.DB, configuredIDs []int) *AdminRepository {
	r := &AdminRepository{db: db, configured: make(map[int]bool)}
	for _, id := range configuredIDs {
		if id > 0 {
			r.configured[id] = true
		}
	}
	return r
}

type adminQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *AdminRepository) access(ctx context.Context, q adminQuerier, actorID int) (models.AdminAccess, error) {
	var a models.AdminAccess
	err := q.QueryRowContext(ctx, `SELECT admin_role, is_admin FROM farmers
 WHERE id = $1 AND deleted_at IS NULL AND suspended_at IS NULL`, actorID).Scan(&a.Role, &a.Wallet)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrAdminForbidden
	}
	if err != nil {
		return a, err
	}
	if r.configured[actorID] {
		a.Role, a.Wallet, a.Configured = "administrator", true, true
	}
	return a, nil
}

// Access always reads current account state; stale process caches cannot grant access.
func (r *AdminRepository) Access(ctx context.Context, actorID int) (models.AdminAccess, error) {
	if r == nil || r.db == nil {
		return models.AdminAccess{}, ErrAdminForbidden
	}
	return r.access(ctx, r.db, actorID)
}

func (r *AdminRepository) require(ctx context.Context, q adminQuerier, actorID int, p models.AdminPermission) error {
	a, err := r.access(ctx, q, actorID)
	if err != nil {
		return err
	}
	if !a.Can(p) {
		return ErrAdminForbidden
	}
	return nil
}

const AdminPageSize = 25

func (r *AdminRepository) Summary(ctx context.Context, actorID int) (models.AdminSummary, error) {
	var s models.AdminSummary
	if err := r.require(ctx, r.db, actorID, models.AdminReadUsers); err != nil {
		return s, err
	}
	err := r.db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM farmers WHERE deleted_at IS NULL),
 (SELECT count(*) FROM farmers WHERE deleted_at IS NULL AND suspended_at IS NOT NULL),
 (SELECT count(*) FROM crops c JOIN farmers f ON f.id=c.farmer_id WHERE c.listed_for_sale AND NOT c.moderation_blocked AND c.quantity>0 AND f.suspended_at IS NULL AND f.deleted_at IS NULL),
 (SELECT count(*) FROM crops c JOIN farmers f ON f.id=c.farmer_id WHERE c.moderation_blocked AND f.deleted_at IS NULL)`).Scan(&s.Users, &s.Suspended, &s.Listings, &s.Blocked)
	return s, err
}

func validAdminQuery(query string, before int64) bool {
	return len(query) <= 100 && before >= 0
}

// Users deliberately omits passwords, bank data, email and full phone numbers.
// Exact phone search supports account lookup without exposing a phone directory.
func (r *AdminRepository) Users(ctx context.Context, actorID int, query string, before int64) ([]models.AdminUser, error) {
	if err := r.require(ctx, r.db, actorID, models.AdminReadUsers); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if !validAdminQuery(query, before) {
		return nil, ErrAdminInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, full_name, '••••' || RIGHT(phone,4), role,
 admin_role, is_admin, suspended_at IS NOT NULL, admin_version, created_at
 FROM farmers WHERE deleted_at IS NULL AND ($2::bigint = 0 OR id < $2)
 AND ($1 = '' OR full_name ILIKE '%' || $1 || '%' OR phone = $1 OR id::text = $1)
 ORDER BY id DESC LIMIT 25`, query, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []models.AdminUser{}
	for rows.Next() {
		var u models.AdminUser
		if err := rows.Scan(&u.ID, &u.FullName, &u.PhoneHint, &u.Role, &u.AdminRole, &u.WalletAdmin, &u.Suspended, &u.Version, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Configured = r.configured[u.ID]
		if u.Configured {
			u.AdminRole, u.WalletAdmin = "administrator", true
		}
		items = append(items, u)
	}
	return items, rows.Err()
}

func (r *AdminRepository) Listings(ctx context.Context, actorID int, query string, before int64) ([]models.AdminListing, error) {
	if err := r.require(ctx, r.db, actorID, models.AdminModerateListings); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if !validAdminQuery(query, before) {
		return nil, ErrAdminInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT c.id, c.name, c.farmer_id, f.full_name, c.location,
 c.quantity, c.unit, c.price_per_unit::text, c.listed_for_sale, c.moderation_blocked,
 f.suspended_at IS NOT NULL, c.moderation_version
 FROM crops c JOIN farmers f ON f.id = c.farmer_id
 WHERE f.deleted_at IS NULL AND (c.listed_for_sale OR c.moderation_blocked)
 AND ($2::bigint = 0 OR c.id < $2)
 AND ($1 = '' OR c.name ILIKE '%' || $1 || '%' OR f.full_name ILIKE '%' || $1 || '%' OR c.id::text = $1)
 ORDER BY c.id DESC LIMIT 25`, query, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []models.AdminListing{}
	for rows.Next() {
		var v models.AdminListing
		if err := rows.Scan(&v.ID, &v.Name, &v.SellerID, &v.SellerName, &v.Location, &v.Quantity, &v.Unit, &v.Price, &v.Listed, &v.Blocked, &v.SellerSuspended, &v.Version); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *AdminRepository) Audit(ctx context.Context, actorID int, before int64) ([]models.AdminAuditEvent, error) {
	if err := r.require(ctx, r.db, actorID, models.AdminReadAudit); err != nil {
		return nil, err
	}
	if before < 0 {
		return nil, ErrAdminInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, COALESCE(actor_id,0), action, resource, target_id,
 outcome, reason, before_state, after_state, request_id, created_at
 FROM admin_audit_events WHERE ($1::bigint = 0 OR id < $1) ORDER BY id DESC LIMIT 25`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []models.AdminAuditEvent{}
	for rows.Next() {
		var e models.AdminAuditEvent
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.Resource, &e.TargetID, &e.Outcome, &e.Reason, &e.Before, &e.After, &e.RequestID, &e.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

type auditExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func appendAdminAudit(ctx context.Context, q auditExecer, e models.AdminAuditEvent) error {
	_, err := q.ExecContext(ctx, `INSERT INTO admin_audit_events
 (actor_id, action, resource, target_id, outcome, reason, before_state, after_state, request_id)
 VALUES (NULLIF($1,0),$2,$3,$4,$5,$6,$7,$8,$9)`, e.ActorID, e.Action, e.Resource, e.TargetID, e.Outcome, e.Reason, e.Before, e.After, e.RequestID)
	return err
}

// RecordWalletAttempt must succeed before an HTTP financial operation starts.
// It records identifiers only, never request bodies or provider errors.
func (r *AdminRepository) RecordWalletAttempt(ctx context.Context, e models.AdminAuditEvent) error {
	if e.Outcome != "attempted" || e.RequestID == "" || e.TargetID <= 0 {
		return ErrAdminInvalid
	}
	if err := r.require(ctx, r.db, e.ActorID, models.AdminManageWallet); err != nil {
		return err
	}
	return appendAdminAudit(ctx, r.db, e)
}

// RecordWalletResult completes an already-authorized attempt even if the actor's
// permissions have since been revoked. This is an internal interface only.
func (r *AdminRepository) RecordWalletResult(ctx context.Context, e models.AdminAuditEvent) error {
	if e.Outcome != "returned" && e.Outcome != "failed" {
		return ErrAdminInvalid
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO admin_audit_events
 (actor_id, action, resource, target_id, outcome, request_id)
 SELECT actor_id, action, resource, target_id, $1, request_id FROM admin_audit_events
 WHERE request_id=$2 AND outcome='attempted' AND actor_id=$3 AND action=$4 AND resource=$5 AND target_id=$6`,
		e.Outcome, e.RequestID, e.ActorID, e.Action, e.Resource, e.TargetID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrAdminInvalid
	}
	return nil
}

func validAdminChange(c models.AdminChange) bool {
	if c.TargetID <= 0 || c.Version < 0 {
		return false
	}
	switch c.Reason {
	case "policy_violation", "suspected_fraud", "incorrect_listing", "review_cleared", "staff_assignment":
		return true
	default:
		return false
	}
}

// serializeGovernance avoids concurrent reciprocal revocations and keeps
// authorization stable for the duration of each administrative transaction.
func serializeGovernance(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext(current_schema()),6006)`)
	return err
}

func adminUserState(role string, wallet, suspended bool) string {
	account, approvals := "Active", "No"
	if suspended {
		account = "Suspended"
	}
	if wallet {
		approvals = "Yes"
	}
	return fmt.Sprintf("Role: %s; Wallet approvals: %s; Account: %s", (models.AdminAccess{Role: role}).Label(), approvals, account)
}

func (r *AdminRepository) ChangeUser(ctx context.Context, actorID int, c models.AdminChange) error {
	if !validAdminChange(c) {
		return ErrAdminInvalid
	}
	p := models.AdminManageUsers
	switch c.Action {
	case "suspend", "reactivate":
	case "role_none", "role_moderator", "role_administrator", "wallet_grant", "wallet_revoke":
		p = models.AdminManageRoles
	default:
		return ErrAdminInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := serializeGovernance(ctx, tx); err != nil {
		return err
	}
	if err := r.require(ctx, tx, actorID, p); err != nil {
		return err
	}
	if actorID == c.TargetID || r.configured[c.TargetID] {
		return ErrAdminProtected
	}
	var role string
	var wallet, suspended bool
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT admin_role, is_admin, suspended_at IS NOT NULL, admin_version
 FROM farmers WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, c.TargetID).Scan(&role, &wallet, &suspended, &version)
	if err != nil {
		return err
	}
	if c.Version != version {
		return ErrAdminConflict
	}
	previous := adminUserState(role, wallet, suspended)
	switch c.Action {
	case "suspend":
		if role == "administrator" || wallet {
			return ErrAdminProtected
		}
		if suspended {
			return ErrAdminConflict
		}
		suspended = true
	case "reactivate":
		if !suspended {
			return ErrAdminConflict
		}
		suspended = false
	case "wallet_grant", "wallet_revoke":
		if suspended || wallet == (c.Action == "wallet_grant") {
			return ErrAdminConflict
		}
		wallet = c.Action == "wallet_grant"
	default:
		next := strings.TrimPrefix(c.Action, "role_")
		if next == "none" {
			next = ""
		}
		if suspended || role == next {
			return ErrAdminConflict
		}
		role = next
	}
	_, err = tx.ExecContext(ctx, `UPDATE farmers SET admin_role=$1,is_admin=$2,
 suspended_at=CASE WHEN $3 THEN COALESCE(suspended_at,clock_timestamp()) ELSE NULL END,
 sessions_revoked_at=CASE WHEN $3 THEN clock_timestamp() ELSE sessions_revoked_at END,
 admin_version=admin_version+1,updated_at=CURRENT_TIMESTAMP WHERE id=$4`, role, wallet, suspended, c.TargetID)
	if err != nil {
		return err
	}
	// Persistent sessions stay revoked after reactivation. Legacy session invalidation
	// is completed by the HTTP handler; both authenticators check suspended state.
	if suspended {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=CURRENT_TIMESTAMP WHERE farmer_id=$1 AND revoked_at IS NULL`, c.TargetID); err != nil {
			return err
		}
	}
	if err := appendAdminAudit(ctx, tx, models.AdminAuditEvent{ActorID: actorID, Action: "user." + c.Action, Resource: "farmers", TargetID: int64(c.TargetID), Outcome: "committed", Reason: c.Reason, Before: previous, After: adminUserState(role, wallet, suspended)}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminRepository) ChangeListing(ctx context.Context, actorID int, c models.AdminChange) error {
	if !validAdminChange(c) || (c.Action != "block" && c.Action != "restore") {
		return ErrAdminInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := serializeGovernance(ctx, tx); err != nil {
		return err
	}
	if err := r.require(ctx, tx, actorID, models.AdminModerateListings); err != nil {
		return err
	}
	var blocked bool
	var version int64
	var sellerID int
	err = tx.QueryRowContext(ctx, `SELECT moderation_blocked,moderation_version,farmer_id FROM crops WHERE id=$1 FOR UPDATE`, c.TargetID).Scan(&blocked, &version, &sellerID)
	if err != nil {
		return err
	}
	if sellerID == actorID {
		return ErrAdminProtected
	}
	if c.Version != version || blocked == (c.Action == "block") {
		return ErrAdminConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE crops SET moderation_blocked=$1,moderation_version=moderation_version+1,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, c.Action == "block", c.TargetID)
	if err != nil {
		return err
	}
	if err := appendAdminAudit(ctx, tx, models.AdminAuditEvent{ActorID: actorID, Action: "listing." + c.Action, Resource: "crops", TargetID: int64(c.TargetID), Outcome: "committed", Reason: c.Reason, Before: fmt.Sprint(blocked), After: fmt.Sprint(!blocked)}); err != nil {
		return err
	}
	return tx.Commit()
}
