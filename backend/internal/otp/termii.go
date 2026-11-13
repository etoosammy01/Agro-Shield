package otp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type TermiiConfig struct {
	APIKey   string
	SenderID string
	BaseURL  string
	Channel  string
}

type TermiiProvider struct {
	apiKey   string
	senderID string
	baseURL  string
	channel  string
	client   *http.Client
}

func NewTermiiProvider(cfg TermiiConfig) (*TermiiProvider, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	senderID := strings.TrimSpace(cfg.SenderID)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	channel := strings.TrimSpace(cfg.Channel)
	if channel == "" {
		channel = "dnd"
	}
	if apiKey == "" || senderID == "" || baseURL == "" {
		return nil, errors.New("Termii provider requires API key, sender ID, and base URL")
	}
	return &TermiiProvider{
		apiKey:   apiKey,
		senderID: senderID,
		baseURL:  baseURL,
		channel:  channel,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (p *TermiiProvider) Send(ctx context.Context, phone, code, _ string) error {
	recipient := strings.TrimPrefix(NormalizePhone(phone), "+")
	payload := map[string]string{
		"api_key": p.apiKey,
		"to":      recipient,
		"from":    p.senderID,
		"sms":     fmt.Sprintf("Your Agro-Shield verification code is %s. It expires in 5 minutes.", code),
		"type":    "plain",
		"channel": p.channel,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/sms/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Termii API returned status %d", resp.StatusCode)
	}
	return nil
}

func (p *TermiiProvider) DevelopmentDelivery() bool { return false }
package otp

import (
"bytes"
"context"
"encoding/json"
"errors"
"fmt"
"io"
"net/http"
"strings"
"time"
)

type TermiiConfig struct {
	APIKey   string
	SenderID string
	BaseURL  string
	Channel  string
}

type TermiiProvider struct {
	apiKey   string
	senderID string
	baseURL  string
	channel  string
	client   *http.Client
}

func NewTermiiProvider(cfg TermiiConfig) (*TermiiProvider, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	senderID := strings.TrimSpace(cfg.SenderID)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	channel := strings.TrimSpace(cfg.Channel)
	if channel == "" {
		channel = "dnd"
	}
	if apiKey == "" || senderID == "" || baseURL == "" {
		return nil, errors.New("Termii provider requires API key, sender ID, and base URL")
	}
	return &TermiiProvider{
		apiKey:   apiKey,
		senderID: senderID,
		baseURL:  baseURL,
		channel:  channel,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (p *TermiiProvider) Send(ctx context.Context, phone, code, _ string) error {
	recipient := strings.TrimPrefix(NormalizePhone(phone), "+")
	payload := map[string]string{
		"api_key": p.apiKey,
		"to":      recipient,
		"from":    p.senderID,
		"sms":     fmt.Sprintf("Your Agro-Shield verification code is %s. It expires in 5 minutes.", code),
		"type":    "plain",
		"channel": p.channel,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/sms/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Termii API returned status %d", resp.StatusCode)
	}
	return nil
}

func (p *TermiiProvider) DevelopmentDelivery() bool { return false }
