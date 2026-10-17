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
// GET /delivery?id=123
// ============================================================
func (h *DeliveryHandler) Detail(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// The route is registered as "/delivery" and links carry ?id=, so the path
	// never holds an id - PathValue was always empty and every delivery link
	// returned 404.
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
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

	// Creating a delivery has a side effect, so it must never happen on a GET.
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
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
		// Send the buyer back to the order page so the message appears on the
		// form itself instead of as a plain-text error page.
		code := deliveryErrorCode(err)
		if code == "notfound" {
			http.Redirect(w, r, "/orders", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/order?id="+strconv.Itoa(orderID)+"&delivery_error="+code+"#request-delivery", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/delivery?id="+strconv.Itoa(delivery.ID), http.StatusSeeOther)
}

// deliveryErrorCode turns a CreateFromOrder error into a short code the order
// page understands. Only codes in this whitelist are ever placed in a URL or a
// page, so a raw error string (which could carry database detail) is never
// shown to the buyer.
func deliveryErrorCode(err error) string {
	switch err.Error() {
	case "delivery address is required":
		return "address"
	case "quantity must be greater than zero":
		return "quantity"
	case "order not found":
		return "notfound"
	case "you can only create a delivery for your own order":
		return "forbidden"
	case "delivery quantity cannot exceed the ordered quantity":
		return "quantity_max"
	default:
		return "unavailable"
	}
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

	id, err := strconv.Atoi(r.FormValue("id"))
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

	http.Redirect(w, r, "/delivery?id="+strconv.Itoa(id), http.StatusSeeOther)
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

	id, err := strconv.Atoi(r.FormValue("id"))
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

	http.Redirect(w, r, "/delivery?id="+strconv.Itoa(id), http.StatusSeeOther)
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

	id, err := strconv.Atoi(r.FormValue("id"))
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

	http.Redirect(w, r, "/delivery?id="+strconv.Itoa(id), http.StatusSeeOther)
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

	id, err := strconv.Atoi(r.FormValue("id"))
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

	http.Redirect(w, r, "/delivery?id="+strconv.Itoa(id), http.StatusSeeOther)
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

	id, err := strconv.Atoi(r.FormValue("id"))
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
