package middleware

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func trustProxyCIDR(t *testing.T, cidr string) {
	t.Helper()
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("parse test CIDR: %v", err)
	}
	SetTrustedProxies([]*net.IPNet{network})
	t.Cleanup(func() { SetTrustedProxies(nil) })
}

func TestSecurityHeadersAreApplied(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("CSP missing frame-ancestors: %q", got)
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS set on plain HTTP: %q", got)
	}
}

func TestSecurityHeadersAddHSTSBehindTLS(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("expected HSTS when the request is HTTPS")
	}
}

func TestSecurityHeadersRequireATrustedProxyForForwardedHTTPS(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	untrusted := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	untrusted.RemoteAddr = "198.51.100.20:1234"
	untrusted.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, untrusted)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("untrusted forwarded proto produced HSTS: %q", got)
	}

	trustProxyCIDR(t, "192.0.2.0/24")
	trusted := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	trusted.RemoteAddr = "192.0.2.10:443"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, trusted)
	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("expected HSTS from a trusted proxy")
	}
}

func TestClientIPIgnoresForwardedHeadersByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := ClientIP(req); got != "192.0.2.10" {
		t.Fatalf("ClientIP = %q, want the direct peer", got)
	}
}

func TestClientIPTrustsForwardedHeadersOnlyFromTrustedProxies(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 192.0.2.10")
	req.RemoteAddr = "198.51.100.20:1234"
	if got := ClientIP(req); got != "198.51.100.20" {
		t.Fatalf("untrusted peer spoofed its address: %q", got)
	}

	trustProxyCIDR(t, "192.0.2.0/24")
	req.RemoteAddr = "192.0.2.10:1234"
	if got := ClientIP(req); got != "203.0.113.9" {
		t.Fatalf("trusted proxy should supply the client address, got %q", got)
	}

	// A proxy that appends to a client-supplied header must not let the
	// client's forged leftmost value win.
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")
	if got := ClientIP(req); got != "203.0.113.9" {
		t.Fatalf("leftmost forged value won: %q", got)
	}

	req.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := ClientIP(req); got != "192.0.2.10" {
		t.Fatalf("invalid X-Forwarded-For should fall back to the peer, got %q", got)
	}
}

func TestCSRFProtectAllowsSameOriginUnsafeRequests(t *testing.T) {
	called := false
	h := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "http://example.test/login", nil)
	req.Header.Set("Origin", "http://example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST was blocked: called=%v status=%d", called, rec.Code)
	}
}

func TestCSRFProtectAllowsSafeMethodsWithoutOrigin(t *testing.T) {
	called := false
	h := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if !called {
		t.Fatal("GET should not require an Origin header")
	}
}

func TestCSRFProtectBlocksCrossOriginUnsafeRequests(t *testing.T) {
	for name, setup := range map[string]func(*http.Request){
		"missing origin": func(r *http.Request) {},
		"different host": func(r *http.Request) { r.Header.Set("Origin", "https://evil.test") },
		"fetch metadata": func(r *http.Request) {
			r.Header.Set("Origin", "http://example.test")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		},
		"unparseable origin": func(r *http.Request) { r.Header.Set("Origin", "://bad") },
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			h := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			req := httptest.NewRequest(http.MethodPost, "http://example.test/login", nil)
			setup(req)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if called || rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 and no handler call, got called=%v status=%d", called, rec.Code)
			}
		})
	}
}

func TestCSRFProtectExemptsServerToServerAndPublicEndpoints(t *testing.T) {
	for _, path := range []string{"/webhooks/flutterwave", "/ussd", "/learning/chat"} {
		t.Run(path, func(t *testing.T) {
			called := false
			h := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "http://example.test"+path, nil))
			if !called {
				t.Fatalf("%s should be exempt", path)
			}
		})
	}
}

func TestRateLimiterEnforcesBurstAndSeparatesKeys(t *testing.T) {
	limiter := NewRateLimiter(1, 1)
	if allowed, _ := limiter.Allow("client-a"); !allowed {
		t.Fatal("first request should be allowed")
	}
	allowed, retryAfter := limiter.Allow("client-a")
	if allowed || retryAfter <= 0 {
		t.Fatalf("second request should be limited with a retry delay, got allowed=%v retry=%v", allowed, retryAfter)
	}
	if allowed, _ := limiter.Allow("client-b"); !allowed {
		t.Fatal("a different key should have its own bucket")
	}
	if allowed, _ := limiter.Allow("  "); !allowed {
		t.Fatal("blank keys should fall back to a shared bucket rather than fail closed")
	}
}

func TestRateLimitOnlyAppliesToConfiguredMethods(t *testing.T) {
	limit := func(path, method string) bool {
		called := false
		h := RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}))
		req := httptest.NewRequest(method, "http://example.test"+path, nil)
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return called
	}
	if !limit("/login", http.MethodGet) {
		t.Fatal("GET /login should not be rate limited")
	}
	if !limit("/marketplace", http.MethodPost) {
		t.Fatal("unlisted paths should not be rate limited")
	}
	// Exhaust the POST /login bucket, then verify the next POST is rejected.
	limited := false
	for i := 0; i < 100; i++ {
		if !limit("/login", http.MethodPost) {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("POST /login should be limited after the burst is exhausted")
	}
}

