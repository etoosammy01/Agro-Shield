package handlers

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type AIAssistant struct {
	ai *services.AIService
}

func NewAIAssistantHandler(ai *services.AIService) *AIAssistant {
	return &AIAssistant{ai: ai}
}

type AIAssistantPageData struct {
	History []models.Diagnosis
	Result  string
	Error   string
}

const (
	maxImageSize = 10 << 20
	maxAudioSize = 10 << 20
	maxVideoSize = 30 << 20
)

var validAICategories = map[string]bool{
	"Crops": true, "Livestock": true, "Poultry": true, "Goats": true,
	"Fish": true, "Pests/Insects": true, "Plant Problems": true,
	"General Farming Questions": true,
}

func readAIUpload(r *http.Request, field string, maxSize int64, allowedPrefix string) ([]byte, string, error) {
	file, header, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxSize {
		return nil, "", errors.New(field + " is too large")
	}
	detectedMIME := http.DetectContentType(data)
	declaredMIME := strings.ToLower(strings.TrimSpace(header.Header.Get("Content-Type")))
	if separator := strings.IndexByte(declaredMIME, ';'); separator >= 0 {
		declaredMIME = strings.TrimSpace(declaredMIME[:separator])
	}

	mime := detectedMIME
	valid := strings.HasPrefix(detectedMIME, allowedPrefix+"/")
	// Browser-recorded audio often uses WebM or MP4 containers. Go detects
	// those containers as video or generic binary even when they contain only
	// an audio track, so retain the browser's audio MIME after validating it.
	if field == "audio" && strings.HasPrefix(declaredMIME, "audio/") {
		validContainer := strings.HasPrefix(detectedMIME, "audio/") ||
			detectedMIME == "video/webm" || detectedMIME == "video/mp4" ||
			detectedMIME == "application/octet-stream"
		if validContainer {
			valid = true
			mime = declaredMIME
		}
	}
	if !valid {
		return nil, "", errors.New("unsupported " + field + " format")
	}
	return data, mime, nil
}

func (h *AIAssistant) Handler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	switch r.Method {

	case http.MethodGet:
		log.Println("User Visited AI Assistant page")
		h.render(w, farmer.ID, "", "")

	case http.MethodPost:
		// Enforce a real request limit before multipart parsing.
		r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			h.render(
				w,
				farmer.ID,
				"",
				"Couldn't read the submitted information",
			)
			return
		}
		// Clean up temporary multipart files when request finishes
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		// 2. Create the request that will be sent to the AI service
		request := services.AIRequest{
			Category:    r.FormValue("category"),
			Description: r.FormValue("description"),
			Thinking:    r.FormValue("thinking") == "true",
		}
		var err error
		if !validAICategories[request.Category] {
			h.render(w, farmer.ID, "", "Please select a valid farming category")
			return
		}
		if strings.TrimSpace(request.Description) == "" && r.MultipartForm.File["image"] == nil && r.MultipartForm.File["audio"] == nil && r.MultipartForm.File["video"] == nil {
			h.render(w, farmer.ID, "", "Describe the issue or attach media before sending")
			return
		}

		// --------------------------------------------------
		// OPTIONAL IMAGE
		// --------------------------------------------------
		request.Image, request.ImageType, err = readAIUpload(r, "image", maxImageSize, "image")
		if err != nil {
			h.render(w, farmer.ID, "", err.Error())
			return
		}

		// --------------------------------------------------
		// OPTIONAL AUDIO
		// --------------------------------------------------
		request.Audio, request.AudioType, err = readAIUpload(r, "audio", maxAudioSize, "audio")
		if err != nil {
			h.render(w, farmer.ID, "", err.Error())
			return
		}

		// --------------------------------------------------
		// OPTIONAL VIDEO
		// --------------------------------------------------
		request.Video, request.VideoType, err = readAIUpload(r, "video", maxVideoSize, "video")
		if err != nil {
			h.render(w, farmer.ID, "", err.Error())
			return
		}

		// --------------------------------------------------
		// SEND REQUEST TO AI SERVICE
		// --------------------------------------------------
		diagnosis, err := h.ai.Diagnose(
			farmer.ID,
			request,
		)

		if err != nil {
			h.render(
				w,
				farmer.ID,
				"",
				err.Error(),
			)
			return
		}

		greeting := "Good evening"
		hour := time.Now().Hour()
		if hour < 12 {
			greeting = "Good morning"
		} else if hour < 17 {
			greeting = "Good afternoon"
		}
		h.render(
			w,
			farmer.ID,
			greeting+", "+farmer.FullName+". I’m Agro-Shield AI, and I’m ready to help you.\n\n"+diagnosis.Result,
			"",
		)

	default:
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

func (h *AIAssistant) render(
	w http.ResponseWriter,
	farmerID int,
	result string,
	errMsg string,
) {
	historyStart := time.Now()

	log.Println("⏱️ Loading diagnosis history")

	history, err := h.ai.History(farmerID)

	if err != nil {
		log.Println(
			"failed to load diagnosis history:",
			err,
		)
	}

	log.Printf("⏱️ History loading took: %v", time.Since(historyStart))
	// The greeting is added in the template using the authenticated farmer.
	data := AIAssistantPageData{
		History: history,
		Result:  result,
		Error:   errMsg,
	}

	if err := render.RenderTemplates(
		w,
		"ai-assistant.html",
		data,
	); err != nil {
		if clientDisconnected(err) {
			log.Printf("AI assistant client disconnected while rendering: %v", err)
			return
		}
		log.Println("render error", err)
		// A template can fail after writing headers/body. Avoid attempting a
		// second response, which produces a misleading superfluous-header log.
	}
}
