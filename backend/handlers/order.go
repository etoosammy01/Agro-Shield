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
	checkout     *Checkout
	wallet       *services.WalletService
	disputes     *services.OrderDisputeService
}

func (h *OrderHandler) ConfigureDisputes(wallet *services.WalletService, disputes *services.OrderDisputeService) {
	h.wallet, h.disputes = wallet, disputes
}

func NewOrderHandler(orderService *services.OrderService, checkout *Checkout) *OrderHandler {
	return &OrderHandler{orderService: orderService, checkout: checkout}
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
		"IsBuyer": order.BuyerID == farmer.ID,
		"Order":   order,
		// A rejected delivery request comes back here as a short code, so the
		// message sits on the form itself instead of on a plain-text error page.
		"DeliveryError": deliveryErrorMessage(r.URL.Query().Get("delivery_error")),
	}
	if h.wallet != nil && h.disputes != nil && h.disputes.Workflows() != nil {
		hold, err := h.wallet.EscrowForOrder(r.Context(), int64(order.ID))
		if err != nil {
			http.Error(w, "Could not load order funds", 503)
			return
		}
		dispute, err := h.disputes.Workflows().LatestForOrder(r.Context(), int64(order.ID), farmer.ID)
		if err != nil {
			http.Error(w, "Could not load order support", 503)
			return
		}
		data["Escrow"], data["Dispute"], data["SupportEnabled"] = hold, dispute, true
		active := dispute != nil && (dispute.Status == models.DisputeSubmitted || dispute.Status == models.DisputeSellerResponded || dispute.Status == models.DisputeUnderReview)
		data["CanConfirmReceipt"] = order.BuyerID == farmer.ID && hold != nil && hold.Status == "held" && !active
		data["CanReportIssue"] = order.BuyerID == farmer.ID && !active && (hold == nil || hold.Status != "closed")
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
// POST /orders/place
// ============================================================
func (h *OrderHandler) Place(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !farmer.IsBuyer() {
		http.Error(w, "Only buyers can place orders", http.StatusForbidden)
		return
	}

	result, err := h.checkout.Pay(w, r, farmer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if result == nil {
		return
	}

	http.Redirect(w, r, "/orders", http.StatusSeeOther)
}
