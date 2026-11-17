package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/middleware"
	"backend/render"
)

type adminStore interface {
	Access(context.Context, int) (models.AdminAccess, error)
	Summary(context.Context, int) (models.AdminSummary, error)
	Users(context.Context, int, string, int64) ([]models.AdminUser, error)
	Listings(context.Context, int, string, int64) ([]models.AdminListing, error)
	Audit(context.Context, int, int64) ([]models.AdminAuditEvent, error)
	ChangeUser(context.Context, int, models.AdminChange) error
	ChangeListing(context.Context, int, models.AdminChange) error
	RecordWalletAttempt(context.Context, models.AdminAuditEvent) error
	RecordWalletResult(context.Context, models.AdminAuditEvent) error
}

type AdminHandler struct{ store adminStore }

// Shared only with the existing wallet page; permission is resolved per request.
type adminAccessContextKey struct{}

func NewAdminHandler(store adminStore) *AdminHandler { return &AdminHandler{store: store} }

type AdminPageData struct {
	Section           string
	Name              string
	Initials          string
	Access            models.AdminAccess
	CanManageUsers    bool
	CanManageRoles    bool
	CanReadAudit      bool
	CanReviewDisputes bool
	CanDecide         bool
	SettlementPending bool
	Disputes          []models.OrderDispute
	Dispute           *models.OrderDispute
	Refund            *models.Refund
	Evidence          []models.OrderDisputeEvidence
	CaseEvents        []models.OrderDisputeEvent
	Order             *models.Order
	Exceptions        []repository.PaymentException
	StatusFilter      string
	ActorID           int
	Summary           models.AdminSummary
	Users             []models.AdminUser
	Listings          []models.AdminListing
	Events            []models.AdminAuditEvent
	Query             string
	Next              string
	First             string
	Success           bool
}

func adminJSON(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json")
}

func adminError(w http.ResponseWriter, r *http.Request, err error) {
	code, message := http.StatusServiceUnavailable, "Unable to complete this request. Refresh and check the current status before trying again."
	switch {
	case errors.Is(err, repository.ErrAdminForbidden):
		code, message = http.StatusForbidden, "You do not have permission to perform this action."
	case errors.Is(err, repository.ErrAdminProtected):
		code, message = http.StatusForbidden, err.Error()
	case errors.Is(err, repository.ErrAdminInvalid):
		code, message = http.StatusBadRequest, "Check the action, record version and reason, then try again."
	case errors.Is(err, repository.ErrAdminConflict):
		code, message = http.StatusConflict, err.Error()
	case errors.Is(err, sql.ErrNoRows):
		code, message = http.StatusNotFound, "This record is no longer available."
	default:
		log.Printf("admin operation failed: %v", err)
	}
	if adminJSON(r) {
		writeJSON(w, code, map[string]string{"error": message})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := render.RenderTemplates(w, "admin-error.html", struct{ Message string }{message}); err != nil {
		log.Printf("render admin error: %v", err)
	}
}

func (h *AdminHandler) authorize(w http.ResponseWriter, r *http.Request, p models.AdminPermission) (int, models.AdminAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		adminError(w, r, repository.ErrAdminForbidden)
		return 0, models.AdminAccess{}, false
	}
	if h.store == nil {
		adminError(w, r, repository.ErrAdminForbidden)
		return 0, models.AdminAccess{}, false
	}
	a, err := h.store.Access(r.Context(), farmer.ID)
	if err == nil && !a.Can(p) {
		err = repository.ErrAdminForbidden
	}
	if err != nil {
		adminError(w, r, err)
		return 0, a, false
	}
	return farmer.ID, a, true
}

