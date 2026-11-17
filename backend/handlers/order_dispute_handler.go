package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"backend/internal/escrow"
	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type OrderDisputeHandler struct {
	service     *services.OrderDisputeService
	repo        *repository.OrderDisputeRepository
	wallet      *services.WalletService
	orders      *services.OrderService
	governance  adminStore
	privateRoot string
}

func NewOrderDisputeHandler(service *services.OrderDisputeService, wallet *services.WalletService, orders *services.OrderService, governance adminStore, privateRoot string) *OrderDisputeHandler {
	return &OrderDisputeHandler{service: service, repo: service.Workflows(), wallet: wallet, orders: orders, governance: governance, privateRoot: privateRoot}
}

type disputeRequest struct {
	DisputeID       int64  `json:"dispute_id"`
	OrderID         int64  `json:"order_id"`
	DeliveryID      *int64 `json:"delivery_id,omitempty"`
	RefundID        int64  `json:"refund_id"`
	ReasonCode      string `json:"reason_code"`
	Description     string `json:"description"`
	RequestedAmount string `json:"requested_amount"`
	ApprovedAmount  string `json:"approved_amount"`
	ApprovalNote    string `json:"approval_note"`
	Response        string `json:"response"`
	Reason          string `json:"reason"`
	Version         *int64 `json:"version"`
	Reviewed        bool   `json:"reviewed"`
}

func disputeJSON(r *http.Request) bool {
	return adminJSON(r) || strings.Contains(r.Header.Get("Content-Type"), "application/json")
}
func readDisputeRequest(w http.ResponseWriter, r *http.Request) (disputeRequest, error) {
	var d disputeRequest
	if r.Method != http.MethodPost {
		return d, fmt.Errorf("method")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&d); err != nil {
			return d, repository.ErrAdminInvalid
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return d, repository.ErrAdminInvalid
		}
		return d, nil
	}
	if err := r.ParseForm(); err != nil {
		return d, repository.ErrAdminInvalid
	}
	var err error
	parseID := func(name string) int64 {
		if r.FormValue(name) == "" {
			return 0
		}
		n, e := strconv.ParseInt(r.FormValue(name), 10, 64)
		if e != nil {
			err = e
		}
		return n
	}
	d.DisputeID = parseID("dispute_id")
	d.OrderID = parseID("order_id")
	d.RefundID = parseID("refund_id")
	if r.FormValue("delivery_id") != "" {
		id := parseID("delivery_id")
		d.DeliveryID = &id
	}
	if raw := r.FormValue("version"); raw != "" {
		v, e := strconv.ParseInt(raw, 10, 64)
		if e != nil {
			err = e
		}
		d.Version = &v
	}
	d.ReasonCode = r.FormValue("reason_code")
	d.Description = r.FormValue("description")
	d.RequestedAmount = r.FormValue("requested_amount")
	d.ApprovedAmount = r.FormValue("approved_amount")
	d.ApprovalNote = r.FormValue("approval_note")
	d.Response = r.FormValue("response")
	d.Reason = r.FormValue("reason")
	d.Reviewed = r.FormValue("reviewed") == "1"
	if err != nil {
		return d, repository.ErrAdminInvalid
	}
	return d, nil
}

