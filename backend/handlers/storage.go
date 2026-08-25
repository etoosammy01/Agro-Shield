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
//
// Responsibility:
// - Display a farmer's products.
// - Receive product management requests.
// - Pass product operations to CropService.
type Storage struct {
	crop *services.CropService
}

// NewStorageHandler creates a new Storage handler.
//
// Responsibility:
// - Connect the handler to CropService.
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
//
// GET:
// - Display the farmer's products.
//
// POST:
// - Create a product.
// - Update a product.
// - Unlist a product.
// - Relist a product.
// - Delete a product.
func (h *Storage) StorageHandler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Buyers don't have produce storage.
	// Send them to the marketplace.
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
				if getErr == nil && candidate != nil && candidate.FarmerID == farmer.ID {
					editCrop = candidate
				}
			}
		}
		h.renderPage(w, farmer.ID, farmer.FullName, "", editCrop)

	case http.MethodPost:
		// Product creation/updates may include an image upload and therefore
		// use multipart/form-data. Listing and deletion forms are ordinary
		// URL-encoded forms; parsing them as multipart rejects valid requests.
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				h.render(w, farmer.ID, farmer.FullName, "Couldn't process the form")
				return
			}
		} else if err := r.ParseForm(); err != nil {
			h.render(w, farmer.ID, farmer.FullName, "Couldn't process the form")
			return
		}

		// Determine which product operation the user requested.
		action := strings.TrimSpace(r.FormValue("action"))

		switch action {

		case "create":
			h.createCrop(w, r, farmer.ID, farmer.FullName)

		case "update":
			h.updateCrop(w, r, farmer.ID, farmer.FullName)

		case "unlist":
			h.unlistCrop(w, r, farmer.ID, farmer.FullName)

		case "relist":
			h.relistCrop(w, r, farmer.ID, farmer.FullName)

		case "delete":
			h.deleteCrop(w, r, farmer.ID, farmer.FullName)

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
//
// Responsibility:
// - Read product information from the form.
// - Validate basic numeric input.
// - Upload the product image.
// - Pass the product to CropService.
func (h *Storage) createCrop(
	w http.ResponseWriter,
	r *http.Request,
	farmerID int,
	fullName string,
) {
	name := strings.TrimSpace(r.FormValue("produce"))
	unit := strings.TrimSpace(r.FormValue("unit"))
	location := strings.TrimSpace(r.FormValue("location"))
	latitude, _ := strconv.ParseFloat(r.FormValue("latitude"), 64)
	longitude, _ := strconv.ParseFloat(r.FormValue("longitude"), 64)
	accuracy, _ := strconv.ParseFloat(r.FormValue("location_accuracy"), 64)

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

	imageURL, err := saveUploadedFile(
		r,
		"produce_image",
		"crops",
	)
	if err != nil {
		log.Println("produce image upload failed:", err)
	}
	if strings.TrimSpace(imageURL) == "" {
		h.render(w, farmerID, fullName, "crop picture is required")
		return
	}

	if err := h.crop.AddCrop(
		farmerID,
		name,
		unit,
		location,
		quantity,
		price,
		listForSale,
		imageURL,
		latitude, longitude, accuracy,
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
//
// Responsibility:
// - Read the product ID.
// - Read the updated product information.
// - Pass the update request to CropService.
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

	// Image upload is optional during an update.
	imageURL := strings.TrimSpace(r.FormValue("image_url"))
	if uploadedURL, uploadErr := saveUploadedFile(r, "produce_image", "crops"); uploadErr != nil {
		log.Println("updated produce image upload failed:", uploadErr)
	} else if strings.TrimSpace(uploadedURL) != "" {
		imageURL = uploadedURL
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
		imageURL,
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
//
// Responsibility:
// - Make the farmer's product unavailable to buyers.
// - Keep the product in the farmer's storage.
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

// relistCrop puts a previously unlisted product
// back on the marketplace.
//
// Responsibility:
// - Make the farmer's product visible to buyers again.
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
//
// Responsibility:
// - Remove the farmer's product when deletion is allowed.
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

// render loads the farmer's products and renders
// the storage page.
//
// Responsibility:
// - Retrieve the farmer's products.
// - Prepare page data.
// - Render storage.html.
func (h *Storage) render(
	w http.ResponseWriter,
	farmerID int,
	fullName string,
	errMsg string,
) {
	h.renderPage(w, farmerID, fullName, errMsg, nil)
}

func (h *Storage) renderPage(w http.ResponseWriter, farmerID int, fullName, errMsg string, editCrop *models.Crop) {
	crops, err := h.crop.MyCrops(farmerID)
	if err != nil {
		log.Println("failed to load crops:", err)

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
		log.Println("storage render error:", err)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
	}
}