func (h *AdminHandler) Page(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", 405)
			return
		}
		permission := models.AdminReadUsers
		if section == "overview" {
			permission = models.AdminEnter
		}
		if section == "listings" {
			permission = models.AdminModerateListings
		}
		if section == "audit" {
			permission = models.AdminReadAudit
		}
		id, access, ok := h.authorize(w, r, permission)
		if !ok {
			return
		}
		if section == "overview" && !access.Can(models.AdminReadUsers) {
			if adminJSON(r) {
				adminError(w, r, repository.ErrAdminForbidden)
			} else {
				http.Redirect(w, r, "/admin/wallet-withdrawals", http.StatusSeeOther)
			}
			return
		}
		var before int64
		var err error
		if raw := r.URL.Query().Get("before"); raw != "" {
			before, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || before < 0 {
				adminError(w, r, repository.ErrAdminInvalid)
				return
			}
		}
		farmer, _ := middleware.FarmerFromContext(r)
		d := AdminPageData{Section: section, Name: farmer.FullName, Initials: models.AdminInitials(farmer.FullName), ActorID: id, Access: access, CanManageUsers: access.Can(models.AdminManageUsers), CanManageRoles: access.Can(models.AdminManageRoles), CanReadAudit: access.Can(models.AdminReadAudit), CanReviewDisputes: access.Can(models.AdminReviewDisputes), Query: strings.TrimSpace(r.URL.Query().Get("q")), Success: r.URL.Query().Get("saved") == "1"}
		if len(d.Query) > 100 {
			adminError(w, r, repository.ErrAdminInvalid)
			return
		}
		if before > 0 {
			d.First = "?" + url.Values{"q": {d.Query}}.Encode()
		}
		var last int64
		switch section {
		case "overview":
			d.Summary, err = h.store.Summary(r.Context(), id)
			if err == nil && d.CanReadAudit {
				d.Events, err = h.store.Audit(r.Context(), id, 0)
				if len(d.Events) > 5 {
					d.Events = d.Events[:5]
				}
			}
		case "users":
			d.Users, err = h.store.Users(r.Context(), id, d.Query, before)
			if len(d.Users) == repository.AdminPageSize {
				last = int64(d.Users[len(d.Users)-1].ID)
			}
		case "listings":
			d.Listings, err = h.store.Listings(r.Context(), id, d.Query, before)
			if len(d.Listings) == repository.AdminPageSize {
				last = int64(d.Listings[len(d.Listings)-1].ID)
			}
		case "audit":
			d.Events, err = h.store.Audit(r.Context(), id, before)
			if len(d.Events) == repository.AdminPageSize {
				last = d.Events[len(d.Events)-1].ID
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			adminError(w, r, err)
			return
		}
		if last > 0 {
			d.Next = "?" + url.Values{"q": {d.Query}, "before": {strconv.FormatInt(last, 10)}}.Encode()
		}
		if adminJSON(r) {
			var items any = d.Summary
			switch section {
			case "users":
				items = d.Users
			case "listings":
				items = d.Listings
			case "audit":
				items = d.Events
			}
			writeJSON(w, 200, map[string]any{"data": items, "next": d.Next})
			return
		}
		var body bytes.Buffer
		if err := render.TMPL.ExecuteTemplate(&body, "admin.html", d); err != nil {
			adminError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = body.WriteTo(w)
	}
}

func decodeAdminChange(w http.ResponseWriter, r *http.Request) (models.AdminChange, error) {
	var c models.AdminChange
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var payload struct {
			TargetID int    `json:"target_id"`
			Version  *int64 `json:"version"`
			Action   string `json:"action"`
			Reason   string `json:"reason"`
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if err := d.Decode(&payload); err != nil {
			return c, repository.ErrAdminInvalid
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return c, repository.ErrAdminInvalid
		}
		if payload.Version == nil {
			return c, repository.ErrAdminInvalid
		}
		c = models.AdminChange{TargetID: payload.TargetID, Version: *payload.Version, Action: payload.Action, Reason: payload.Reason}
	} else {
		if err := r.ParseForm(); err != nil {
			return c, repository.ErrAdminInvalid
		}
		var err error
		c.TargetID, err = strconv.Atoi(r.PostForm.Get("target_id"))
		if err != nil {
			return c, repository.ErrAdminInvalid
		}
		c.Version, err = strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
		if err != nil {
			return c, repository.ErrAdminInvalid
		}
		c.Action, c.Reason = r.PostForm.Get("action"), r.PostForm.Get("reason")
	}
	if c.TargetID <= 0 || c.Version < 0 || c.Action == "" || c.Reason == "" {
		return c, repository.ErrAdminInvalid
	}
	return c, nil
}