func disputeFailure(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusServiceUnavailable, "We couldn't complete that request. Check the current status before trying again."
	switch {
	case errors.Is(err, repository.ErrDisputeNotFound), errors.Is(err, repository.ErrDisputeOrderNotFound):
		status, message = 404, "That record is unavailable."
	case errors.Is(err, repository.ErrDisputeNotBuyer), errors.Is(err, repository.ErrDisputeNotSeller):
		status, message = 404, "That record is unavailable."
	case errors.Is(err, repository.ErrAdminForbidden), errors.Is(err, repository.ErrSelfApproval), errors.Is(err, services.ErrRefundAdminOnly), errors.Is(err, services.ErrDisputeAdminOnly):
		status, message = 403, "An authorized administrator who is not part of this order must review it."
	case errors.Is(err, repository.ErrAdminConflict):
		status, message = 409, "This review changed. Reload it and check the latest information before deciding."
	case errors.Is(err, repository.ErrActiveDisputeExists):
		status, message = 409, "This order already has an active dispute. Open it from My disputes."
	case errors.Is(err, repository.ErrDisputeState), errors.Is(err, repository.ErrRefundAlreadyExists), errors.Is(err, repository.ErrRefundDisputeNotUnderReview):
		status, message = 409, "This case already changed or has an approved refund. Reload it to continue."
	case errors.Is(err, repository.ErrRefundVideoEvidenceRequired):
		status, message = 400, "Quality refunds require validated video evidence from the buyer. Review the uploaded video first."
	case errors.Is(err, repository.ErrDisputeNotPaid):
		status, message = 409, "This order does not have a confirmed payment."
	case errors.Is(err, escrow.ErrEscrowDisputed):
		status, message = 409, "Receipt cannot be confirmed while this order has an active dispute."
	case errors.Is(err, escrow.ErrEscrowClosed):
		status, message = 409, "This order's funds have already been released or refunded."
	case errors.Is(err, escrow.ErrRefundExceedsPaid):
		status, message = 409, "This refund exceeds the amount still available for this order."
	case errors.Is(err, repository.ErrAdminInvalid), errors.Is(err, repository.ErrDisputeAmount), errors.Is(err, repository.ErrRefundInvalidAmount), errors.Is(err, services.ErrInvalidDisputeInput), errors.Is(err, services.ErrInvalidDisputeReason), errors.Is(err, services.ErrInvalidSellerResponse):
		status, message = 400, "Check the amount, reason, review version and required confirmation, then try again."
	}
	if disputeJSON(r) {
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = render.RenderTemplates(w, "admin-error.html", struct{ Message string }{message})
}

func (h *OrderDisputeHandler) actor(w http.ResponseWriter, r *http.Request) (*models.Farmer, bool) {
	w.Header().Set("Cache-Control", "no-store")
	f, ok := middleware.FarmerFromContext(r)
	if !ok || f == nil || h.repo == nil {
		disputeFailure(w, r, repository.ErrAdminForbidden)
		return nil, false
	}
	return f, true
}

func (h *OrderDisputeHandler) result(w http.ResponseWriter, r *http.Request, id int64, payload any) {
	if disputeJSON(r) {
		writeJSON(w, 200, payload)
		return
	}
	path := "/disputes/view"
	if strings.Contains(r.URL.Path, "/admin/") {
		path = "/admin/disputes/view"
	}
	http.Redirect(w, r, path+"?id="+strconv.FormatInt(id, 10)+"&saved=1", 303)
}

func (h *OrderDisputeHandler) Create(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	d, err := readDisputeRequest(w, r)
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	created, err := h.service.Create(r.Context(), f.ID, services.CreateOrderDisputeInput{OrderID: d.OrderID, DeliveryID: d.DeliveryID, ReasonCode: d.ReasonCode, Description: d.Description, RequestedAmount: d.RequestedAmount})
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	if disputeJSON(r) {
		writeJSON(w, 201, map[string]any{"dispute": created})
		return
	}
	h.result(w, r, created.ID, created)
}

func (h *OrderDisputeHandler) Respond(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	d, err := readDisputeRequest(w, r)
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	if err := h.service.Respond(r.Context(), d.DisputeID, f.ID, d.Response); err != nil {
		disputeFailure(w, r, err)
		return
	}
	h.result(w, r, d.DisputeID, map[string]string{"status": "seller_responded"})
}

func (h *OrderDisputeHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	d, err := readDisputeRequest(w, r)
	if err != nil || d.Version == nil || !d.Reviewed {
		disputeFailure(w, r, repository.ErrAdminInvalid)
		return
	}
	if err := h.repo.WithdrawDispute(r.Context(), d.DisputeID, f.ID, *d.Version); err != nil {
		disputeFailure(w, r, err)
		return
	}
	h.result(w, r, d.DisputeID, map[string]string{"status": "withdrawn"})
}

func (h *OrderDisputeHandler) AdminChange(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, ok := h.actor(w, r)
		if !ok {
			return
		}
		d, err := readDisputeRequest(w, r)
		if err != nil || d.Version == nil || *d.Version < 0 || !d.Reviewed {
			disputeFailure(w, r, repository.ErrAdminInvalid)
			return
		}
		var payload any = map[string]string{"status": "saved"}
		switch action {
		case "review":
			err = h.service.StartAdminReview(r.Context(), d.DisputeID, f.ID, *d.Version)
		case "reject":
			err = h.service.Reject(r.Context(), d.DisputeID, f.ID, d.Reason, *d.Version)
		case "approve":
			payload, err = h.service.ApproveRefund(r.Context(), d.DisputeID, f.ID, d.ApprovedAmount, d.ApprovalNote, *d.Version)
		case "settle":
			var refund *models.Refund
			refund, err = h.service.SettleRefund(r.Context(), d.RefundID, f.ID, *d.Version)
			payload = refund
			if refund != nil {
				d.DisputeID = refund.DisputeID
			}
		default:
			err = repository.ErrAdminInvalid
		}
		if err != nil {
			if refund, ok := payload.(*models.Refund); ok && refund != nil && action == "approve" {
				if disputeJSON(r) {
					writeJSON(w, 202, map[string]any{"refund": refund, "message": "Approval saved. Settlement is pending; open the case to check its status or retry."})
					return
				}
				http.Redirect(w, r, "/admin/disputes/view?id="+strconv.FormatInt(refund.DisputeID, 10)+"&settlement_pending=1", 303)
				return
			}
			disputeFailure(w, r, err)
			return
		}
		h.result(w, r, d.DisputeID, payload)
	}
}

