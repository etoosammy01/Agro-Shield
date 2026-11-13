package handlers

import (
	"backend/render"
	"log"
	"net/http"
)

type AgroDash struct {
	Name string
}
type HomePage struct {
	Register string
	Login    string
	Home     string
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		log.Println("User Visited Home Page")
		if err := render.RenderTemplates(w, "index.html", nil); err != nil {
			log.Println("render error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

func OfflineHandler(w http.ResponseWriter, r *http.Request) error {
	return render.RenderTemplates(w, "offline.html", nil)
}
