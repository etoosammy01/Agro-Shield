package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// bodyLimitForPath returns the maximum request body the application will read
// for a path. The limits are deliberately above the handler-level limits so
// existing validation still produces the more specific error message, while
// still preventing an unbounded multipart or JSON body.
func bodyLimitForPath(path string) int64 {
	switch {
	case strings.HasPrefix(path, "/ai-assistant"):
		return 55 << 20 // 50 MB upload + multipart overhead
	case strings.HasPrefix(path, "/storage"):
		return 35 << 20 // several 10 MB crop images plus form fields
	case path == "/complete-profile":
		return 12 << 20
	case path == "/profile" || strings.HasPrefix(path, "/profile/"):
		return 12 << 20
	case path == "/register":
		return 1 << 20
	case path == "/verify-phone":
		return 16 << 10
	case path == "/orders/pay":
		return 1 << 20
	case path == "/wallet/virtual-account":
		// A BVN/NIN and a consent checkbox; nothing here needs a large body.
		return 16 << 10
	case strings.HasPrefix(path, "/webhooks/"):
		return 1 << 20
	case path == "/chat/typing":
		return 4 << 10
	case path == "/learning/chat":
		return 64 << 10
	default:
		return 2 << 20
	}
}

// LimitRequestBody applies a path-specific hard cap to r.Body. It is a
// backstop, not a replacement for handler-level validation.
func LimitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, bodyLimitForPath(r.URL.Path))
		}
		next.ServeHTTP(w, r)
	})
}

type rateRule struct {
	path    string
	methods map[string]bool
	limiter *RateLimiter
}

// Rules are intentionally conservative and only cover endpoints whose abuse
// has a direct cost or account-security impact. Static files and ordinary
// authenticated page reads are not rate-limited.
var rateRules = []rateRule{
	{path: "/login", methods: postOnly(), limiter: NewRateLimiter(20, 30)},
	{path: "/register", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/verify-phone", methods: postOnly(), limiter: NewRateLimiter(5, 10)},
	{path: "/forgot-password", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/learning/chat", methods: postOnly(), limiter: NewRateLimiter(20, 30)},
	{path: "/ai-assistant", methods: postOnly(), limiter: NewRateLimiter(6, 10)},
	{path: "/wallet/lookup-account", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/wallet/otp/send", methods: postOnly(), limiter: NewRateLimiter(5, 10)},
	{path: "/wallet/deposit", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/wallet/virtual-account", methods: postOnly(), limiter: NewRateLimiter(5, 10)},
	{path: "/wallet/virtual-account/check", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/wallet/withdraw", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/wallet/transfer", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/orders/pay", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/orders/place", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/marketplace", methods: postOnly(), limiter: NewRateLimiter(20, 30)},
	{path: "/chat/send", methods: postOnly(), limiter: NewRateLimiter(30, 60)},
	{path: "/chat/typing", methods: allMethods(), limiter: NewRateLimiter(600, 800)},
	{path: "/chat/private", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/chat/group/create", methods: postOnly(), limiter: NewRateLimiter(10, 20)},
	{path: "/ussd", methods: allMethods(), limiter: NewRateLimiter(120, 200)},
	{path: "/locations/", methods: allMethods(), limiter: NewRateLimiter(60, 100)},
}

func postOnly() map[string]bool {
	return map[string]bool{http.MethodPost: true}
}

func allMethods() map[string]bool {
	return map[string]bool{
		http.MethodGet:  true,
		http.MethodPost: true,
	}
}

// RateLimit rejects requests over the configured per-IP budget for the path.
// It returns 429 with Retry-After instead of silently queueing work.
func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rule := rateRuleFor(r.URL.Path, r.Method)
		if rule == nil {
			next.ServeHTTP(w, r)
			return
		}
		allowed, retryAfter := rule.limiter.Allow(ClientIP(r))
		if !allowed {
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func rateRuleFor(path, method string) *rateRule {
	var best *rateRule
	for i := range rateRules {
		rule := &rateRules[i]
		if !rule.methods[method] {
			continue
		}
		if path == rule.path || (strings.HasSuffix(rule.path, "/") && strings.HasPrefix(path, rule.path)) {
			if best == nil || len(rule.path) > len(best.path) {
				best = rule
			}
		}
	}
	return best
}

// RequestGuard composes the request-level protections in a predictable order:
// cheap headers and rate checks first, then origin validation, then the body
// cap that protects parsers and upload handlers.
func RequestGuard(next http.Handler) http.Handler {
	return SecurityHeaders(RateLimit(CSRFProtect(LimitRequestBody(next))))
}

// RateLimiter is a small per-key token bucket. It is intentionally in-memory:
// it is correct for one application instance and is not a substitute for an
// edge/shared limiter when multiple instances run.
type RateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*rateBucket
	rate      float64 // tokens per second
	burst     float64
	lastSweep time.Time
}

type rateBucket struct {
	tokens   float64
	last     time.Time
	lastSeen time.Time
}

func NewRateLimiter(perMinute, burst int) *RateLimiter {
	if perMinute < 1 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{
		buckets:   make(map[string]*rateBucket),
		rate:      float64(perMinute) / 60,
		burst:     float64(burst),
		lastSweep: time.Now(),
	}
}

func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	if strings.TrimSpace(key) == "" {
		key = "unknown"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) > 5*time.Minute {
		for bucketKey, bucket := range l.buckets {
			if now.Sub(bucket.lastSeen) > 30*time.Minute {
				delete(l.buckets, bucketKey)
			}
		}
		l.lastSweep = now
	}

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &rateBucket{tokens: l.burst, last: now}
		l.buckets[key] = bucket
	}
	elapsed := now.Sub(bucket.last).Seconds()
	if elapsed > 0 {
		bucket.tokens += elapsed * l.rate
		if bucket.tokens > l.burst {
			bucket.tokens = l.burst
		}
	}
	bucket.last = now
	bucket.lastSeen = now

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}
	wait := time.Duration((1-bucket.tokens)/l.rate*float64(time.Second)) + time.Second
	return false, wait
}
