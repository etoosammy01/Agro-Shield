package repository

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestSessionCreateAndFindValid verifies that a newly created session
// can be retrieved and that the correct farmer ID is returned.
func TestSessionCreateAndFindValid(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	farmer := createTestUser(t, db)
	repo := NewSessionRepository(db)

	tokenHash := uniqueRef("SESSION")
	expiresAt := time.Now().Add(24 * time.Hour)

	if err := repo.Create(ctx, tokenHash, farmer.ID, expiresAt); err != nil {
		t.Fatalf("create session: %v", err)
	}

	gotFarmerID, err := repo.FindValid(ctx, tokenHash)
	if err != nil {
		t.Fatalf("find valid session: %v", err)
	}

	if gotFarmerID != farmer.ID {
		t.Errorf("farmer ID = %d, want %d", gotFarmerID, farmer.ID)
	}
}

// TestSessionRejectsExpired verifies that an expired session
// cannot be used to authenticate a farmer.
func TestSessionRejectsExpired(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	farmer := createTestUser(t, db)
	repo := NewSessionRepository(db)

	tokenHash := uniqueRef("EXPIRED")
	expiresAt := time.Now().Add(-1 * time.Hour)

	if err := repo.Create(ctx, tokenHash, farmer.ID, expiresAt); err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	_, err := repo.FindValid(ctx, tokenHash)

	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error = %v, want ErrSessionNotFound", err)
	}
}

// TestSessionRejectsRevoked verifies that logging out invalidates
// the session even if its original expiry time has not passed.
func TestSessionRejectsRevoked(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	farmer := createTestUser(t, db)
	repo := NewSessionRepository(db)

	tokenHash := uniqueRef("REVOKED")
	expiresAt := time.Now().Add(24 * time.Hour)

	if err := repo.Create(ctx, tokenHash, farmer.ID, expiresAt); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := repo.Revoke(ctx, tokenHash); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	_, err := repo.FindValid(ctx, tokenHash)

	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error = %v, want ErrSessionNotFound", err)
	}
}

// TestRevokeAllForFarmer verifies that revoking one farmer's sessions
// does not affect another farmer's sessions.
func TestRevokeAllForFarmer(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	farmerOne := createTestUser(t, db)
	farmerTwo := createTestUser(t, db)

	repo := NewSessionRepository(db)
	expiresAt := time.Now().Add(24 * time.Hour)

	firstSession := uniqueRef("FARMER_ONE")
	secondSession := uniqueRef("FARMER_ONE")
	otherFarmerSession := uniqueRef("FARMER_TWO")

	for _, session := range []struct {
		hash     string
		farmerID int
	}{
		{firstSession, farmerOne.ID},
		{secondSession, farmerOne.ID},
		{otherFarmerSession, farmerTwo.ID},
	} {
		if err := repo.Create(ctx, session.hash, session.farmerID, expiresAt); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}

	if err := repo.RevokeAllForFarmer(ctx, farmerOne.ID); err != nil {
		t.Fatalf("revoke all sessions: %v", err)
	}

	for _, tokenHash := range []string{firstSession, secondSession} {
		_, err := repo.FindValid(ctx, tokenHash)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("session %q was not revoked", tokenHash)
		}
	}

	gotFarmerID, err := repo.FindValid(ctx, otherFarmerSession)
	if err != nil {
		t.Fatalf("other farmer's session should remain valid: %v", err)
	}

	if gotFarmerID != farmerTwo.ID {
		t.Errorf("farmer ID = %d, want %d", gotFarmerID, farmerTwo.ID)
	}
}

// TestDeleteExpired verifies that expired and revoked sessions
// are removed while active sessions remain untouched.
func TestDeleteExpired(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	farmer := createTestUser(t, db)
	repo := NewSessionRepository(db)

	expiredSession := uniqueRef("EXPIRED")
	revokedSession := uniqueRef("REVOKED")
	activeSession := uniqueRef("ACTIVE")

	if err := repo.Create(ctx, expiredSession, farmer.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	if err := repo.Create(ctx, revokedSession, farmer.ID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("create session to revoke: %v", err)
	}

	if err := repo.Create(ctx, activeSession, farmer.ID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("create active session: %v", err)
	}

	if err := repo.Revoke(ctx, revokedSession); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	deleted, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("delete expired sessions: %v", err)
	}

	if deleted != 2 {
		t.Errorf("deleted sessions = %d, want 2", deleted)
	}

	// The active session must still be available.
	if _, err := repo.FindValid(ctx, activeSession); err != nil {
		t.Errorf("active session should remain valid: %v", err)
	}

	// The expired session must no longer exist.
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE session_token_hash = $1`,
		expiredSession,
	).Scan(&count); err != nil {
		t.Fatalf("count expired session: %v", err)
	}

	if count != 0 {
		t.Errorf("expired session count = %d, want 0", count)
	}
}
