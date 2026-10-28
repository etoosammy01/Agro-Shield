package services

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// WelcomeEmailNotifier queues optional notifications. It has no OTP or phone
// verification dependency and never waits for an email provider.
type WelcomeEmailNotifier interface {
	NotifyWelcome(to, name string) error
}

var (
	ErrWelcomeEmailDisabled = errors.New("welcome email requires BREVO_API_KEY and BREVO_FROM_EMAIL")
	ErrWelcomeEmailQueueFull = errors.New("welcome email queue is full")
	ErrWelcomeEmailClosed    = errors.New("welcome email service is shutting down")
)

type welcomeEmailJob struct{ to, name string }

// WelcomeEmailService bounds background work and drains it on graceful
// shutdown. Notifications are best effort; the queue is not persisted.
type WelcomeEmailService struct {
	sender EmailSender
	jobs   chan welcomeEmailJob
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
}

func NewWelcomeEmailService(sender EmailSender) *WelcomeEmailService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &WelcomeEmailService{
		sender: sender, jobs: make(chan welcomeEmailJob, 32),
		done: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
	go s.run()
	return s
}

func (s *WelcomeEmailService) NotifyWelcome(to, name string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return nil // Email remains optional at registration.
	}
	if s.sender == nil {
		return ErrWelcomeEmailDisabled
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrWelcomeEmailClosed
	}
	select {
	case s.jobs <- welcomeEmailJob{to: to, name: strings.TrimSpace(name)}:
		return nil
	default:
		return ErrWelcomeEmailQueueFull
	}
}

func (s *WelcomeEmailService) run() {
	defer close(s.done)
	for {
		select {
		case <-s.ctx.Done():
			return
		case job, ok := <-s.jobs:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
			err := s.sender.SendWelcome(ctx, job.to, job.name)
			cancel()
			if err != nil {
				log.Printf("welcome_email delivery failed: %v", err)
			} else {
				log.Print("welcome_email accepted by Brevo")
			}
		}
	}
}

func (s *WelcomeEmailService) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.jobs)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		s.cancel()
		return nil
	case <-ctx.Done():
		s.cancel()
		return ctx.Err()
	}
}
