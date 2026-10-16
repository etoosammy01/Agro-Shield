package handlers

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"

	"backend/internal/services"
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

// RegisterPageData is everything register.html needs to render the form,
// including the values to refill after an error and the messages to show.
// Passwords are deliberately never included, so they are never sent back
// to the browser.
type RegisterPageData struct {
	// Error is a general message shown in a banner above the form.
	Error string

	// FieldErrors holds per-field messages, keyed by: first, last, email,
	// phone, community, state, lga, country, password, confirm.
	FieldErrors map[string]string

	// PhoneTaken adds "Log in" / "Forgot password" links under the phone field.
	PhoneTaken bool

	FirstName string
	LastName  string
	Email     string
	Phone     string
	Community string
	State     string
	LGA       string
	Country   string
	Role      string
}

func newRegisterPage() RegisterPageData {
	return RegisterPageData{
		FieldErrors: map[string]string{},
		Country:     "Nigeria",
		Role:        "farmer",
	}
}

// renderRegisterPage writes the registration form with the given HTTP status.
func renderRegisterPage(w http.ResponseWriter, status int, page RegisterPageData) {
	if status != http.StatusOK {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
	}
	if err := render.RenderTemplates(w, "register.html", page); err != nil {
		log.Println("render error", err)
		// Once a non-200 status has been written we can't send a second
		// error response, so only do it for the normal case.
		if status == http.StatusOK {
			http.Error(w, "Internal Server error", http.StatusInternalServerError)
		}
	}
}

// validateRegistration checks the submitted values and records a friendly
// message for each problem in page.FieldErrors.
func validateRegistration(page *RegisterPageData, password, confirm string) {
	if page.FirstName == "" {
		page.FieldErrors["first"] = "Enter your first name."
	}
	if page.LastName == "" {
		page.FieldErrors["last"] = "Enter your last name."
	}
	if page.Email != "" {
		parsed, err := mail.ParseAddress(page.Email)
		if err != nil || parsed.Address != page.Email {
			page.FieldErrors["email"] = "Enter a valid email address."
		}
	}
	if page.Phone == "" {
		page.FieldErrors["phone"] = "Enter your phone number."
	}
	if page.Community == "" {
		page.FieldErrors["community"] = "Enter your community or village."
	}
	if page.State == "" {
		page.FieldErrors["state"] = "Choose your state."
	}
	if page.LGA == "" {
		page.FieldErrors["lga"] = "Choose your LGA."
	}
	if page.Country == "" {
		page.FieldErrors["country"] = "Enter your country."
	}
	switch {
	case password == "":
		page.FieldErrors["password"] = "Create a password."
	case len(password) < 8:
		page.FieldErrors["password"] = "Password must be at least 8 characters."
	}
	if confirm != password {
		page.FieldErrors["confirm"] = "Passwords don't match."
	}
}

func (h *Register) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		log.Println("User Visited Register page")
		renderRegisterPage(w, http.StatusOK, newRegisterPage())

	case http.MethodPost:
		h.submitRegistration(w, r)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Register) submitRegistration(w http.ResponseWriter, r *http.Request) {
	page := newRegisterPage()

	// The form is small, so cap the request body at 1 MB.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	if err := r.ParseForm(); err != nil {
		page.Error = "We couldn't read that form. Please try again."
		renderRegisterPage(w, http.StatusBadRequest, page)
		return
	}

	page.FirstName = strings.TrimSpace(r.FormValue("first-name"))
	page.LastName = strings.TrimSpace(r.FormValue("last-name"))
	page.Email = strings.TrimSpace(r.FormValue("email"))
	page.Phone = strings.TrimSpace(r.FormValue("phone"))
	page.Community = strings.TrimSpace(r.FormValue("community"))
	page.State = strings.TrimSpace(r.FormValue("state"))
	page.LGA = strings.TrimSpace(r.FormValue("lga"))
	page.Country = strings.TrimSpace(r.FormValue("country"))
	if r.FormValue("role") == "buyer" {
		page.Role = "buyer"
	}

	password := r.FormValue("password")
	confirm := r.FormValue("confirm-password")

	validateRegistration(&page, password, confirm)
	if len(page.FieldErrors) > 0 {
		page.Error = "Please fix the highlighted fields and try again."
		renderRegisterPage(w, http.StatusUnprocessableEntity, page)
		return
	}

	// Profile photo and bank details are collected on the mandatory
	// Complete Your Profile step immediately after this, not here —
	// so they're intentionally passed empty.
	farmer, err := h.service.Register(
		page.FirstName,
		page.LastName,
		page.Phone,
		page.Email,
		password,
		page.Community,
		page.LGA,
		page.State,
		page.Country,
		page.Role,
		"",
		"", "", "",
	)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrPhoneTaken):
			log.Println("registration rejected: phone number already registered")
			page.FieldErrors["phone"] = "This phone number is already registered."
			page.PhoneTaken = true
			page.Error = "An account with this phone number already exists."
			renderRegisterPage(w, http.StatusConflict, page)

		case errors.Is(err, services.ErrPasswordTooShort):
			page.FieldErrors["password"] = "Password must be at least 8 characters."
			page.Error = "Please fix the highlighted fields and try again."
			renderRegisterPage(w, http.StatusUnprocessableEntity, page)

		default:
			// Unexpected failure: log the real cause for developers, but
			// never show internal error text to the user.
			log.Printf("registration failed: %v", err)
			page.Error = "Something went wrong on our side. Please try again in a moment."
			renderRegisterPage(w, http.StatusInternalServerError, page)
		}
		return
	}

	log.Println("User registered successfully as", page.Role)

	// Auto-login: the new account lands straight on the mandatory
	// Complete Your Profile step instead of a separate /login visit.
	sessionID := middleware.CreateSession(farmer.ID)
	middleware.SetSessionCookie(w, sessionID)

	http.Redirect(w, r, "/complete-profile", http.StatusSeeOther)
}