type DisputePageData struct {
	Farmer                     *models.Farmer
	Purchases, Sales           []models.OrderDispute
	Dispute                    *models.OrderDispute
	Order                      *models.Order
	Refund                     *models.Refund
	Evidence                   []models.OrderDisputeEvidence
	CaseEvents                 []models.OrderDisputeEvent
	IsBuyer, IsSeller, Success bool
}

func (h *OrderDisputeHandler) Mine(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	purchases, err := h.service.ListByBuyer(r.Context(), f.ID)
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	sales, err := h.service.ListBySeller(r.Context(), f.ID)
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	if disputeJSON(r) {
		writeJSON(w, 200, map[string]any{"purchases": purchases, "sales": sales})
		return
	}
	if err := render.RenderTemplates(w, "disputes.html", DisputePageData{Farmer: f, Purchases: purchases, Sales: sales}); err != nil {
		http.Error(w, "Unable to load disputes", 503)
	}
}

func (h *OrderDisputeHandler) New(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("order_id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	order, err := h.orders.GetByID(id)
	if err != nil || order == nil || order.BuyerID != f.ID {
		http.NotFound(w, r)
		return
	}
	if err := render.RenderTemplates(w, "disputes.html", DisputePageData{Farmer: f, Order: order, IsBuyer: true}); err != nil {
		http.Error(w, "Unable to load dispute form", 503)
	}
}

func (h *OrderDisputeHandler) Case(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, ok := h.actor(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
		d, err := h.service.GetForActor(r.Context(), id, f.ID)
		if err != nil {
			disputeFailure(w, r, err)
			return
		}
		order, err := h.orders.GetByID(int(d.OrderID))
		if err != nil {
			disputeFailure(w, r, err)
			return
		}
		evidence, err := h.repo.ListEvidence(r.Context(), id)
		if err != nil {
			disputeFailure(w, r, err)
			return
		}
		events, err := h.repo.ListEvents(r.Context(), id)
		if err != nil {
			disputeFailure(w, r, err)
			return
		}
		refund, err := h.repo.GetRefundForDispute(r.Context(), id)
		if err != nil {
			disputeFailure(w, r, err)
			return
		}
		if disputeJSON(r) {
			writeJSON(w, 200, map[string]any{"dispute": d, "refund": refund, "evidence": evidence, "events": events})
			return
		}
		if admin {
			access, err := h.governance.Access(r.Context(), f.ID)
			if err != nil || !access.Can(models.AdminReviewDisputes) {
				disputeFailure(w, r, repository.ErrAdminForbidden)
				return
			}
			data := AdminPageData{Section: "disputes", Name: f.FullName, Initials: models.AdminInitials(f.FullName), Access: access, ActorID: f.ID, CanReadAudit: access.Can(models.AdminReadAudit), CanReviewDisputes: true, CanDecide: f.ID != d.BuyerID && f.ID != d.SellerID, Dispute: d, Refund: refund, Order: order, Evidence: evidence, CaseEvents: events, Success: r.URL.Query().Get("saved") == "1"}
			data.SettlementPending = r.URL.Query().Get("settlement_pending") == "1" && refund != nil && refund.Status != models.RefundSucceeded
			if err := render.RenderTemplates(w, "admin.html", data); err != nil {
				http.Error(w, "Unable to load review", 503)
			}
			return
		}
		data := DisputePageData{Farmer: f, Dispute: d, Order: order, Refund: refund, Evidence: evidence, CaseEvents: events, IsBuyer: d.BuyerID == f.ID, IsSeller: d.SellerID == f.ID, Success: r.URL.Query().Get("saved") == "1"}
		if err := render.RenderTemplates(w, "disputes.html", data); err != nil {
			http.Error(w, "Unable to load dispute", 503)
		}
	}
}

func (h *OrderDisputeHandler) Queue(exceptions bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, ok := h.actor(w, r)
		if !ok {
			return
		}
		access, err := h.governance.Access(r.Context(), f.ID)
		if err != nil || !access.Can(models.AdminReviewDisputes) {
			disputeFailure(w, r, repository.ErrAdminForbidden)
			return
		}
		data := AdminPageData{Section: "disputes", Name: f.FullName, Initials: models.AdminInitials(f.FullName), Access: access, ActorID: f.ID, CanReadAudit: access.Can(models.AdminReadAudit), CanReviewDisputes: true, StatusFilter: r.URL.Query().Get("status")}
		if exceptions {
			data.Section = "exceptions"
			data.Exceptions, err = h.repo.PaymentExceptions(r.Context(), f.ID)
		} else {
			before := int64(0)
			if raw := r.URL.Query().Get("before"); raw != "" {
				before, err = strconv.ParseInt(raw, 10, 64)
				if err != nil || before < 0 {
					disputeFailure(w, r, repository.ErrAdminInvalid)
					return
				}
			}
			data.Disputes, err = h.repo.ListForReview(r.Context(), f.ID, data.StatusFilter, before)
			if len(data.Disputes) == 25 {
				data.Next = "?" + url.Values{"status": {data.StatusFilter}, "before": {strconv.FormatInt(data.Disputes[24].ID, 10)}}.Encode()
			}
			if before > 0 {
				data.First = "?" + url.Values{"status": {data.StatusFilter}}.Encode()
			}
		}
		if err != nil {
			disputeFailure(w, r, err)
			return
		}
		if disputeJSON(r) {
			if exceptions {
				writeJSON(w, 200, data.Exceptions)
			} else {
				writeJSON(w, 200, map[string]any{"items": data.Disputes, "next": data.Next})
			}
			return
		}
		if err := render.RenderTemplates(w, "admin.html", data); err != nil {
			http.Error(w, "Unable to load review queue", 503)
		}
	}
}

