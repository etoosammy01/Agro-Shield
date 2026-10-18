package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

// requestIDContextKey keeps the request ID available to handlers without
// putting it in a query string or a response body.
type requestIDContextKey struct{}

var trustedProxyNets atomic.Value // []*net.IPNet

// SetTrustedProxies records the networks whose forwarded headers may be
// trusted. An empty list disables forwarded-header trust. The request's
// immediate peer must belong to one of these networks, so a client can only
// choose its own X-Forwarded-For value when it is itself a configured proxy.
func SetTrustedProxies(nets []*net.IPNet) {
	if len(nets) == 0 {
		trustedProxyNets.Store([]*net.IPNet(nil))
		return
	}
	copied := make([]*net.IPNet, len(nets))
	copy(copied, nets)
	trustedProxyNets.Store(copied)
}

func forwardedHeadersTrusted(r *http.Request) bool {
	peer := remoteIP(r)
	if peer == nil {
		return false
	}
	return isTrustedProxyIP(peer)
}

func isTrustedProxyIP(ip net.IP) bool {
	value := trustedProxyNets.Load()
	if value == nil {
		return false
	}
	nets, _ := value.([]*net.IPNet)
	for _, network := range nets {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	return net.ParseIP(host)
}

// RequestIDFromContext returns the ID attached by RequestLogger.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

// RequestLogger assigns a request ID, logs one structured line per request,
// and never logs the query string (which can contain payment references).
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, id)
		r = r.WithContext(ctx)

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf(
			"http_request method=%s path=%q status=%d duration_ms=%d request_id=%s remote=%s",
			r.Method,
			r.URL.Path,
			status,
			time.Since(start).Milliseconds(),
			id,
			ClientIP(r),
		)
	})
}

// Recoverer converts an unexpected panic into a 500 instead of dropping the
// connection and leaving no diagnostic trail. It never writes the panic value
// to the client.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf(
					"panic recovered request_id=%s path=%q value=%q stack=%q",
					RequestIDFromContext(r.Context()),
					r.URL.Path,
					truncate(strings.TrimSpace(strings.ReplaceAll(toText(recovered), "\n", " ")), 300),
					truncate(string(debug.Stack()), 4000),
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ClientIP returns the address used for rate limiting. X-Forwarded-For is
// ignored unless SetTrustProxyHeaders(true) was called explicitly.
func ClientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" && forwardedHeadersTrusted(r) {
		// Walk from the right, skipping hops that are themselves trusted
		// proxies. This works whether the proxy overwrites the header or
		// appends to it, and never returns a value the client placed to the
		// left of the last trusted hop.
		parts := strings.Split(forwarded, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil || isTrustedProxyIP(ip) {
				continue
			}
			return ip.String()
		}
	}
	if ip := remoteIP(r); ip != nil {
		return ip.String()
	}
	return strings.TrimSpace(r.RemoteAddr)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "req-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(buf)
}

func sanitizeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if b.Len() >= 64 {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func toText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return "non-string panic value"
	}
}