func (h *AdminHandler) Change(resource string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", 405)
			return
		}
		permission := models.AdminManageUsers
		if resource == "listings" {
			permission = models.AdminModerateListings
		}
		id, _, ok := h.authorize(w, r, permission)
		if !ok {
			return
		}
		c, err := decodeAdminChange(w, r)
		if err == nil {
			if resource == "listings" {
				err = h.store.ChangeListing(r.Context(), id, c)
			} else {
				err = h.store.ChangeUser(r.Context(), id, c)
			}
		}
		if err != nil {
			adminError(w, r, err)
			return
		}
		if resource == "users" && c.Action == "suspend" {
			middleware.ForgetFarmerPresence(c.TargetID)
		}
		if adminJSON(r) {
			writeJSON(w, 200, map[string]string{"status": "saved"})
			return
		}
		http.Redirect(w, r, "/admin/"+resource+"?saved=1", http.StatusSeeOther)
	}
}

// WalletAccess guards existing wallet handlers with fresh permissions. It writes
// a durable intent before any provider call. Database triggers separately record
// committed status transitions, including outcomes arriving via webhooks.
func (h *AdminHandler) WalletAccess(next http.HandlerFunc, action, resource, idField string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, access, ok := h.authorize(w, r, models.AdminManageWallet)
		if !ok {
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), adminAccessContextKey{}, access))
		if r.Method == http.MethodGet && action == "" {
			next(w, r)
			return
		}
		if r.Method != http.MethodPost || action == "" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			adminError(w, r, repository.ErrAdminInvalid)
			return
		}
		target, err := strconv.ParseInt(r.PostForm.Get(idField), 10, 64)
		if err != nil || target <= 0 {
			adminError(w, r, repository.ErrAdminInvalid)
			return
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			adminError(w, r, err)
			return
		}
		e := models.AdminAuditEvent{ActorID: id, Action: action, Resource: resource, TargetID: target, Outcome: "attempted", RequestID: hex.EncodeToString(token[:])}
		if err := h.store.RecordWalletAttempt(r.Context(), e); err != nil {
			adminError(w, r, err)
			return
		}
		recorded := &adminResponse{ResponseWriter: w, status: 200}
		defer func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			e.Outcome = "returned"
			location, _ := url.Parse(recorded.Header().Get("Location"))
			if recorded.status >= 400 || (location != nil && location.Query().Get("error") != "") {
				e.Outcome = "failed"
			}
			if p := recover(); p != nil {
				e.Outcome = "failed"
				_ = h.store.RecordWalletResult(ctx, e)
				panic(p)
			}
			if err := h.store.RecordWalletResult(ctx, e); err != nil {
				log.Printf("admin audit completion missing for request %s: %v", e.RequestID, err)
			}
		}()
		next(recorded, r)
	}
}

// DisputeAccess is the HTTP boundary for Member 5's review actions. Refund
// eligibility and settlement stay in their existing services/repositories.
// Register this inside the same authentication/CSRF wrapper as other admin URLs.
func (h *AdminHandler) DisputeAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := h.authorize(w, r, models.AdminReviewDisputes)
		if ok {
			next(w, r)
		}
	}
}

type adminResponse struct {
	http.ResponseWriter
	status  int
	written bool
}

func (w *adminResponse) WriteHeader(status int) {
	if w.written {
		return
	}
	if status >= 200 {
		w.status = status
		w.written = true
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *adminResponse) Write(p []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *adminResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