func (h *OrderDisputeHandler) ConfirmReceipt(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	d, err := readDisputeRequest(w, r)
	if err != nil || !d.Reviewed || d.OrderID <= 0 {
		disputeFailure(w, r, repository.ErrAdminInvalid)
		return
	}
	if err := h.wallet.ReleaseEscrow(r.Context(), d.OrderID, f.ID); err != nil {
		disputeFailure(w, r, err)
		return
	}
	if disputeJSON(r) {
		writeJSON(w, 200, map[string]string{"status": "receipt_confirmed"})
		return
	}
	http.Redirect(w, r, "/order?id="+strconv.FormatInt(d.OrderID, 10), 303)
}

func (h *OrderDisputeHandler) UploadEvidence(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (20<<20)+(64<<10))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		disputeFailure(w, r, repository.ErrAdminInvalid)
		return
	}
	defer r.MultipartForm.RemoveAll()
	id, err := strconv.ParseInt(r.FormValue("dispute_id"), 10, 64)
	if err != nil {
		disputeFailure(w, r, repository.ErrAdminInvalid)
		return
	}
	d, err := h.service.GetForActor(r.Context(), id, f.ID)
	if err != nil || (d.BuyerID != f.ID && d.SellerID != f.ID) {
		disputeFailure(w, r, repository.ErrDisputeNotFound)
		return
	}
	file, header, err := r.FormFile("evidence")
	if err != nil {
		disputeFailure(w, r, repository.ErrAdminInvalid)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (20<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 20<<20 {
		disputeFailure(w, r, repository.ErrAdminInvalid)
		return
	}
	kind, err := evidenceContentType(data)
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		disputeFailure(w, r, err)
		return
	}
	key := hex.EncodeToString(random[:]) + ".dat"
	digest := sha256.Sum256(data)
	path, err := h.evidencePath(key, true)
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		disputeFailure(w, r, err)
		return
	}
	_, writeErr := target.Write(data)
	closeErr := target.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		disputeFailure(w, r, repository.ErrAdminInvalid)
		return
	}
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if len(name) > 160 {
		name = "Evidence"
	}
	evidence := &models.OrderDisputeEvidence{DisputeID: id, UploadedBy: f.ID, StorageKey: key, ContentHash: hex.EncodeToString(digest[:]), OriginalFilename: name, ContentType: kind, SizeBytes: int64(len(data))}
	if err := h.repo.AddEvidence(r.Context(), evidence); err != nil {
		_ = os.Remove(path)
		disputeFailure(w, r, err)
		return
	}
	h.result(w, r, id, evidence)
}

func (h *OrderDisputeHandler) DownloadEvidence(w http.ResponseWriter, r *http.Request) {
	f, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("dispute_id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.service.GetForActor(r.Context(), id, f.ID); err != nil {
		http.NotFound(w, r)
		return
	}
	evidenceID, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	items, err := h.repo.ListEvidence(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, e := range items {
		if e.ID != evidenceID || len(e.StorageKey) != 68 || filepath.Base(e.StorageKey) != e.StorageKey {
			continue
		}
		path, err := h.evidencePath(e.StorageKey, false)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err = file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != e.SizeBytes {
			http.NotFound(w, r)
			return
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil || hex.EncodeToString(hash.Sum(nil)) != e.ContentHash {
			http.NotFound(w, r)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", e.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": e.OriginalFilename}))
		http.ServeContent(w, r, "evidence", info.ModTime(), file)
		return
	}
	http.NotFound(w, r)
}
