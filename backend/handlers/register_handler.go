package handlers

import "backend/internal/services"

type Register struct {
	service      *services.AuthService
	sessions     *services.SessionService
	welcomeEmail services.WelcomeEmailNotifier
}

func NewRegisterHandler(
	service *services.AuthService,
	sessions *services.SessionService,
	welcomeEmail services.WelcomeEmailNotifier,
) *Register {
	return &Register{
		service:      service,
		sessions:     sessions,
		welcomeEmail: welcomeEmail,
	}
}
