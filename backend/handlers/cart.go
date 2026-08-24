package handlers

import (
	"log"
	"net/http"
	"strconv"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

// ============================================================
// CART HANDLER
//
// Responsibility:
// - Display the buyer's cart.
// - Display products accepted through negotiation.
// - Remove products from the cart.
// - Protect the cart so users can only access their own items.
// ============================================================

type Cart struct {
	service *services.CartService
	events  *repository.MarketEventRepository
}

// ============================================================
// CREATE CART HANDLER
//
// Connects the Cart Handler to the Cart Service.
// ============================================================

func NewCartHandler(service *services.CartService, events *repository.MarketEventRepository) *Cart {
	return &Cart{
		service: service,
		events:  events,
	}
}

// ============================================================
// CART PAGE DATA
//
// This is the data that will be sent to cart.html.
// ============================================================

type CartPageData struct {
	Items  []models.CartItem
	UserID int
	Error  string
	Total  float64
}

// ============================================================
// CART HANDLER
//
// GET:
//     Displays the buyer's cart.
//
// POST:
//     Handles cart actions such as removing an item.
// ============================================================

func (h *Cart) Handler(w http.ResponseWriter, r *http.Request) {

	// ========================================================
	// 1. GET LOGGED-IN USER
	// ========================================================

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	// ========================================================
	// 2. CART IS FOR BUYERS
	//
	// A farmer who is not acting as a buyer does not need
	// access to the buyer cart.
	// ========================================================

	if !farmer.IsBuyer() {
		http.Redirect(
			w,
			r,
			"/marketplace",
			http.StatusSeeOther,
		)
		return
	}

	// ========================================================
	// 3. HANDLE HTTP METHOD
	// ========================================================

	switch r.Method {

	case http.MethodGet:

		// ----------------------------------------------------
		// GET CART
		// ----------------------------------------------------

		log.Println("User Visited Cart")
		if h.events != nil {
			uid := farmer.ID
			_ = h.events.Record(&models.MarketEvent{EventType: "cart_view", UserID: &uid})
			_ = h.events.Record(&models.MarketEvent{EventType: "checkout_started", UserID: &uid})
		}

		h.render(
			w,
			farmer.ID,
			"",
		)

	case http.MethodPost:

		// ----------------------------------------------------
		// POST CART ACTION
		// ----------------------------------------------------

		action := r.FormValue("action")

		switch action {

		// ====================================================
		// REMOVE ITEM
		// ====================================================

		case "remove":

			cartID, err := strconv.Atoi(
				r.FormValue("cart_id"),
			)

			if err != nil || cartID <= 0 {
				h.render(
					w,
					farmer.ID,
					"Invalid cart item",
				)
				return
			}

			// ------------------------------------------------
			// Remove item through the Cart Service.
			//
			// The service makes sure the item belongs
			// to the logged-in buyer.
			// ------------------------------------------------

			if err := h.service.RemoveFromCart(
				farmer.ID,
				cartID,
			); err != nil {

				log.Println(
					"failed to remove cart item:",
					err,
				)

				h.render(
					w,
					farmer.ID,
					err.Error(),
				)

				return
			}
			if h.events != nil {
				uid := farmer.ID
				_ = h.events.Record(&models.MarketEvent{EventType: "cart_item_removed", UserID: &uid})
			}

			// ------------------------------------------------
			// Return to cart after successful removal.
			// ------------------------------------------------

			http.Redirect(
				w,
				r,
				"/cart",
				http.StatusSeeOther,
			)

		default:

			h.render(
				w,
				farmer.ID,
				"Unknown cart action",
			)
		}

	default:

		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

// ============================================================
// RENDER CART
//
// Responsibility:
// - Load the current buyer's cart.
// - Prepare the data.
// - Render cart.html.
// ============================================================

func (h *Cart) render(
	w http.ResponseWriter,
	buyerID int,
	errMsg string,
) {

	// ========================================================
	// 1. LOAD CART
	// ========================================================

	items, err := h.service.MyCart(buyerID)

	if err != nil {

		log.Println(
			"failed to load cart:",
			err,
		)

		errMsg = err.Error()
	}

	// ========================================================
	// 2. PREPARE PAGE DATA
	// ========================================================

	data := CartPageData{
		Items:  items,
		UserID: buyerID,
		Error:  errMsg,
	}
	_, data.Total = cartSummary(items)

	// ========================================================
	// 3. RENDER CART HTML
	// ========================================================

	if err := render.RenderTemplates(
		w,
		"cart.html",
		data,
	); err != nil {

		log.Println(
			"cart render error:",
			err,
		)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
	}
}
