package handlers

import (
	"log"
	"net/http"

	"backend/middleware"
	"backend/render"
)

type UserReg struct {
	First_Name       string
	Last_Name        string
	Phone            string
	Email            string
	Password         string
	Confirm_Password string
	Role             string
}

func (h *Register) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		log.Println("User Visited Register page")
		if err := render.RenderTemplates(w, "register.html", nil); err != nil {
			log.Println("render error", err)
			http.Error(w, "Internal Server error", http.StatusInternalServerError)
			return
		}
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid registration form", http.StatusBadRequest)
			return
		}

		user := UserReg{
			First_Name:       r.FormValue("first-name"),
			Last_Name:        r.FormValue("last-name"),
			Phone:            r.FormValue("phone"),
			Email:            r.FormValue("email"),
			Password:         r.FormValue("password"),
			Confirm_Password: r.FormValue("confirm-password"),
			Role:             r.FormValue("role"),
		}
		if user.First_Name == "" || user.Last_Name == "" || user.Phone == "" || user.Password == "" || user.Confirm_Password == "" {
			log.Println("user details must not be empty")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		} else if user.Password != user.Confirm_Password {
			log.Println("Password Mismatch")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		community := r.FormValue("community")
		lga := r.FormValue("lga")
		state := r.FormValue("state")
		country := r.FormValue("country")

		// Profile photo and bank details are collected on the mandatory
		// Complete Your Profile step immediately after this, not here —
		// so they're intentionally passed empty.
		farmer, err := h.service.Register(
			user.First_Name,
			user.Last_Name,
			user.Phone,
			user.Email,
			user.Password,
			community,
			lga,
			state,
			country,
			user.Role,
			"",
			"", "", "",
		)

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		log.Println("User registered successfully as", user.Role)

		// Auto-login: the new account lands straight on the mandatory
		// Complete Your Profile step instead of a separate /login visit.
		sessionID := middleware.CreateSession(farmer.ID)
		middleware.SetSessionCookie(w, sessionID)

		http.Redirect(w, r, "/complete-profile", http.StatusSeeOther)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
}
