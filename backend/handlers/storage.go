package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

// Storage handles farmer product/storage operations.
type Storage struct {
	crop *services.CropService
}

// NewStorageHandler creates a new Storage handler.
func NewStorageHandler(crop *services.CropService) *Storage {
	return &Storage{
		crop: crop,
	}
}

// StoragePageData contains the information needed
// to render the farmer's storage page.
type StoragePageData struct {
	FullName string
	Crops    []models.Crop
	EditCrop *models.Crop
	Error    string
}

// StorageHandler handles all farmer storage requests.
func (h *Storage) StorageHandler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Buyers don't have produce storage.
	if farmer.IsBuyer() {
		http.Redirect(w, r, "/marketplace", http.StatusSeeOther)
		return
	}

	switch r.Method {

	case http.MethodGet:
		log.Println("User visited storage")

		var editCrop *models.Crop

		if editID := strings.TrimSpace(r.URL.Query().Get("edit")); editID != "" {
			cropID, err := strconv.Atoi(editID)

			if err == nil && cropID > 0 {
				candidate, getErr := h.crop.GetCrop(cropID)

				if getErr == nil &&
					candidate != nil &&
					candidate.FarmerID == farmer.ID {

					editCrop = candidate
				}
			}
		}

		h.renderPage(
			w,
			farmer.ID,
			farmer.FullName,
			"",
			editCrop,
		)

	case http.MethodPost:

		if strings.HasPrefix(
			strings.ToLower(r.Header.Get("Content-Type")),
			"multipart/form-data",
		) {
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				h.render(
					w,
					farmer.ID,
					farmer.FullName,
					"Couldn't process the form",
				)
				return
			}
		} else {
			if err := r.ParseForm(); err != nil {
				h.render(
					w,
					farmer.ID,
					farmer.FullName,
					"Couldn't process the form",
				)
				return
			}
		}

		action := strings.TrimSpace(r.FormValue("action"))

		switch action {

		case "create":
			h.createCrop(
				w,
				r,
				farmer.ID,
				farmer.FullName,
			)

		case "update":
			h.updateCrop(
				w,
				r,
				farmer.ID,
				farmer.FullName,
			)

		case "unlist":
			h.unlistCrop(
				w,
				r,
				farmer.ID,
				farmer.FullName,
			)

		case "relist":
			h.relistCrop(
				w,
				r,
				farmer.ID,
				farmer.FullName,
			)

		case "delete":
			h.deleteCrop(
				w,
				r,
				farmer.ID,
				farmer.FullName,
			)

		default:
			h.render(
				w,
				farmer.ID,
				farmer.FullName,
				"Invalid product action",
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

// createCrop handles creation of a new product.
func (h *Storage) createCrop(
	w http.ResponseWriter,
	r *http.Request,
	farmerID int,
	fullName string,
) {
	name := strings.TrimSpace(r.FormValue("produce"))
	unit := strings.TrimSpace(r.FormValue("unit"))
	location := strings.TrimSpace(r.FormValue("location"))

	latitude, _ := strconv.ParseFloat(
		r.FormValue("latitude"),
		64,
	)

	longitude, _ := strconv.ParseFloat(
		r.FormValue("longitude"),
		64,
	)

	accuracy, _ := strconv.ParseFloat(
		r.FormValue("location_accuracy"),
		64,
	)

	quantity, err := strconv.ParseFloat(
		strings.TrimSpace(r.FormValue("quantity")),
		64,
	)

	if err != nil {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid quantity",
		)
		return
	}

	price, err := strconv.ParseFloat(
		strings.TrimSpace(r.FormValue("price")),
		64,
	)

	if err != nil {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid price",
		)
		return
	}

	listForSale := r.FormValue("list_for_sale") == "on"

	// Product image is compulsory.
	imageURL, err := saveUploadedFile(
		r,
		"produce_image",
		"crops",
	)

	if err != nil {
		log.Println(
			"produce image upload failed:",
			err,
		)

		h.render(
			w,
			farmerID,
			fullName,
			"Failed to upload product picture",
		)
		return
	}

	imageURL = strings.TrimSpace(imageURL)

	if imageURL == "" {
		h.render(
			w,
			farmerID,
			fullName,
			"At least one crop picture is required",
		)
		return
	}

	// CropService expects []string.
	imageURLs := []string{
		imageURL,
	}

	if err := h.crop.AddCrop(
		farmerID,
		name,
		unit,
		location,
		quantity,
		price,
		listForSale,
		imageURLs,
		latitude,
		longitude,
		accuracy,
	); err != nil {

		h.render(
			w,
			farmerID,
			fullName,
			err.Error(),
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/storage",
		http.StatusSeeOther,
	)
}

// updateCrop handles editing an existing product.
func (h *Storage) updateCrop(
	w http.ResponseWriter,
	r *http.Request,
	farmerID int,
	fullName string,
) {
	cropID, err := strconv.Atoi(
		strings.TrimSpace(r.FormValue("crop_id")),
	)

	if err != nil || cropID <= 0 {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid product ID",
		)
		return
	}

	name := strings.TrimSpace(r.FormValue("produce"))
	unit := strings.TrimSpace(r.FormValue("unit"))
	location := strings.TrimSpace(r.FormValue("location"))

	latitude, _ := strconv.ParseFloat(
		r.FormValue("latitude"),
		64,
	)

	longitude, _ := strconv.ParseFloat(
		r.FormValue("longitude"),
		64,
	)

	accuracy, _ := strconv.ParseFloat(
		r.FormValue("location_accuracy"),
		64,
	)

	quantity, err := strconv.ParseFloat(
		strings.TrimSpace(r.FormValue("quantity")),
		64,
	)

	if err != nil {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid quantity",
		)
		return
	}

	price, err := strconv.ParseFloat(
		strings.TrimSpace(r.FormValue("price")),
		64,
	)

	if err != nil {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid price",
		)
		return
	}

	listForSale := r.FormValue("list_for_sale") == "on"

	// --------------------------------------------------------
	// PRODUCT IMAGES
	// --------------------------------------------------------

	var imageURLs []string

	// Keep the existing image if one exists.
	existingImageURL := strings.TrimSpace(
		r.FormValue("image_url"),
	)

	if existingImageURL != "" {
		imageURLs = append(
			imageURLs,
			existingImageURL,
		)
	}

	// A new image is optional during update.
	uploadedURL, uploadErr := saveUploadedFile(
		r,
		"produce_image",
		"crops",
	)

	if uploadErr != nil {
		log.Println(
			"updated produce image upload failed:",
			uploadErr,
		)
	} else {
		uploadedURL = strings.TrimSpace(uploadedURL)

		if uploadedURL != "" {
			imageURLs = append(
				imageURLs,
				uploadedURL,
			)
		}
	}

	// Make sure there is still at least one image.
	if len(imageURLs) == 0 {
		h.render(
			w,
			farmerID,
			fullName,
			"At least one crop picture is required",
		)
		return
	}

	if err := h.crop.UpdateCrop(
		farmerID,
		cropID,
		name,
		unit,
		location,
		quantity,
		price,
		listForSale,
		imageURLs,
		latitude,
		longitude,
		accuracy,
	); err != nil {

		h.render(
			w,
			farmerID,
			fullName,
			err.Error(),
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/storage",
		http.StatusSeeOther,
	)
}

