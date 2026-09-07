package handlers

import (
	"log"
	"net/http"

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

	Bank_Name      string
	Account_Number string
	Account_Name   string
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
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "Invalid registration form", http.StatusBadRequest)
			return
		}
		photoURL, err := saveUploadedFile(r, "profile_picture", "farmers")
		if err != nil || photoURL == "" {
			http.Error(w, "Profile picture is required", http.StatusBadRequest)
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

			Bank_Name:      r.FormValue("bank-name"),
			Account_Number: r.FormValue("account-number"),
			Account_Name:   r.FormValue("account-name"),
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
		if user.Role == "farmer" {

			if user.Bank_Name == "" ||
				user.Account_Number == "" ||
				user.Account_Name == "" {

				http.Error(
					w,
					"Bank details are required for farmers",
					http.StatusBadRequest,
				)
				return
			}
		}
		location := r.FormValue("location")

		// Uses the service injected into this handler (h.service), not a
		// package-level global — a prior version used an uninitialized
		// global and would have panicked on every registration.
		err = h.service.Register(
			user.First_Name,
			user.Last_Name,
			user.Phone,
			user.Email,
			user.Password,
			location,
			user.Role,
			photoURL,

			user.Bank_Name,
			user.Account_Name,
			user.Account_Number,
		)

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		log.Println("User registered successfully as", user.Role)

		// New users always land on /login first — they're redirected from
		// there into the correct dashboard for their role once they sign in.
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
}
