package handlers

import (
	"log"
	"net/http"
	"strconv"

	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
	"time"
)

// ProductHandler handles requests related to viewing one product.
type ProductHandler struct {
	crop   *services.CropService
	events *repository.MarketEventRepository
}

// NewProductHandler creates a ProductHandler and gives it access to CropService.
func NewProductHandler(crop *services.CropService, events *repository.MarketEventRepository) *ProductHandler {
	return &ProductHandler{
		crop:   crop,
		events: events,
	}
}

// ProductDetailsHandler gets one product by its ID and displays its details page.
func (h *ProductHandler) ProductDetailsHandler(w http.ResponseWriter, r *http.Request) {
	cropID, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || cropID <= 0 {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}

	crop, err := h.crop.GetCrop(cropID)
	if err != nil {
		log.Println("failed to load product:", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if crop == nil {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}
	if h.events != nil {
		var userID *int
		if farmer, ok := middleware.FarmerFromContext(r); ok && farmer != nil {
			id := farmer.ID
			userID = &id
		}
		_ = h.events.RecordListingView(cropID, userID, r.URL.Query().Get("session_id"), time.Hour)
	}

	data := ProductPageData{
		Crop: crop,
	}

	if err := render.RenderTemplates(w, "product.html", data); err != nil {
		log.Println("render error:", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// ProductPageData contains the information sent to the product details page.
type ProductPageData struct {
	Crop  interface{}
	Error string
}
