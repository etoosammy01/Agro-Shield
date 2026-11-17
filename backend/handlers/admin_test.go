package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/middleware"
)

type fakeAdminStore struct {
	access                                      models.AdminAccess
	accessErr, attemptErr, resultErr, changeErr error
	changes                                     int
	change                                      models.AdminChange
	attempts, results                           []models.AdminAuditEvent
	users                                       []models.AdminUser
	listings                                    []models.AdminListing
}

func (s *fakeAdminStore) Access(context.Context, int) (models.AdminAccess, error) {
	return s.access, s.accessErr
}
func (s *fakeAdminStore) Summary(context.Context, int) (models.AdminSummary, error) {
	return models.AdminSummary{Users: 2, Listings: 1}, nil
}
func (s *fakeAdminStore) Users(context.Context, int, string, int64) ([]models.AdminUser, error) {
	return s.users, nil
}
func (s *fakeAdminStore) Listings(context.Context, int, string, int64) ([]models.AdminListing, error) {
	return s.listings, nil
}
func (s *fakeAdminStore) Audit(context.Context, int, int64) ([]models.AdminAuditEvent, error) {
	return []models.AdminAuditEvent{{ID: 1, ActorID: 81, Action: "user.suspend", Resource: "farmers", TargetID: 12, Outcome: "committed", Before: "Active", After: "Suspended", Reason: "policy_violation", CreatedAt: time.Now()}}, nil
}
func (s *fakeAdminStore) ChangeUser(_ context.Context, _ int, c models.AdminChange) error {
	s.changes++
	s.change = c
	return s.changeErr
}
func (s *fakeAdminStore) ChangeListing(ctx context.Context, id int, c models.AdminChange) error {
	return s.ChangeUser(ctx, id, c)
}
func (s *fakeAdminStore) RecordWalletAttempt(_ context.Context, e models.AdminAuditEvent) error {
	s.attempts = append(s.attempts, e)
	return s.attemptErr
}
func (s *fakeAdminStore) RecordWalletResult(_ context.Context, e models.AdminAuditEvent) error {
	s.results = append(s.results, e)
	return s.resultErr
}

type adminFarmerReader struct{}

func (adminFarmerReader) GetByID(id int) (*models.Farmer, error) {
	done := sql.NullTime{Time: time.Now(), Valid: true}
	return &models.Farmer{ID: id, FullName: "Ada Example", Role: "farmer", PhotoURL: "/fixture.jpg", SelfieCapturedAt: done, PhoneVerifiedAt: done, HasTransactionPIN: true}, nil
}

func adminRequest(t *testing.T, h http.HandlerFunc, method, path, body, kind string) *httptest.ResponseRecorder {
	t.Helper()
	token := middleware.CreateSession(81)
	t.Cleanup(func() { middleware.DeleteSession(token) })
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", kind)
	r.Header.Set("Origin", "http://example.com")
	r.AddCookie(&http.Cookie{Name: "agroshield_session", Value: token})
	w := httptest.NewRecorder()
	middleware.RequireAuth(adminFarmerReader{}, h)(w, r)
	return w
}

