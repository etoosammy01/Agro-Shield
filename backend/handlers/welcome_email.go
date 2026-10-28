package handlers

import "log"

func (h *Register) notifyWelcomeEmail(farmerID int, email, name string) {
	if email == "" {
		return
	}
	if h.welcomeEmail == nil {
		log.Printf("welcome_email disabled for user %d", farmerID)
		return
	}
	if err := h.welcomeEmail.NotifyWelcome(email, name); err != nil {
		log.Printf("welcome_email could not be queued for user %d: %v", farmerID, err)
		return
	}
	log.Printf("welcome_email queued for user %d", farmerID)
}
