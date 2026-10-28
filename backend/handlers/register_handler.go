package handlers

import "backend/internal/services"

type Register struct {
	service      *services.AuthService
	welcomeEmail services.WelcomeEmailNotifier
}

func NewRegisterHandler(
	service *services.AuthService,
	welcomeEmail services.WelcomeEmailNotifier,
) *Register {
	return &Register{
		service:      service,
		welcomeEmail: welcomeEmail,
	}
}