func TestAdminHandlersEnforcePermissionsAndFailClosed(t *testing.T) {
	for _, access := range []models.AdminAccess{{}, {Role: "moderator"}, {Wallet: true}} {
		s := &fakeAdminStore{access: access}
		h := NewAdminHandler(s)
		for _, resource := range []string{"users", "listings"} {
			if resource == "listings" && access.Role == "moderator" {
				continue
			}
			w := adminRequest(t, h.Change(resource), "POST", "/api/admin/"+resource+"/change", `{"target_id":12,"version":0,"action":"suspend","reason":"policy_violation"}`, "application/json")
			if w.Code != 403 || s.changes != 0 {
				t.Fatalf("access=%+v resource=%s status=%d changes=%d", access, resource, w.Code, s.changes)
			}
		}
	}
	s := &fakeAdminStore{access: models.AdminAccess{Role: "administrator"}, accessErr: errors.New("private database details")}
	w := adminRequest(t, NewAdminHandler(s).Page("users"), "GET", "/api/admin/users", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "private database details") {
		t.Fatalf("unsafe error response: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminChangeRejectsMalformedRequestsAndStaleDecisions(t *testing.T) {
	for _, body := range []string{
		`{`, `{"target_id":12,"action":"suspend","reason":"policy_violation"}`,
		`{"target_id":12,"version":0,"action":"suspend","reason":"policy_violation","actor_id":1}`,
		`{"target_id":12,"version":0,"action":"suspend","reason":"policy_violation"} {}`,
		`{"target_id":12,"version":-1,"action":"suspend","reason":"policy_violation"}`,
		`{"target_id":0,"version":0,"action":"suspend","reason":"policy_violation"}`,
		`{"target_id":12,"version":0,"action":"suspend","reason":"` + strings.Repeat("x", 5000) + `"}`,
	} {
		s := &fakeAdminStore{access: models.AdminAccess{Role: "administrator"}}
		w := adminRequest(t, NewAdminHandler(s).Change("users"), "POST", "/api/admin/users/change", body, "application/json")
		if w.Code != 400 || s.changes != 0 {
			t.Fatalf("invalid request accepted: status=%d changes=%d", w.Code, s.changes)
		}
	}
	s := &fakeAdminStore{access: models.AdminAccess{Role: "administrator"}, changeErr: repository.ErrAdminConflict}
	w := adminRequest(t, NewAdminHandler(s).Change("users"), "POST", "/api/admin/users/change", `{"target_id":12,"version":4,"action":"suspend","reason":"policy_violation"}`, "application/json")
	if w.Code != 409 || s.change.Version != 4 {
		t.Fatalf("stale change: status=%d change=%+v", w.Code, s.change)
	}
}

func TestAdminWalletWritesIntentBeforeOperationAndRecordsOutcome(t *testing.T) {
	for _, failIntent := range []bool{false, true} {
		s := &fakeAdminStore{access: models.AdminAccess{Wallet: true}}
		if failIntent {
			s.attemptErr = errors.New("audit unavailable")
		}
		called := false
		next := func(w http.ResponseWriter, r *http.Request) {
			called = true
			if len(s.attempts) != 1 {
				t.Fatal("operation preceded audit intent")
			}
			http.Redirect(w, r, "/admin/wallet-withdrawals?other=1&error=declined", 303)
		}
		w := adminRequest(t, NewAdminHandler(s).WalletAccess(next, "wallet.withdrawal.approve", "wallet_withdrawals", "withdrawal_id"), "POST", "/admin/wallet-withdrawals/approve", "withdrawal_id=19&password=never-log-this", "application/x-www-form-urlencoded")
		if failIntent {
			if called || w.Code != 503 || len(s.results) != 0 {
				t.Fatal("wallet operation ran without an audit record")
			}
			continue
		}
		if !called || len(s.results) != 1 || s.results[0].Outcome != "failed" {
			t.Fatalf("wallet result not recorded: %+v", s.results)
		}
		e := s.results[0]
		if e.ActorID != 81 || e.TargetID != 19 || len(e.RequestID) != 32 || e.RequestID != s.attempts[0].RequestID || e.Reason != "" {
			t.Fatalf("incorrect audit attribution: %+v", e)
		}
	}
}

func TestAdminConsoleRendersRolesAndEscapesMemberContent(t *testing.T) {
	for _, role := range []string{"administrator", "moderator"} {
		for _, wallet := range []bool{false, true} {
			s := &fakeAdminStore{access: models.AdminAccess{Role: role, Wallet: wallet}, users: []models.AdminUser{{ID: 12, FullName: `<img src=x onerror=alert(1)>`, Role: "farmer", PhoneHint: "••••1234", CreatedAt: time.Now()}}, listings: []models.AdminListing{{ID: 6, Name: "Maize", SellerID: 12, Price: "1000.00", Quantity: 2, Listed: true}}}
			for _, section := range []string{"overview", "users", "listings", "audit"} {
				w := adminRequest(t, NewAdminHandler(s).Page(section), "GET", "/admin/"+section, "", "")
				if section == "audit" && role == "moderator" {
					if w.Code != 403 {
						t.Fatal("moderator accessed audit")
					}
					continue
				}
				if w.Code != 200 {
					t.Fatalf("render %s/%s: %d %s", role, section, w.Code, w.Body.String())
				}
				body := w.Body.String()
				if strings.Contains(body, `<img src=x onerror=`) {
					t.Fatal("unescaped member HTML")
				}
				if section == "users" {
					if !strings.Contains(body, "Account ID #12") {
						t.Fatal("member account ID is not labeled")
					}
					if strings.Contains(body, `href="/admin/wallet-withdrawals?user_id=12#wallet-repair"`) != wallet {
						t.Fatal("wallet repair selection did not respect wallet access")
					}
				}
				if role == "moderator" && (strings.Contains(body, `href="/admin/audit"`) || strings.Contains(body, `name="action" value="suspend"`) || strings.Contains(body, `value="role_administrator"`)) {
					t.Fatal("moderator UI exposed administrator controls")
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("admin response can be cached")
				}
			}
		}
	}
}

func TestAdminDisputeBoundaryUsesGovernancePermission(t *testing.T) {
	for _, access := range []models.AdminAccess{{}, {Wallet: true}, {Role: "moderator"}, {Role: "administrator"}} {
		store := &fakeAdminStore{access: access}
		called := false
		handler := NewAdminHandler(store).DisputeAccess(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })
		w := adminRequest(t, handler, "POST", "/admin/disputes/review", "", "")
		if called != access.Can(models.AdminReviewDisputes) || (called && w.Code != 204) || (!called && w.Code != 403) {
			t.Fatal("dispute endpoint allowed the wrong role")
		}
	}
}
