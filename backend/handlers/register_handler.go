package handlers

import "backend/internal/services"

type Register struct {
	service      *services.AuthService
	welcomeEmail services.WelcomeEmailNotifier
	sessions     *services.SessionService
}

func NewRegisterHandler(
	service *services.AuthService,
	welcomeEmail services.WelcomeEmailNotifier,
	sessions ...*services.SessionService,
) *Register {
	var backend *services.SessionService
	if len(sessions) > 0 {
		backend = sessions[0]
	}
	return &Register{
		service:      service,
		welcomeEmail: welcomeEmail,
		sessions:     backend,
	}
}
