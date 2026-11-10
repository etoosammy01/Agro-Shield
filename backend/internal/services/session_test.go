package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

// fakeSessionRepository imitates the database repository for unit tests.
type fakeSessionRepository struct {
	tokenHash string
	farmerID  int
	expiresAt time.Time

	revokedHash     string
	revokedFarmerID int

	deletedCount int64
	createErr    error
	findErr      error
	revokeErr    error
	revokeAllErr error
	cleanupErr   error

	// cleanupCalls receives one value each time DeleteExpired is invoked.
	// It stays nil in tests that do not exercise the background worker, so
	// those tests never block on it.
	cleanupCalls chan struct{}
}

func (f *fakeSessionRepository) Create(
	_ context.Context,
	tokenHash string,
	farmerID int,
	expiresAt time.Time,
) error {
	if f.createErr != nil {
		return f.createErr
	}

	f.tokenHash = tokenHash
	f.farmerID = farmerID
	f.expiresAt = expiresAt
	return nil
}

func (f *fakeSessionRepository) FindValid(
	_ context.Context,
	tokenHash string,
) (int, error) {
	if f.findErr != nil {
		return 0, f.findErr
	}

	if tokenHash != f.tokenHash {
		return 0, errors.New("session not found")
	}

	return f.farmerID, nil
}

func (f *fakeSessionRepository) Revoke(
	_ context.Context,
	tokenHash string,
) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}

	f.revokedHash = tokenHash
	return nil
}

func (f *fakeSessionRepository) RevokeAllForFarmer(
	_ context.Context,
	farmerID int,
) error {
	if f.revokeAllErr != nil {
		return f.revokeAllErr
	}

	f.revokedFarmerID = farmerID
	return nil
}

func (f *fakeSessionRepository) DeleteExpired(
	_ context.Context,
) (int64, error) {
	if f.cleanupCalls != nil {
		// Non-blocking send: a slow test must never stall the worker, and a
		// full buffer only means the test already stopped consuming signals.
		select {
		case f.cleanupCalls <- struct{}{}:
		default:
		}
	}

	if f.cleanupErr != nil {
		return 0, f.cleanupErr
	}

	return f.deletedCount, nil
}

func TestSessionServiceCreate(t *testing.T) {
	repo := &fakeSessionRepository{}
	service := NewSessionService(repo)

	before := time.Now().UTC()

	token, expiresAt, err := service.Create(context.Background(), 42)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	after := time.Now().UTC()

	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}

	if _, err := hex.DecodeString(token); err != nil {
		t.Errorf("token is not valid hexadecimal: %v", err)
	}

	if repo.tokenHash == token {
		t.Error("database should store the hash, not the original token")
	}

	expectedHash := sha256.Sum256([]byte(token))
	if repo.tokenHash != hex.EncodeToString(expectedHash[:]) {
		t.Error("stored token hash does not match SHA-256")
	}

	if repo.farmerID != 42 {
		t.Errorf("farmer ID = %d, want 42", repo.farmerID)
	}

	if expiresAt.Before(before.Add(24*time.Hour)) ||
		expiresAt.After(after.Add(24*time.Hour)) {
		t.Error("session expiry should be approximately 24 hours from creation")
	}

	if !repo.expiresAt.Equal(expiresAt) {
		t.Error("repository expiry does not match returned expiry")
	}
}

func TestSessionServiceCreatePropagatesRepositoryError(t *testing.T) {
	expectedErr := errors.New("database unavailable")
	repo := &fakeSessionRepository{createErr: expectedErr}
	service := NewSessionService(repo)

	_, _, err := service.Create(context.Background(), 42)

	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}

func TestSessionServiceValidate(t *testing.T) {
	token := "test-session-token"
	expectedHash := sha256.Sum256([]byte(token))

	repo := &fakeSessionRepository{
		tokenHash: hex.EncodeToString(expectedHash[:]),
		farmerID:  42,
	}

	service := NewSessionService(repo)

	farmerID, err := service.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("validate session: %v", err)
	}

	if farmerID != 42 {
		t.Errorf("farmer ID = %d, want 42", farmerID)
	}
}

func TestSessionServiceValidatePropagatesError(t *testing.T) {
	expectedErr := errors.New("session not found")
	repo := &fakeSessionRepository{findErr: expectedErr}
	service := NewSessionService(repo)

	_, err := service.Validate(context.Background(), "unknown-token")

	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}

func TestSessionServiceRevoke(t *testing.T) {
	repo := &fakeSessionRepository{}
	service := NewSessionService(repo)

	token := "test-session-token"

	if err := service.Revoke(context.Background(), token); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	expectedHash := sha256.Sum256([]byte(token))
	if repo.revokedHash != hex.EncodeToString(expectedHash[:]) {
		t.Error("revoke should pass the token hash to the repository")
	}
}

