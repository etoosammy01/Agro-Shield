package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type Marketplace struct {
	crop         *services.CropService
	wallet       *services.WalletService
	events       *repository.MarketEventRepository
	notification *services.NotificationService
}

func NewMarketplaceHandler(crop *services.CropService, wallet *services.WalletService, events *repository.MarketEventRepository, notification *services.NotificationService) *Marketplace {
	return &Marketplace{crop: crop, wallet: wallet, events: events, notification: notification}
}

type MarketplacePageData struct {
	Crops         []models.Crop
	IsBuyer       bool
	CurrentUserID int
	Message       string
	Error         string
	SearchTerm    string
	// CheckoutTokens gives each rendered buy form its own idempotency key. A
	// page with several cards must not make the second purchase look like a
	// retry of the first, so the token is per card rather than per page.
	CheckoutTokens map[int]string
}

// newCheckoutToken returns an unguessable key for one rendered checkout form.
func newCheckoutToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
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
		h.render(w, farmer.IsBuyer(), farmer.ID, "", "", strings.TrimSpace(r.URL.Query().Get("q")))
	case http.MethodPost:
		if r.FormValue("action") == "search" {
			term := strings.TrimSpace(r.FormValue("search_term"))
			uid := farmer.ID
			if h.events != nil {
				_ = h.events.RecordSearch(term, r.FormValue("normalized_produce"), farmer.Location, &uid, r.FormValue("session_id"))
			}
			h.render(w, farmer.IsBuyer(), farmer.ID, "", "", term)
			return
		}
		if !farmer.IsBuyer() {
			h.render(w, farmer.IsBuyer(), farmer.ID, "", "Only buyer accounts can place orders", "")
			return
		}
		cropID, _ := strconv.Atoi(r.FormValue("crop_id"))
		quantity, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)
		checkoutToken := strings.TrimSpace(r.FormValue("checkout_token"))
		if checkoutToken == "" {
			h.render(w, farmer.IsBuyer(), farmer.ID, "", "This page is out of date. Refresh the marketplace and try again. No order was placed.", "")
			return
		}
		orderID, alreadyPlaced, err := h.wallet.PayForOrder(r.Context(), farmer.ID, cropID, quantity, checkoutToken)
		if err != nil {
			h.render(w, farmer.IsBuyer(), farmer.ID, "", userError(err, "We couldn't complete that purchase. No order was placed; check the quantity and your wallet balance, then try again."), "")
			return
		}
		if alreadyPlaced {
			// The page was submitted twice with one checkout token. The buyer was
			// charged once, so say that rather than implying a second order.
			h.render(w, farmer.IsBuyer(), farmer.ID, "This order was already placed. Your wallet was charged only once.", "", "")
			return
		}
		h.record("checkout_completed", &cropID, farmer.ID, r, fmt.Sprintf(`{"quantity":%g}`, quantity))
		if h.notification != nil {
			_ = h.notification.CreateNotification(farmer.ID, "Order placed",
				fmt.Sprintf("Order #%d was paid from your wallet.", orderID), "order_placed")
			if crop, cropErr := h.crop.GetCrop(cropID); cropErr == nil && crop != nil {
				_ = h.notification.CreateNotification(crop.FarmerID, "New order",
					fmt.Sprintf("%s ordered %g %s of %s.", farmer.FullName, quantity, crop.Unit, crop.Name), "order_placed")
			}
		}
		h.render(w, farmer.IsBuyer(), farmer.ID, "Order placed successfully!", "", "")
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

func (h *Marketplace) render(w http.ResponseWriter, isBuyer bool, currentUserID int, message, errMsg, searchTerm string) {
	var (
		crops []models.Crop
		err   error
	)
	if strings.TrimSpace(searchTerm) == "" {
		crops, err = h.crop.AvailableCrops()
	} else {
		crops, err = h.crop.SearchAvailableCrops(searchTerm)
	}
	if err != nil {
		log.Println("failed to load crops:", err)
		errMsg = "We couldn't load the marketplace right now. Please try again."
	}
	tokens := make(map[int]string, len(crops))
	for _, crop := range crops {
		token, tokenErr := newCheckoutToken()
		if tokenErr != nil {
			log.Println("could not generate checkout token:", tokenErr)
			if errMsg == "" {
				errMsg = "Checkout is temporarily unavailable. Please refresh and try again."
			}
			break
		}
		tokens[crop.ID] = token
	}
	data := MarketplacePageData{
		Crops:          crops,
		IsBuyer:        isBuyer,
		CurrentUserID:  currentUserID,
		Message:        message,
		Error:          errMsg,
		SearchTerm:     searchTerm,
		CheckoutTokens: tokens,
	}
	if err := render.RenderTemplates(w, "marketplace.html", data); err != nil {
		log.Println("render error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
