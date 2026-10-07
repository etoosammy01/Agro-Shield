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
// GET /orders/{id}
// ============================================================
func (h *OrderHandler) Detail(w http.ResponseWriter, r *http.Request) {
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
	}

	if err := render.RenderTemplates(w, "order.html", data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
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
