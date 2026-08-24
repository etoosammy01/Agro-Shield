package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type Marketplace struct {
	crop   *services.CropService
	order  *services.OrderService
	events *repository.MarketEventRepository
}

func NewMarketplaceHandler(crop *services.CropService, order *services.OrderService, events *repository.MarketEventRepository) *Marketplace {
	return &Marketplace{crop: crop, order: order, events: events}
}

type MarketplacePageData struct {
	Crops         []models.Crop
	IsBuyer       bool
	CurrentUserID int
	Message       string
	Error         string
}

func (h *Marketplace) MarketplaceHandler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		log.Println("User Visited Marketplace")
		h.record("marketplace_view", nil, farmer.ID, r)
		h.render(w, farmer.IsBuyer(), farmer.ID, "", "")
	case http.MethodPost:
		if r.FormValue("action") == "search" {
			uid := farmer.ID
			if h.events != nil {
				_ = h.events.RecordSearch(r.FormValue("search_term"), r.FormValue("normalized_produce"), farmer.Location, &uid, r.FormValue("session_id"))
			}
			h.render(w, farmer.IsBuyer(), farmer.ID, "", "")
			return
		}
		if !farmer.IsBuyer() {
			h.render(w, farmer.IsBuyer(), farmer.ID, "", "Only buyer accounts can place orders")
			return
		}
		cropID, _ := strconv.Atoi(r.FormValue("crop_id"))
		quantity, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)
		if err := h.order.PlaceOrder(farmer.ID, cropID, quantity); err != nil {
			h.render(w, farmer.IsBuyer(), farmer.ID, "", err.Error())
			return
		}
		h.record("checkout_completed", &cropID, farmer.ID, r, fmt.Sprintf(`{"quantity":%g}`, quantity))
		h.render(w, farmer.IsBuyer(), farmer.ID, "Order placed successfully!", "")
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Marketplace) record(eventType string, cropID *int, userID int, r *http.Request, metadata ...string) {
	if h.events == nil {
		return
	}
	uid := userID
	sessionID, _ := r.Context().Value("session_id").(string)
	meta := "{}"
	if len(metadata) > 0 {
		meta = metadata[0]
	}
	_ = h.events.Record(&models.MarketEvent{EventType: eventType, CropID: cropID, UserID: &uid, SessionID: sessionID, Metadata: meta})
}

func (h *Marketplace) render(w http.ResponseWriter, isBuyer bool, currentUserID int, message, errMsg string) {
	crops, err := h.crop.AvailableCrops()
	if err != nil {
		log.Println("failed to load crops:", err)
	}
	data := MarketplacePageData{
		Crops:         crops,
		IsBuyer:       isBuyer,
		CurrentUserID: currentUserID,
		Message:       message,
		Error:         errMsg,
	}
	if err := render.RenderTemplates(w, "marketplace.html", data); err != nil {
		log.Println("render error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
