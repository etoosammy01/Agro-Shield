package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type AIDiagnosisHistory struct {
	ai *services.AIService
}

func NewAIDiagnosisHistoryHandler(
	ai *services.AIService,
) *AIDiagnosisHistory {

	return &AIDiagnosisHistory{
		ai: ai,
	}
}

type AIDiagnosisHistoryPageData struct {
	History []models.Diagnosis
}

func (h *AIDiagnosisHistory) Handler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {

		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)

		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		id, err := strconv.Atoi(r.FormValue("id"))
		if err != nil || id <= 0 {
			http.Error(w, "Invalid diagnosis", http.StatusBadRequest)
			return
		}
		if err := h.ai.DeleteDiagnosis(farmer.ID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Diagnosis not found", http.StatusNotFound)
				return
			}
			log.Println("failed to delete diagnosis:", err)
			http.Error(w, "Unable to delete diagnosis", http.StatusInternalServerError)
			return
		}
		redirectTo := "/ai-diagnosis-history"
		if r.FormValue("return_to") == "/ai-assistant" {
			redirectTo = "/ai-assistant"
		}
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
		return
	}

	if r.Method != http.MethodGet {

		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)

		return
	}

	log.Println("User visited AI diagnosis history")

	history, err := h.ai.History(farmer.ID)

	if err != nil {

		log.Println(
			"failed to load diagnosis history:",
			err,
		)

		http.Error(
			w,
			"Unable to load diagnosis history",
			http.StatusInternalServerError,
		)

		return
	}

	data := AIDiagnosisHistoryPageData{
		History: history,
	}

	if err := render.RenderTemplates(
		w,
		"ai-diagnosis-history.html",
		data,
	); err != nil {

		log.Println(
			"render error:",
			err,
		)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)

		return
	}

}
