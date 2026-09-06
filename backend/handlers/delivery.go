package handlers

import (
	"net/http"
	"strconv"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type DeliveryHandler struct {
	deliveryService *services.DeliveryService
}

func NewDeliveryHandler(deliveryService *services.DeliveryService) *DeliveryHandler {
	return &DeliveryHandler{deliveryService: deliveryService}
}

// ============================================================
// GET /deliveries
// ============================================================
func (h *DeliveryHandler) List(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var (
		deliveries []models.Delivery
		err        error
	)

	if farmer.IsBuyer() {
		deliveries, err = h.deliveryService.MyDeliveries(farmer.ID)
	} else {
		deliveries, err = h.deliveryService.MyOutgoing(farmer.ID)
	}

	if err != nil {
		http.Error(w, "Could not load deliveries", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Farmer":     farmer,
		"IsBuyer":    farmer.IsBuyer(),
		"Deliveries": deliveries,
	}

	if err := render.RenderTemplates(w, "deliveries.html", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// ============================================================
// GET /deliveries/{id}
// ============================================================
func (h *DeliveryHandler) Detail(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	delivery, err := h.deliveryService.GetByID(id)
	if err != nil || delivery == nil {
		http.NotFound(w, r)
		return
	}

	if delivery.BuyerID != farmer.ID && delivery.FarmerID != farmer.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	data := map[string]any{
		"Farmer":   farmer,
		"IsBuyer":  farmer.IsBuyer(),
		"Delivery": delivery,
	}

	if err := render.RenderTemplates(w, "delivery.html", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// ============================================================
// POST /deliveries/create
// ============================================================
func (h *DeliveryHandler) Create(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !farmer.IsBuyer() {
		http.Error(w, "Only buyers can request delivery", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	orderID, _ := strconv.Atoi(r.FormValue("order_id"))
	quantity, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)

	delivery, err := h.deliveryService.CreateFromOrder(
		orderID,
		farmer.ID,
		r.FormValue("address"),
		r.FormValue("lga"),
		r.FormValue("state"),
		r.FormValue("country"),
		quantity,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/deliveries/"+strconv.Itoa(delivery.ID), http.StatusSeeOther)
}

// ============================================================
// POST /deliveries/{id}/status
// ============================================================
func (h *DeliveryHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	if err := h.deliveryService.UpdateStatus(id, farmer.ID, r.FormValue("status")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/deliveries/"+strconv.Itoa(id), http.StatusSeeOther)
}

// ============================================================
// POST /deliveries/{id}/tracking
// ============================================================
func (h *DeliveryHandler) UpdateTracking(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	err = h.deliveryService.UpdateTracking(
		id,
		farmer.ID,
		r.FormValue("courier_name"),
		r.FormValue("courier_phone"),
		r.FormValue("tracking_number"),
		r.FormValue("vehicle_info"),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/deliveries/"+strconv.Itoa(id), http.StatusSeeOther)
}

// ============================================================
// POST /deliveries/{id}/delivered
// ============================================================
func (h *DeliveryHandler) MarkDelivered(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	if err := h.deliveryService.MarkDelivered(id, farmer.ID, r.FormValue("proof_url")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/deliveries/"+strconv.Itoa(id), http.StatusSeeOther)
}

// ============================================================
// POST /deliveries/{id}/failed
// ============================================================
func (h *DeliveryHandler) MarkFailed(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	if err := h.deliveryService.MarkFailed(id, farmer.ID, r.FormValue("reason")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/deliveries/"+strconv.Itoa(id), http.StatusSeeOther)
}

// ============================================================
// POST /deliveries/{id}/cancel
// ============================================================
func (h *DeliveryHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if err := h.deliveryService.Cancel(id, farmer.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/deliveries", http.StatusSeeOther)
}
