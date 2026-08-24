package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

// ============================================================
// NEGOTIATION HANDLER
//
// Responsibility:
// - Display negotiations.
// - Start negotiations.
// - Display negotiation conversations.
// - Send normal chat messages.
// - Send offers.
// - Accept individual offers.
// - Reject individual offers.
//
// IMPORTANT:
//
// Negotiation chat and offers are different.
//
// CHAT:
// - Unlimited.
// - Does not consume rounds.
// - Not affected by negotiation expiry.
//
// OFFERS:
// - Limited by negotiation rounds.
// - Can be accepted/rejected.
// - Affected by negotiation expiry.
// ============================================================

type Negotiation struct {
	service *services.NegotiationService
	events  *repository.MarketEventRepository
}

// ============================================================
// CREATE NEGOTIATION HANDLER
// ============================================================

func NewNegotiationHandler(
	service *services.NegotiationService,
	events *repository.MarketEventRepository,
) *Negotiation {

	return &Negotiation{
		service: service,
		events:  events,
	}
}

// ============================================================
// NEGOTIATION LIST PAGE DATA
// ============================================================

type NegotiationListPageData struct {
	Negotiations []models.Negotiation
	UserID       int
}

// ============================================================
// LIST NEGOTIATIONS
//
// Displays all negotiations involving the logged-in user.
// ============================================================

func (h *Negotiation) ListHandler(
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

	negotiations, err := h.service.MyNegotiations(
		farmer.ID,
	)

	if err != nil {
		log.Println(
			"failed to load negotiations:",
			err,
		)
	}

	data := NegotiationListPageData{
		Negotiations: negotiations,
		UserID:       farmer.ID,
	}

	if err := render.RenderTemplates(
		w,
		"negotiations.html",
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
	}
}

// ============================================================
// START NEGOTIATION
//
// Starts a negotiation from a marketplace listing.
//
// POST /negotiation/start
// ============================================================

func (h *Negotiation) StartHandler(
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
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	// --------------------------------------------------------
	// Get crop ID.
	// --------------------------------------------------------

	cropID, err := strconv.Atoi(
		r.FormValue("crop_id"),
	)

	if err != nil || cropID <= 0 {
		http.Redirect(
			w,
			r,
			"/marketplace",
			http.StatusSeeOther,
		)
		return
	}

	// --------------------------------------------------------
	// Get quantity.
	// --------------------------------------------------------

	quantity, err := strconv.ParseFloat(
		r.FormValue("quantity"),
		64,
	)

	if err != nil || quantity <= 0 {
		http.Redirect(
			w,
			r,
			"/marketplace",
			http.StatusSeeOther,
		)
		return
	}

	// --------------------------------------------------------
	// Get first offer price.
	// --------------------------------------------------------

	price, err := strconv.ParseFloat(
		r.FormValue("offer_price"),
		64,
	)

	if err != nil || price <= 0 {
		http.Redirect(
			w,
			r,
			"/marketplace",
			http.StatusSeeOther,
		)
		return
	}

	message := r.FormValue("message")

	// --------------------------------------------------------
	// Create negotiation.
	// --------------------------------------------------------

	negotiation, err := h.service.StartNegotiation(
		farmer.ID,
		cropID,
		quantity,
		price,
		message,
	)

	if err != nil {

		log.Println(
			"failed to start negotiation:",
			err,
		)

		http.Redirect(
			w,
			r,
			"/marketplace",
			http.StatusSeeOther,
		)

		return
	}
	if h.events != nil {
		uid := farmer.ID
		cid := cropID
		_ = h.events.Record(&models.MarketEvent{EventType: "negotiation_started", CropID: &cid, UserID: &uid, Metadata: fmt.Sprintf(`{"quantity":%g,"offer_price":%g}`, quantity, price)})
	}

	// --------------------------------------------------------
	// Open negotiation conversation.
	// --------------------------------------------------------

	http.Redirect(
		w,
		r,
		"/negotiation?id="+strconv.Itoa(
			negotiation.ID,
		),
		http.StatusSeeOther,
	)
}

// ============================================================
// NEGOTIATION THREAD PAGE DATA
// ============================================================

type NegotiationThreadPageData struct {
	Negotiation *models.Negotiation
	Messages    []models.NegotiationMessage
	UserID      int
	Error       string
	TimeLeft    string
}

// ============================================================
// THREAD HANDLER
//
// GET:
//     Display negotiation.
//
// POST:
//
//     action=chat
//     action=offer
//     action=accept
//     action=reject
// ============================================================

