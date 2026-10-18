package handlers

import "backend/internal/services"

type Register struct {
	service     *services.AuthService
	emailSender services.EmailSender
}

func NewRegisterHandler(
	service *services.AuthService,
	emailSender services.EmailSender,
) *Register {
	return &Register{
		service:     service,
		emailSender: emailSender,
	}
}