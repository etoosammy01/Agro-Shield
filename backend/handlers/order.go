package handlers

import (
	"net/http"
	"strconv"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type OrderHandler struct {
	orderService *services.OrderService
	wallet       *services.WalletService
}

func NewOrderHandler(orderService *services.OrderService, wallet *services.WalletService) *OrderHandler {
	return &OrderHandler{orderService: orderService, wallet: wallet}
}

// ============================================================
// GET /orders
// ============================================================
func (h *OrderHandler) List(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var (
		orders []models.Order
		err    error
	)

	if farmer.IsBuyer() {
		orders, err = h.orderService.MyPurchases(farmer.ID)
	} else {
		orders, err = h.orderService.MySales(farmer.ID)
	}

	if err != nil {
		http.Error(w, "Could not load orders", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Farmer":  farmer,
		"IsBuyer": farmer.IsBuyer(),
		"Orders":  orders,
	}

	if err := render.RenderTemplates(w, "orders.html", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// ============================================================
// GET /order?id=123
// ============================================================
func (h *OrderHandler) Detail(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// The route is registered as "/order" and links carry ?id=, so the path
	// never holds an id - PathValue was always empty and every order link
	// returned 404.
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	order, err := h.orderService.GetByID(id)
	if err != nil || order == nil {
		http.NotFound(w, r)
		return
	}

	if order.BuyerID != farmer.ID && order.SellerID != farmer.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	data := map[string]any{
		"Farmer":  farmer,
		"IsBuyer": farmer.IsBuyer(),
		"Order":   order,
		// A rejected delivery request comes back here as a short code, so the
		// message sits on the form itself instead of on a plain-text error page.
		"DeliveryError": deliveryErrorMessage(r.URL.Query().Get("delivery_error")),
	}

	if err := render.RenderTemplates(w, "order.html", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// deliveryErrorMessage maps a whitelisted code to a sentence. An unknown or
// missing code renders nothing, so arbitrary query text is never reflected.
func deliveryErrorMessage(code string) string {
	switch code {
	case "address":
		return "Enter a delivery address."
	case "quantity":
		return "Enter a quantity greater than zero."
	case "quantity_max":
		return "The delivery quantity cannot be more than the ordered quantity."
	case "forbidden":
		return "You can only request delivery for your own order."
	case "unavailable":
		return "We couldn't create that delivery right now. Please try again."
	default:
		return ""
	}
}

// ============================================================
// POST /orders
// ============================================================
func (h *OrderHandler) Place(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !farmer.IsBuyer() {
		http.Error(w, "Only buyers can place orders", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	cropID, _ := strconv.Atoi(r.FormValue("crop_id"))
	quantity, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)

	if _, err := h.wallet.PayForOrder(r.Context(), farmer.ID, cropID, quantity); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/orders", http.StatusSeeOther)
}