func (h *Negotiation) ThreadHandler(
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

	// --------------------------------------------------------
	// Get negotiation ID.
	// --------------------------------------------------------

	negotiationID, err := strconv.Atoi(
		r.URL.Query().Get("id"),
	)

	if err != nil || negotiationID <= 0 {

		http.Error(
			w,
			"Invalid negotiation ID",
			http.StatusBadRequest,
		)

		return
	}

	// ========================================================
	// HANDLE REQUEST METHOD
	// ========================================================

	switch r.Method {

	// ========================================================
	// GET
	// ========================================================

	case http.MethodGet:

		h.render(
			w,
			negotiationID,
			farmer.ID,
			"",
		)

	// ========================================================
	// POST
	// ========================================================

	case http.MethodPost:

		action := r.FormValue("action")

		switch action {

		// ====================================================
		// NORMAL CHAT MESSAGE
		//
		// Chat does NOT:
		// - consume a negotiation round
		// - use the offer timer
		// - create an offer
		// ====================================================

		case "chat":

			message := r.FormValue("message")

			if message == "" {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					"message cannot be empty",
				)

				return
			}

			err := h.service.SendMessage(
				negotiationID,
				farmer.ID,
				message,
			)

			if err != nil {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					err.Error(),
				)

				return
			}

		// ====================================================
		// SEND OFFER
		// ====================================================

		case "offer":

			price, err := strconv.ParseFloat(
				r.FormValue("offer_price"),
				64,
			)

			if err != nil || price <= 0 {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					"invalid offer price",
				)

				return
			}

			message := r.FormValue("message")

			err = h.service.SendOffer(
				negotiationID,
				farmer.ID,
				price,
				message,
			)

			if err != nil {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					err.Error(),
				)

				return
			}

		// ====================================================
		// ACCEPT OFFER
		//
		// IMPORTANT:
		// offer_id identifies the exact offer being accepted.
		// ====================================================

		case "accept":

			offerID, err := strconv.Atoi(
				r.FormValue("offer_id"),
			)

			if err != nil || offerID <= 0 {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					"invalid offer ID",
				)

				return
			}

			err = h.service.Accept(
				negotiationID,
				offerID,
				farmer.ID,
			)

			if err != nil {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					err.Error(),
				)

				return
			}

		// ====================================================
		// REJECT OFFER
		//
		// Rejecting an offer does NOT close the negotiation.
		// ====================================================

		case "reject":

			offerID, err := strconv.Atoi(
				r.FormValue("offer_id"),
			)

			if err != nil || offerID <= 0 {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					"invalid offer ID",
				)

				return
			}

			err = h.service.Reject(
				negotiationID,
				offerID,
				farmer.ID,
			)

			if err != nil {

				h.render(
					w,
					negotiationID,
					farmer.ID,
					err.Error(),
				)

				return
			}

		// ====================================================
		// UNKNOWN ACTION
		// ====================================================

		default:

			h.render(
				w,
				negotiationID,
				farmer.ID,
				"invalid negotiation action",
			)

			return
		}

		// ----------------------------------------------------
		// After successful POST, return to negotiation page.
		// ----------------------------------------------------

		http.Redirect(
			w,
			r,
			"/negotiation?id="+strconv.Itoa(
				negotiationID,
			),
			http.StatusSeeOther,
		)

	// ========================================================
	// UNSUPPORTED METHOD
	// ========================================================

	default:

		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

// ============================================================
// RENDER NEGOTIATION THREAD
//
// Loads:
//
// - Negotiation information.
// - Complete chat history.
// - Offers.
// - Accepted offers.
// - Rejected offers.
// ============================================================

func (h *Negotiation) render(
	w http.ResponseWriter,
	negotiationID,
	userID int,
	errMsg string,
) {

	negotiation, messages, err := h.service.Thread(
		negotiationID,
	)

	if err != nil || negotiation == nil {

		http.Error(
			w,
			"Negotiation not found",
			http.StatusNotFound,
		)

		return
	}

	// ========================================================
	// CALCULATE TIME LEFT
	//
	// IMPORTANT:
	//
	// This timer is only informational for the negotiation
	// offer window.
	//
	// It does NOT control normal chat.
	// ========================================================

	timeLeft := negotiation.TimeLeft()

	timeLeftStr := "Expired"

	if timeLeft > 0 {

		hours := int(
			timeLeft.Hours(),
		)

		minutes := int(
			timeLeft.Minutes(),
		) % 60

		timeLeftStr =
			strconv.Itoa(hours) +
				"h " +
				strconv.Itoa(minutes) +
				"m left"
	}

	// ========================================================
	// PAGE DATA
	// ========================================================

	data := NegotiationThreadPageData{
		Negotiation: negotiation,
		Messages:    messages,
		UserID:      userID,
		Error:       errMsg,
		TimeLeft:    timeLeftStr,
	}

	// ========================================================
	// RENDER TEMPLATE
	// ========================================================

	if err := render.RenderTemplates(
		w,
		"negotiation.html",
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
	}
}
