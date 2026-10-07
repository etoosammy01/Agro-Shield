package handlers

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"backend/internal/services"
	"backend/render"
)

type LocationDirectory struct {
	locations *services.NigeriaLocationService
}

func NewLocationDirectory(locations *services.NigeriaLocationService) *LocationDirectory {
	return &LocationDirectory{locations: locations}
}

func (h *LocationDirectory) States(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	states, err := h.locations.States()
	if err != nil {
		log.Printf("load Nigerian state directory: %v", err)
		http.Error(w, "State options are temporarily unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	render.JSON(w, http.StatusOK, states)
}

func (h *LocationDirectory) LGAs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	lgas, err := h.locations.LGAs(r.URL.Query().Get("state"))
	if err != nil {
		if strings.TrimSpace(r.URL.Query().Get("state")) == "" {
			http.Error(w, "A state is required", http.StatusBadRequest)
			return
		}
		if errors.Is(err, services.ErrUnknownNigeriaState) {
			http.Error(w, "The selected state is not in the Nigerian state directory", http.StatusBadRequest)
			return
		}
		log.Printf("load Nigerian LGA directory: %v", err)
		http.Error(w, "Local government options are temporarily unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	render.JSON(w, http.StatusOK, lgas)
}
