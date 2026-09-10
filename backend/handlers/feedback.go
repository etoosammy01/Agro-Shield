package handlers

import (
	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"encoding/json"
	"net/http"
)

type FeedbackHandler struct {
	service *services.FeedbackService
}

func NewFeedbackHandler(service *services.FeedbackService) *FeedbackHandler {
	return &FeedbackHandler{
		service: service,
	}
}

func (h *FeedbackHandler) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var feedback models.Feedback

	if err := json.NewDecoder(r.Body).Decode(&feedback); err != nil {
		http.Error(w, "Invalid feedback", http.StatusBadRequest)
		return
	}

	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	feedback.UserID = farmer.ID

	if err := h.service.CreateFeedback(&feedback); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Feedback submitted successfully",
	})
}
