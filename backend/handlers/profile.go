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

type Profile struct {
	crop    *services.CropService
	order   *services.OrderService
	farmers *repository.FarmerRepository
	wallet  *services.WalletService
}

func NewProfileHandler(crop *services.CropService, order *services.OrderService, farmers *repository.FarmerRepository, wallet *services.WalletService) *Profile {
	return &Profile{crop: crop, order: order, farmers: farmers, wallet: wallet}
}

// ProfilePageData embeds the farmer/buyer so the template can use .FullName,
// .Phone, .Location, .Role, .CreatedAt directly, plus role-specific data.
type ProfilePageData struct {
	*models.Farmer
	IsOwner       bool
	IsWalletAdmin bool
	Error         string
	Success       string
	Crops         []models.Crop
	Purchases     []models.Order
	Sales         []models.Order
}

func (h *Profile) ProfileHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("User Visited Profile")

	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if idValue := r.URL.Query().Get("id"); idValue != "" {
		id, err := strconv.Atoi(idValue)
		if err != nil || id <= 0 {
			http.Error(w, "Invalid profile ID", http.StatusBadRequest)
			return
		}
		profile, err := h.farmers.GetByID(id)
		if err != nil || profile == nil {
			http.NotFound(w, r)
			return
		}
		farmer = profile
	}

	data := ProfilePageData{
		Farmer: farmer, IsOwner: r.URL.Query().Get("id") == "",
		Error: r.URL.Query().Get("error"), Success: r.URL.Query().Get("success"),
	}
	data.IsWalletAdmin = h.wallet.IsAdmin(farmer.ID)

	if !data.IsOwner {
		// Public profile: contact details only.
	} else if farmer.IsBuyer() {
		if purchases, err := h.order.MyPurchases(farmer.ID); err == nil {
			data.Purchases = purchases
		} else {
			log.Println("failed to load purchases:", err)
		}
	} else {
		if crops, err := h.crop.MyCrops(farmer.ID); err == nil {
			data.Crops = crops
		} else {
			log.Println("failed to load crops:", err)
		}
		if sales, err := h.order.MySales(farmer.ID); err == nil {
			data.Sales = sales
		} else {
			log.Println("failed to load sales:", err)
		}
	}

	if err := render.RenderTemplates(w, "profile.html", data); err != nil {
		log.Println("err Render Problem", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
