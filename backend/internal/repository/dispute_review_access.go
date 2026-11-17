package repository

import (
	"context"
	"database/sql"
	"time"

	"backend/internal/models"
)

// DisputeReviewAccess adapts Member 5's existing IsAdmin contract to the
// governance permission. It must never be used as a wallet authorizer.
type DisputeReviewAccess struct{ governance *AdminRepository }

func NewDisputeReviewAccess(governance *AdminRepository) *DisputeReviewAccess {
	return &DisputeReviewAccess{governance: governance}
}

func (a *DisputeReviewAccess) IsAdmin(actorID int) bool {
	if a == nil || a.governance == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	access, err := a.governance.Access(ctx, actorID)
	return err == nil && access.Can(models.AdminReviewDisputes)
}

// AuthorizeTx must run before a dispute/refund transaction locks its records.
// Sharing the governance lock makes authorization stable until commit, including
// when another administrator concurrently removes the reviewer's staff role.
func (a *DisputeReviewAccess) AuthorizeTx(ctx context.Context, tx *sql.Tx, actorID int) error {
	if a == nil || a.governance == nil || tx == nil {
		return ErrAdminForbidden
	}
	if err := serializeGovernance(ctx, tx); err != nil {
		return err
	}
	return a.governance.require(ctx, tx, actorID, models.AdminReviewDisputes)
}