// unlistCrop removes a product from the marketplace.
func (h *Storage) unlistCrop(
	w http.ResponseWriter,
	r *http.Request,
	farmerID int,
	fullName string,
) {
	cropID, err := strconv.Atoi(
		strings.TrimSpace(r.FormValue("crop_id")),
	)

	if err != nil || cropID <= 0 {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid product ID",
		)
		return
	}

	if err := h.crop.UnlistCrop(
		farmerID,
		cropID,
	); err != nil {

		h.render(
			w,
			farmerID,
			fullName,
			err.Error(),
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/storage",
		http.StatusSeeOther,
	)
}

// relistCrop puts a product back on the marketplace.
func (h *Storage) relistCrop(
	w http.ResponseWriter,
	r *http.Request,
	farmerID int,
	fullName string,
) {
	cropID, err := strconv.Atoi(
		strings.TrimSpace(r.FormValue("crop_id")),
	)

	if err != nil || cropID <= 0 {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid product ID",
		)
		return
	}

	if err := h.crop.RelistCrop(
		farmerID,
		cropID,
	); err != nil {

		h.render(
			w,
			farmerID,
			fullName,
			err.Error(),
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/storage",
		http.StatusSeeOther,
	)
}

// deleteCrop permanently removes a product.
func (h *Storage) deleteCrop(
	w http.ResponseWriter,
	r *http.Request,
	farmerID int,
	fullName string,
) {
	cropID, err := strconv.Atoi(
		strings.TrimSpace(r.FormValue("crop_id")),
	)

	if err != nil || cropID <= 0 {
		h.render(
			w,
			farmerID,
			fullName,
			"Invalid product ID",
		)
		return
	}

	if err := h.crop.DeleteCrop(
		farmerID,
		cropID,
	); err != nil {

		h.render(
			w,
			farmerID,
			fullName,
			err.Error(),
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/storage",
		http.StatusSeeOther,
	)
}

// render loads the farmer's products and renders the storage page.
func (h *Storage) render(
	w http.ResponseWriter,
	farmerID int,
	fullName string,
	errMsg string,
) {
	h.renderPage(
		w,
		farmerID,
		fullName,
		errMsg,
		nil,
	)
}

// renderPage loads products and renders storage.html.
func (h *Storage) renderPage(
	w http.ResponseWriter,
	farmerID int,
	fullName string,
	errMsg string,
	editCrop *models.Crop,
) {
	crops, err := h.crop.MyCrops(farmerID)

	if err != nil {
		log.Println(
			"failed to load crops:",
			err,
		)

		http.Error(
			w,
			"Unable to load storage",
			http.StatusInternalServerError,
		)

		return
	}

	data := StoragePageData{
		FullName: fullName,
		Crops:    crops,
		EditCrop: editCrop,
		Error:    errMsg,
	}

	if err := render.RenderTemplates(
		w,
		"storage.html",
		data,
	); err != nil {

		log.Println(
			"storage render error:",
			err,
		)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
	}
}
