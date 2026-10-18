package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// SecurityHeaders adds conservative browser protections. The CSP deliberately
// does not restrict script or style sources yet: the templates still contain
// inline scripts/styles and an external Font Awesome stylesheet. Tightening
// those sources without a compatibility pass would break existing pages.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		headers.Set("Permissions-Policy", "geolocation=(self), microphone=(self), camera=(self)")
		headers.Set("Content-Security-Policy",
			"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'")
		if RequestIsHTTPS(r) {
			headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// CSRFProtect rejects unsafe cross-origin form/fetch requests. SameSite=Lax
// already blocks the common cross-site POST case, but it is not a complete
// CSRF defense, so this adds an Origin/Referer check.
//
// Flutterwave webhooks and USSD gateway callbacks are server-to-server and
// carry their own authentication/signature mechanisms, so they are exempt.
// The public learning chat is unauthenticated and stateless; it is exempted
// because third-party clients may call it without browser Origin headers.
func CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) || csrfExemptPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		source := strings.TrimSpace(r.Header.Get("Origin"))
		if source == "" {
			source = strings.TrimSpace(r.Header.Get("Referer"))
		}
		if source == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		parsed, err := url.Parse(source)
		if err != nil || parsed.Host == "" || !sameRequestHost(parsed.Host, r.Host) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func csrfExemptPath(path string) bool {
	if path == "/webhooks/flutterwave" || strings.HasPrefix(path, "/webhooks/") {
		return true
	}
	if path == "/ussd" {
		return true
	}
	if path == "/learning/chat" {
		return true
	}
	return false
}

func sameRequestHost(originHost, requestHost string) bool {
	originHost = strings.ToLower(strings.TrimSpace(originHost))
	requestHost = strings.ToLower(strings.TrimSpace(requestHost))
	if originHost == "" || requestHost == "" {
		return false
	}
	return originHost == requestHost
}

// RequestIsHTTPS reports whether the request reached the application over
// HTTPS, including a forwarded scheme only when the immediate peer is a
// configured trusted proxy.
func RequestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return forwardedHeadersTrusted(r) &&
		strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