func TestBodyLimitForPath(t *testing.T) {
	tests := []struct {
		path string
		want int64
	}{
		{"/ai-assistant", 55 << 20},
		{"/storage", 35 << 20},
		{"/profile/photo", 12 << 20},
		{"/register", 1 << 20},
		{"/orders/pay", 1 << 20},
		{"/wallet/virtual-account", 16 << 10},
		{"/webhooks/flutterwave", 1 << 20},
		{"/learning/chat", 64 << 10},
		{"/chat/typing", 4 << 10},
		{"/marketplace", 2 << 20},
	}
	for _, tc := range tests {
		if got := bodyLimitForPath(tc.path); got != tc.want {
			t.Errorf("bodyLimitForPath(%q) = %d, want %d", tc.path, got, tc.want)
		}
	}
}

func TestLimitRequestBodyRejectsOversizedBodies(t *testing.T) {
	var readErr error
	h := LimitRequestBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	body := bytes.Repeat([]byte("a"), int(bodyLimitForPath("/learning/chat"))+1)
	req := httptest.NewRequest(http.MethodPost, "/learning/chat", bytes.NewReader(body))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if readErr == nil {
		t.Fatal("expected an oversized body to fail while reading")
	}
}

func TestCompressionCompressesTextResponses(t *testing.T) {
	h := Compression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, strings.Repeat("hello ", 100))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary missing Accept-Encoding: %q", rec.Header().Get("Vary"))
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("response is not valid gzip: %v", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if !strings.HasPrefix(string(decoded), "hello hello") {
		t.Fatalf("unexpected decoded body: %q", string(decoded[:20]))
	}
}

func TestCompressionSkipsNonTextAndHEAD(t *testing.T) {
	t.Run("image", func(t *testing.T) {
		h := Compression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not-really-a-png"))
		}))
		req := httptest.NewRequest(http.MethodGet, "/static/a.png", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("image was compressed: %q", got)
		}
	})

	t.Run("head", func(t *testing.T) {
		h := Compression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
		}))
		req := httptest.NewRequest(http.MethodHead, "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("HEAD response was compressed: %q", got)
		}
		if got := rec.Header().Get("Vary"); got != "" {
			t.Fatalf("HEAD response should bypass compression entirely, got Vary=%q", got)
		}
	})
}

func TestRequestLoggerSetsAndSanitizesRequestID(t *testing.T) {
	var seen string
	h := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/dashboard?payment_ref=secret", nil)
	req.Header.Set("X-Request-ID", "bad id;drop")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "badiddrop" {
		t.Fatalf("context request ID = %q, want badiddrop", seen)
	}
	if got := rec.Header().Get("X-Request-ID"); got != "badiddrop" {
		t.Fatalf("response request ID = %q, want badiddrop", got)
	}
}

func TestRequestLoggerGeneratesAnIDWhenNoneIsSupplied(t *testing.T) {
	h := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) == "" {
			t.Fatal("expected a generated request ID")
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if len(rec.Header().Get("X-Request-ID")) != 32 {
		t.Fatalf("expected a 32-character generated ID, got %q", rec.Header().Get("X-Request-ID"))
	}
}

func TestRecovererReturns500WithoutLeakingThePanic(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("internal sentinel value")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "internal sentinel") {
		t.Fatalf("panic value leaked to the client: %q", rec.Body.String())
	}
}

func TestStaticCacheSetsHeaders(t *testing.T) {
	tests := []struct {
		path   string
		cache  string
		robots string
	}{
		{"/static/pages/index.html", "no-cache", "noindex, nofollow"},
		{"/static/uploads/photo.jpg", "public, max-age=86400", ""},
		{"/static/css/style.css", "public, max-age=3600", ""},
		{"/static/assets/images/Farmer1.jpeg", "public, max-age=86400", ""},
		{"/static/assets/fonts/x.woff2", "public, max-age=2592000", ""},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			h := StaticCache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if got := rec.Header().Get("Cache-Control"); got != tc.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tc.cache)
			}
			if got := rec.Header().Get("X-Robots-Tag"); got != tc.robots {
				t.Errorf("X-Robots-Tag = %q, want %q", got, tc.robots)
			}
		})
	}
}

func TestNoDirectoryListing(t *testing.T) {
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/static/uploads/", http.StatusNotFound},
		{"/static/uploads/a.jpg", http.StatusNoContent},
	} {
		called := false
		h := NoDirectoryListing(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s status = %d, want %d", tc.path, rec.Code, tc.want)
		}
		if tc.want == http.StatusNotFound && called {
			t.Errorf("%s should not reach the file handler", tc.path)
		}
	}
}

// Funding-account setup carries an identity document, and the status check can
// trigger a provider call. Both are POST-only in the route table; the limiter
// backs that up so a script cannot hammer either.
func TestFundingAccountPostsAreRateLimited(t *testing.T) {
	for index, path := range []string{"/wallet/virtual-account", "/wallet/virtual-account/check"} {
		allow := func(method string) bool {
			called := false
			h := RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(method, "http://example.test"+path, nil)
			req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", index+10)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return called
		}

		limited := false
		for i := 0; i < 100; i++ {
			if !allow(http.MethodPost) {
				limited = true
				break
			}
		}
		if !limited {
			t.Errorf("POST %s was never limited", path)
		}
		if !allow(http.MethodGet) {
			t.Errorf("GET %s was rate limited although it is not a configured action", path)
		}
	}
}
