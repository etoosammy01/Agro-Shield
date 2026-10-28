package services

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type emailDeliveryFunc func(context.Context, string, string) error

func (f emailDeliveryFunc) SendWelcome(ctx context.Context, to, name string) error {
	return f(ctx, to, name)
}

func testWelcomeEmailService(t *testing.T, sender EmailSender) *WelcomeEmailService {
	t.Helper()
	s := NewWelcomeEmailService(sender)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	return s
}

func TestWelcomeEmailQueuesWithoutWaitingForDelivery(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var deliveries atomic.Int32
	s := testWelcomeEmailService(t, emailDeliveryFunc(func(ctx context.Context, _, _ string) error {
		if deliveries.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}))
	defer close(release)
	if err := s.NotifyWelcome("member@example.test", "Member"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("email worker did not start")
	}
	result := make(chan error, 1)
	go func() { result <- s.NotifyWelcome("second@example.test", "Second") }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("welcome email blocked registration while the provider was waiting")
	}
	for i := 1; i < cap(s.jobs); i++ {
		if err := s.NotifyWelcome("queued@example.test", "Member"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.NotifyWelcome("overflow@example.test", "Member"); !errors.Is(err, ErrWelcomeEmailQueueFull) {
		t.Fatalf("full queue error=%v", err)
	}
}

func TestWelcomeEmailFailureDoesNotStopLaterNotifications(t *testing.T) {
	var deliveries atomic.Int32
	s := testWelcomeEmailService(t, emailDeliveryFunc(func(context.Context, string, string) error {
		if deliveries.Add(1) == 1 {
			return errors.New("provider unavailable")
		}
		return nil
	}))
	for i := 0; i < 2; i++ {
		if err := s.NotifyWelcome("member@example.test", "Member"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if deliveries.Load() != 2 {
		t.Fatal("email failure stopped the worker or shutdown did not drain the queue")
	}
	if err := s.NotifyWelcome("member@example.test", "Member"); !errors.Is(err, ErrWelcomeEmailClosed) {
		t.Fatalf("notification accepted after shutdown: %v", err)
	}
}

func TestWelcomeEmailShutdownCancelsProviderRequest(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	s := testWelcomeEmailService(t, emailDeliveryFunc(func(ctx context.Context, _, _ string) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}))
	if err := s.NotifyWelcome("member@example.test", "Member"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("email worker did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown error=%v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the email provider request")
	}
}

func TestWelcomeEmailIsOptional(t *testing.T) {
	s := testWelcomeEmailService(t, nil)
	if err := s.NotifyWelcome("", "Member"); err != nil {
		t.Fatal("registration without email was rejected")
	}
	if err := s.NotifyWelcome("member@example.test", "Member"); !errors.Is(err, ErrWelcomeEmailDisabled) {
		t.Fatalf("missing email configuration error=%v", err)
	}
}
