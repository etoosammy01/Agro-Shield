
package services

import (
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

type EmailSender interface {
	SendWelcome(to, name string) error
}

type BrevoEmailSender struct {
	host      string
	login     string
	key       string
	fromEmail string
	fromName  string
}

func NewBrevoEmailSender(
	host, login, key, fromEmail, fromName string,
) *BrevoEmailSender {
	return &BrevoEmailSender{
		host:      strings.TrimSpace(host),
		login:     strings.TrimSpace(login),
		key:       strings.TrimSpace(key),
		fromEmail: strings.TrimSpace(fromEmail),
		fromName:  strings.TrimSpace(fromName),
	}
}

func (s *BrevoEmailSender) SendWelcome(to, name string) error {
	to = strings.TrimSpace(to)
	name = strings.TrimSpace(name)

	if s.host == "" || s.login == "" || s.key == "" ||
		s.fromEmail == "" {
		return fmt.Errorf("Brevo SMTP configuration is incomplete")
	}

	if to == "" || strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("recipient email is invalid")
	}

	fromName := s.fromName
	if fromName == "" {
		fromName = "Agro-Shield"
	}

	subject := "Welcome to Agro-Shield"
	body := fmt.Sprintf(
		"Hello %s,\n\n"+
			"Welcome to Agro-Shield!\n\n"+
			"Your account has been created successfully. "+
			"Please complete your profile to get started.\n\n"+
			"Thank you for joining us.\n\n"+
			"The Agro-Shield Team\n",
		name,
	)

	message := fmt.Sprintf(
		"From: %s <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=UTF-8\r\n"+
			"\r\n%s",
		fromName, s.fromEmail, to, subject, body,
	)

	address := net.JoinHostPort(s.host, "587")
	auth := smtp.PlainAuth("", s.login, s.key, s.host)

	return smtp.SendMail(
		address,
		auth,
		s.fromEmail,
		[]string{to},
		[]byte(message),
	)
}
