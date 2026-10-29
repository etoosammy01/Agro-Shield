package config

import "testing"

func setValidTestEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("GROQ_API_KEY", "test-key")
	t.Setenv("PAYMENT_REDIRECT_URL", "https://example.test/payment/callback")
	t.Setenv("OTPDEV_API_KEY", "")
	t.Setenv("OTP_DEV_API_KEY", "")
	t.Setenv("BREVO_API_KEY", "")
	t.Setenv("BREVO_FROM_EMAIL", "")
	t.Setenv("BREVO_FROM_NAME", "")
	t.Setenv("TRUST_PROXY_HEADERS", "")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
}

func TestLoadDefaultsSMSProviderToLog(t *testing.T) {
	setValidTestEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SMSProvider != "log" {
		t.Fatalf("SMSProvider = %q, want log", cfg.SMSProvider)
	}
}

func TestLoadUsesBuiltInOTPSettingsWithAPIKey(t *testing.T) {
	setValidTestEnvironment(t)
	t.Setenv("OTPDEV_API_KEY", " test-key ")
	// Stale settings from older deployments must not override the built-ins
	// or disable SMS when the API key is present.
	t.Setenv("SMS_PROVIDER", "log")
	t.Setenv("OTPDEV_SENDER", " MyFarm ")
	t.Setenv("OTPDEV_TEMPLATE_ID", " account-template ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMSProvider != "otpdev" || cfg.OTPDevAPIKey != "test-key" || cfg.OTPDevSender != "OTP Dev" || cfg.OTPDevTemplateID != "a724614a-3a7c-4625-930e-2a8b45141474" || cfg.OTPDevBaseURL != "https://api.otp.dev" {
		t.Fatalf("incorrect OTP.dev configuration: provider=%q sender=%q template=%q base=%q", cfg.SMSProvider, cfg.OTPDevSender, cfg.OTPDevTemplateID, cfg.OTPDevBaseURL)
	}
}

func TestLoadAcceptsLegacyOTPDevAPIKey(t *testing.T) {
	setValidTestEnvironment(t)
	t.Setenv("OTP_DEV_API_KEY", "legacy-key")
	cfg, err := Load()
	if err != nil || cfg.SMSProvider != "otpdev" || cfg.OTPDevAPIKey != "legacy-key" {
		t.Fatalf("legacy API key was not accepted: %v", err)
	}
}

func TestLoadRequiresRealSMSInProduction(t *testing.T) {
	setValidTestEnvironment(t)
	t.Setenv("APP_ENV", "production")
	for _, transport := range []string{"", "log", "otpdev"} {
		t.Setenv("SMS_PROVIDER", transport)
		if _, err := Load(); err == nil {
			t.Fatal("production allowed OTP codes delivered only to a log")
		}
	}
	t.Setenv("SMS_PROVIDER", "log")
	t.Setenv("OTPDEV_API_KEY", "production-key")
	if cfg, err := Load(); err != nil || cfg.SMSProvider != "otpdev" {
		t.Fatalf("production OTP.dev configuration was rejected: %v", err)
	}
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
