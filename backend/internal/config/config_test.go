package config

import "testing"

func setValidTestEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("GROQ_API_KEY", "test-key")
	t.Setenv("PAYMENT_REDIRECT_URL", "https://example.test/payment/callback")
	t.Setenv("SMS_PROVIDER", "log")
	t.Setenv("TRUST_PROXY_HEADERS", "")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
}

func TestLoadRejectsForwardedHeadersWithoutTrustedProxyNetworks(t *testing.T) {
	setValidTestEnvironment(t)
	t.Setenv("TRUST_PROXY_HEADERS", "true")

	if _, err := Load(); err == nil {
		t.Fatal("expected startup to reject trust without TRUSTED_PROXY_CIDRS")
	}
}

func TestLoadRejectsTrustedProxyNetworksWithoutExplicitTrust(t *testing.T) {
	setValidTestEnvironment(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")

	if _, err := Load(); err == nil {
		t.Fatal("expected startup to reject TRUSTED_PROXY_CIDRS without TRUST_PROXY_HEADERS=true")
	}
}

func TestLoadRejectsInvalidTrustedProxyNetwork(t *testing.T) {
	setValidTestEnvironment(t)
	t.Setenv("TRUST_PROXY_HEADERS", "true")
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8,not-a-network")

	if _, err := Load(); err == nil {
		t.Fatal("expected startup to reject an invalid proxy CIDR")
	}
}

func TestLoadParsesTrustedProxyNetworksOnlyWhenEnabled(t *testing.T) {
	setValidTestEnvironment(t)
	t.Setenv("TRUST_PROXY_HEADERS", "true")
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 192.168.1.0/24")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.TrustProxyHeaders {
		t.Fatal("expected TrustProxyHeaders to be enabled")
	}
	if len(cfg.TrustedProxyCIDRs) != 2 {
		t.Fatalf("got %d trusted networks, want 2", len(cfg.TrustedProxyCIDRs))
	}
	if got := cfg.TrustedProxyCIDRs[0].String(); got != "10.0.0.0/8" {
		t.Errorf("first network = %q", got)
	}
}

func TestLoadDefaultsToNoForwardedHeaderTrust(t *testing.T) {
	setValidTestEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TrustProxyHeaders || len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("forwarded headers trusted by default: trust=%v nets=%v", cfg.TrustProxyHeaders, cfg.TrustedProxyCIDRs)
	}
}