func TestSessionServiceRevokeAllForFarmer(t *testing.T) {
	repo := &fakeSessionRepository{}
	service := NewSessionService(repo)

	if err := service.RevokeAllForFarmer(context.Background(), 42); err != nil {
		t.Fatalf("revoke all sessions: %v", err)
	}

	if repo.revokedFarmerID != 42 {
		t.Errorf("farmer ID = %d, want 42", repo.revokedFarmerID)
	}
}

func TestSessionServiceCleanupExpired(t *testing.T) {
	repo := &fakeSessionRepository{deletedCount: 5}
	service := NewSessionService(repo)

	deleted, err := service.CleanupExpired(context.Background())
	if err != nil {
		t.Fatalf("cleanup expired sessions: %v", err)
	}

	if deleted != 5 {
		t.Errorf("deleted = %d, want 5", deleted)
	}
}

// startCleanupWorker runs RunCleanup in the background and returns a stop
// function that cancels the context and waits for the worker to exit, so no
// goroutine leaks out of a test.
func startCleanupWorker(
	t *testing.T,
	service *SessionService,
	repo *fakeSessionRepository,
	interval time.Duration,
) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.RunCleanup(ctx, interval)
	}()

	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("session cleanup worker did not stop after cancellation")
		}
	}
}

// awaitCleanupPasses waits for the worker to invoke DeleteExpired n times.
func awaitCleanupPasses(t *testing.T, repo *fakeSessionRepository, n int) {
	t.Helper()

	for i := 0; i < n; i++ {
		select {
		case <-repo.cleanupCalls:
		case <-time.After(2 * time.Second):
			t.Fatalf("cleanup pass %d of %d did not run", i+1, n)
		}
	}
}

func TestSessionCleanupWorkerRunsAtStartup(t *testing.T) {
	repo := &fakeSessionRepository{
		deletedCount: 3,
		cleanupCalls: make(chan struct{}, 16),
	}
	service := NewSessionService(repo)

	stop := startCleanupWorker(t, service, repo, time.Hour)

	// RunCleanup performs its first pass before the ticker starts, so the
	// signal must arrive without waiting for any interval.
	awaitCleanupPasses(t, repo, 1)

	stop()
}

func TestSessionCleanupWorkerRunsOnTicks(t *testing.T) {
	repo := &fakeSessionRepository{
		deletedCount: 1,
		cleanupCalls: make(chan struct{}, 64),
	}
	service := NewSessionService(repo)

	stop := startCleanupWorker(t, service, repo, 10*time.Millisecond)
	defer stop()

	// One pass at startup plus at least one more from a tick.
	awaitCleanupPasses(t, repo, 2)
}

func TestSessionCleanupWorkerStopsOnCancel(t *testing.T) {
	repo := &fakeSessionRepository{cleanupCalls: make(chan struct{}, 16)}
	service := NewSessionService(repo)

	stop := startCleanupWorker(t, service, repo, time.Hour)

	// The startup pass confirms the worker is inside its loop before it is
	// asked to stop.
	awaitCleanupPasses(t, repo, 1)

	stop()
}

func TestSessionCleanupWorkerKeepsRunningAfterRepositoryError(t *testing.T) {
	repo := &fakeSessionRepository{
		cleanupErr:   errors.New("database unavailable"),
		cleanupCalls: make(chan struct{}, 64),
	}
	service := NewSessionService(repo)

	stop := startCleanupWorker(t, service, repo, 10*time.Millisecond)
	defer stop()

	// The startup pass fails, and the next tick must still be attempted.
	awaitCleanupPasses(t, repo, 2)
}

func TestSessionCleanupWorkerWithZeroIntervalUsesOneHourDefault(t *testing.T) {
	repo := &fakeSessionRepository{cleanupCalls: make(chan struct{}, 16)}
	service := NewSessionService(repo)

	stop := startCleanupWorker(t, service, repo, 0)
	defer stop()

	// Only the startup pass should have run: the effective interval is one
	// hour, so no tick can fire while this test is executing.
	awaitCleanupPasses(t, repo, 1)

	select {
	case <-repo.cleanupCalls:
		t.Error("unexpected extra cleanup pass; zero interval did not use the one-hour default")
	default:
	}
}

func TestEffectiveCleanupInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{"zero falls back to the default", 0, DefaultSessionCleanupInterval},
		{"negative falls back to the default", -time.Minute, DefaultSessionCleanupInterval},
		{"positive intervals are kept", 30 * time.Minute, 30 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveCleanupInterval(tt.interval); got != tt.want {
				t.Errorf("effectiveCleanupInterval(%v) = %v, want %v", tt.interval, got, tt.want)
			}
		})
	}
}
