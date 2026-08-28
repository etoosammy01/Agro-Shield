package handlers

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

// ProductHandler handles requests related to viewing one product.
type ProductHandler struct {
	crop   *services.CropService
	events *repository.MarketEventRepository
}

// NewProductHandler creates a new ProductHandler.
func NewProductHandler(
	crop *services.CropService,
	events *repository.MarketEventRepository,
) *ProductHandler {
	return &ProductHandler{
		crop:   crop,
		events: events,
	}
}

// ProductDetailsHandler gets one product by ID
// and displays its details page.
func (h *ProductHandler) ProductDetailsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	// ========================================================
	// 1. GET PRODUCT ID
	// ========================================================

	idString := r.URL.Query().Get("id")

	cropID, err := strconv.Atoi(idString)
	if err != nil || cropID <= 0 {
		http.Error(
			w,
			"Invalid product ID",
			http.StatusBadRequest,
		)
		return
	}

	// ========================================================
	// 2. GET PRODUCT
	// ========================================================

	crop, err := h.crop.GetCrop(cropID)
	if err != nil {
		log.Println("failed to load product:", err)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
		return
	}

	if crop == nil {
		http.Error(
			w,
			"Product not found",
			http.StatusNotFound,
		)
		return
	}

	// ========================================================
	// 3. GET PRODUCT IMAGES
	// ========================================================

	images, err := h.crop.ListImages(cropID)
	if err != nil {
		log.Println("failed to load product images:", err)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
		return
	}

	// ========================================================
	// 4. RECORD MARKETPLACE VIEW
	// ========================================================

	if h.events != nil {
		var userID *int

		farmer, ok := middleware.FarmerFromContext(r)

		if ok && farmer != nil {
			id := farmer.ID
			userID = &id
		}

		sessionID := r.URL.Query().Get("session_id")

		_ = h.events.RecordListingView(
			cropID,
			userID,
			sessionID,
			time.Hour,
		)
	}

	// ========================================================
	// 5. PREPARE TEMPLATE DATA
	// ========================================================

	data := ProductPageData{
		Crop:   crop,
		Images: images,
	}

	// ========================================================
	// 6. RENDER PRODUCT PAGE
	// ========================================================

	if err := render.RenderTemplates(
		w,
		"product.html",
		data,
	); err != nil {
		log.Println("render product page error:", err)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
		return
	}
}

// ============================================================
// PRODUCT PAGE DATA
// ============================================================
//
// This is what product.html receives.
//
// .Crop   -> product information
// .Images -> all product pictures
// .Error  -> optional error message
// ============================================================

type ProductPageData struct {
	Crop   *models.Crop
	Images []models.CropImage
	Error  string
}