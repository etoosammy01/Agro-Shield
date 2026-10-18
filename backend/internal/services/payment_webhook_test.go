package services

import (
	"context"
	"errors"
	"testing"
)

// ProcessWebhook's first job is refusing a delivery that cannot be
// authenticated. That check happens before any database or provider call, so it
// can be exercised without either.
func TestProcessWebhookRejectsAnUnauthenticatedDelivery(t *testing.T) {
	service := &PaymentService{fw: clientWithTransport(&fakeTransport{status: 200})}

	for _, hash := range []string{"", "wrong-hash", "test-hash "} {
		if _, err := service.ProcessWebhook(context.Background(),
			[]byte(`{"event":"charge.completed","data":{"id":1}}`), hash); !errors.Is(err, ErrUnauthorizedWebhook) {
			t.Errorf("hash %q error = %v, want ErrUnauthorizedWebhook", hash, err)
		}
	}
}

func TestProcessWebhookRejectsMalformedOrEmptyEventsBeforeProcessing(t *testing.T) {
	service := &PaymentService{fw: clientWithTransport(&fakeTransport{status: 200})}

	for name, body := range map[string]string{
		"malformed":  `{"event":`,
		"no event":   `{"data":{"id":1,"tx_ref":"AGRO-VA-7"}}`,
		"no id":      `{"event":"charge.completed","data":{"tx_ref":"AGRO-VA-7"}}`,
		"zero id":    `{"event":"charge.completed","data":{"id":0}}`,
		"empty body": ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.ProcessWebhook(context.Background(), []byte(body), "test-hash"); err == nil {
				t.Fatal("an unusable webhook was accepted")
			}
		})
	}
}
