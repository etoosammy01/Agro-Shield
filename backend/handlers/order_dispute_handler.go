package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
)

type OrderDisputeHandler struct {
	service *services.OrderDisputeService
}

func NewOrderDisputeHandler(service *services.OrderDisputeService) *OrderDisputeHandler {
	return &OrderDisputeHandler{service: service}
}

// POST /orders/disputes
func (h *OrderDisputeHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return

	}

	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil || !farmer.IsBuyer() {
		http.Error(w, "Only authenticated buyers can open disputes", http.StatusForbidden)
		return
	}

	var req struct {
		OrderID         int64  `json:"order_id"`
		DeliveryID      *int64 `json:"delivery_id"`
		ReasonCode      string `json:"reason_code"`
		Description     string `json:"description"`
		RequestedAmount string `json:"requested_amount"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid dispute request", http.StatusBadRequest)
		return
	}

	// Reject additional JSON values in the same request body.
	var extra any
	if err := json.NewDecoder(r.Body).Decode(&extra); err != io.EOF {
		http.Error(w, "Invalid dispute request", http.StatusBadRequest)
		return
	}

	dispute, err := h.service.Create(r.Context(), farmer.ID, services.CreateOrderDisputeInput{
		OrderID:         req.OrderID,
		DeliveryID:      req.DeliveryID,
		ReasonCode:      req.ReasonCode,
		Description:     req.Description,
		RequestedAmount: req.RequestedAmount,
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidDisputeInput),
			errors.Is(err, services.ErrInvalidDisputeReason),
			errors.Is(err, repository.ErrDisputeAmount),
			errors.Is(err, repository.ErrDisputeDelivery):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})

		case errors.Is(err, repository.ErrDisputeOrderNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Order not found"})

		case errors.Is(err, repository.ErrDisputeNotBuyer):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "You cannot dispute this order"})

		case errors.Is(err, repository.ErrDisputeNotPaid):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "This order does not have a confirmed wallet payment"})

		case errors.Is(err, repository.ErrActiveDisputeExists):
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "An active dispute already exists for this order",
			})
		default:
			log.Printf("create order dispute for buyer %d: %v", farmer.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not create dispute"})
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "Dispute submitted successfully",
		"dispute": dispute,
	})
}

// POST /orders/disputes/respond
func (h *OrderDisputeHandler) Respond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		DisputeID int64  `json:"dispute_id"`
		Response  string `json:"response"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid response request", http.StatusBadRequest)
		return
	}

	err := h.service.Respond(r.Context(), req.DisputeID, farmer.ID, req.Response)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidDisputeInput),
			errors.Is(err, services.ErrInvalidSellerResponse):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})

		case errors.Is(err, repository.ErrDisputeNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Dispute not found"})

		case errors.Is(err, repository.ErrDisputeState):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "This dispute can no longer receive a seller response"})

		default:
			log.Printf("seller response for dispute %d by user %d: %v", req.DisputeID, farmer.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not submit response"})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Your response was submitted successfully",
	})
}

// Ensure the handler and model remain connected through the expected API type.

func (h *OrderDisputeHandler) AdminReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		DisputeID int64 `json:"dispute_id"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	err := h.service.StartAdminReview(r.Context(), req.DisputeID, farmer.ID)
	if err != nil {
		h.writeAdminError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Dispute moved into administrator review",
	})
}

func (h *OrderDisputeHandler) AdminReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		DisputeID int64  `json:"dispute_id"`
		Reason    string `json:"reason"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	err := h.service.Reject(r.Context(), req.DisputeID, farmer.ID, req.Reason)
	if err != nil {
		h.writeAdminError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Dispute rejected and decision recorded",
	})
}

func (h *OrderDisputeHandler) writeAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrDisputeAdminOnly):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Administrator access required"})
	case errors.Is(err, services.ErrInvalidDisputeInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, repository.ErrDisputeNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Dispute not found"})
	case errors.Is(err, repository.ErrDisputeState):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Dispute cannot be changed in its current state"})
	default:
		log.Printf("administrator dispute action failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not process dispute"})
	}
}


func (h *OrderDisputeHandler) ApproveRefund(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		DisputeID      int64  `json:"dispute_id"`
		ApprovedAmount string `json:"approved_amount"`
		ApprovalNote   string `json:"approval_note"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "Invalid refund approval request", http.StatusBadRequest)
		return
	}

	refund, err := h.service.ApproveRefund(
		r.Context(),
		req.DisputeID,
		farmer.ID,
		req.ApprovedAmount,
		req.ApprovalNote,
	)

	if err != nil {
		switch {
		case errors.Is(err, services.ErrRefundAdminOnly):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "Administrator access required",
			})

		case errors.Is(err, services.ErrInvalidDisputeInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})

		case errors.Is(err, services.ErrRefundStoreUnavailable):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Refund service is unavailable",
			})

		case errors.Is(err, repository.ErrDisputeNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "Dispute not found",
			})

		case errors.Is(err, repository.ErrRefundInvalidAmount):
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})

		case errors.Is(err, repository.ErrRefundVideoEvidenceRequired):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "Video evidence is required before approving a quality refund",
			})

		case errors.Is(err, repository.ErrRefundAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "A refund already exists for this dispute",
			})

		case errors.Is(err, repository.ErrRefundDisputeNotUnderReview):
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "Dispute must be under administrator review",
			})

		default:
			log.Printf("approve refund for dispute %d: %v", req.DisputeID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "Could not approve refund",
			})
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "Refund approved. Financial settlement is still pending.",
		"refund": map[string]any{
			"id":              refund.ID,
			"dispute_id":      refund.DisputeID,
			"order_id":        refund.OrderID,
			"requested_amount": refund.RequestedAmount,
			"approved_amount": refund.ApprovedAmount,
			"currency":         refund.Currency,
			"status":           refund.Status,
			"reference":        refund.Reference,
		},
	})
}

