package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"backend/internal/models"
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
var _ *models.OrderDispute

func (r *OrderDisputeRepository) StartAdminReview(
	ctx context.Context,
	disputeID int64,
	adminID int,
) error {
	if disputeID <= 0 || adminID <= 0 {
		return fmt.Errorf("invalid dispute or administrator")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM order_disputes
		WHERE id = $1
		FOR UPDATE
	`, disputeID).Scan(&currentStatus)

	if errors.Is(err, sql.ErrNoRows) {
		return ErrDisputeNotFound
	}
	if err != nil {
		return err
	}

	if currentStatus != models.DisputeSubmitted &&
		currentStatus != models.DisputeSellerResponded {
		return ErrDisputeState
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE order_disputes
		SET status = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, models.DisputeUnderReview, disputeID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO order_dispute_events (
			dispute_id, actor_id, actor_role, event_type,
			from_status, to_status, note
		)
		VALUES ($1, $2, 'admin', 'admin_review_started', $3, $4, $5)
	`,
		disputeID,
		adminID,
		currentStatus,
		models.DisputeUnderReview,
		"Administrator started reviewing this dispute",
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *OrderDisputeRepository) Reject(
	ctx context.Context,
	disputeID int64,
	adminID int,
	reason string,
) error {
	if disputeID <= 0 || adminID <= 0 {
		return fmt.Errorf("invalid dispute or administrator")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("rejection reason is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM order_disputes
		WHERE id = $1
		FOR UPDATE
	`, disputeID).Scan(&currentStatus)

	if errors.Is(err, sql.ErrNoRows) {
		return ErrDisputeNotFound
	}
	if err != nil {
		return err
	}

	switch currentStatus {
	case models.DisputeSubmitted,
		models.DisputeSellerResponded,
		models.DisputeUnderReview:
	default:
		return ErrDisputeState
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE order_disputes
		SET status = $1,
		    resolution_note = $2,
		    resolved_by = $3,
		    resolved_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $4
	`,
		models.DisputeResolvedRejected,
		reason,
		adminID,
		disputeID,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO order_dispute_events (
			dispute_id, actor_id, actor_role, event_type,
			from_status, to_status, note
		)
		VALUES ($1, $2, 'admin', 'dispute_rejected', $3, $4, $5)
	`,
		disputeID,
		adminID,
		currentStatus,
		models.DisputeResolvedRejected,
		reason,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}
