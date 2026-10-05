package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"backend/internal/services"
	"backend/render"
)

type Learning struct {
	ai *services.AIService
}

func NewLearningHandler(ai *services.AIService) *Learning {
	return &Learning{ai: ai}
}

type learningChatRequest struct {
	Messages []services.AIChatMessage `json:"messages"`
}

func (h *Learning) Page(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := render.RenderTemplates(w, "learning.html", nil); err != nil {
		log.Printf("learning page render failed: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Learning) Chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request learningChatRequest
	if err := decoder.Decode(&request); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "Request is too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Invalid chat request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid chat request", http.StatusBadRequest)
		return
	}

	answer, err := h.ai.Learn(request.Messages)
	if err != nil {
		if errors.Is(err, services.ErrInvalidLearningChat) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("learning assistant request failed: %v", err)
		http.Error(w, "The AI assistant could not answer just now. Please try again.", http.StatusBadGateway)
		return
	}
	render.JSON(w, http.StatusOK, map[string]string{"answer": answer})
}
