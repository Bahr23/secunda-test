package v1

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bahr/secunda/pkg/logger"
)

type RateLimitOptions struct {
	RequestsPerSecond     float64
	Burst                 int
	AuthRequestsPerSecond float64
	AuthBurst             int
	MaxKeys               int
	IdleTTL               time.Duration
}

type bucket struct {
	tokens  float64
	updated time.Time
}

// An instance-local token bucket with bounded memory. Active entries are never
// evicted to admit new identities: rotating keys cannot reset an exhausted bucket.
type limiter struct {
	mu             sync.Mutex
	buckets        map[string]bucket
	rate           float64
	burst, maxKeys int
	idleTTL        time.Duration
	nextSweep      time.Time
	now            func() time.Time
}

func newLimiter(rate float64, burst, maxKeys int, ttl time.Duration, now func() time.Time) *limiter {
	return &limiter{buckets: make(map[string]bucket), rate: rate, burst: burst, maxKeys: maxKeys, idleTTL: ttl, now: now}
}

// allow returns zero when admitted, otherwise the minimum retry delay.
func (l *limiter) allow(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.nextSweep) {
		for key, b := range l.buckets {
			if now.Sub(b.updated) >= l.idleTTL {
				delete(l.buckets, key)
			}
		}
		l.nextSweep = now.Add(min(l.idleTTL, time.Minute))
	}
	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= l.maxKeys {
			return l.nextSweep.Sub(now)
		}
		b = bucket{tokens: float64(l.burst), updated: now}
	}
	b.tokens = math.Min(float64(l.burst), b.tokens+now.Sub(b.updated).Seconds()*l.rate)
	b.updated = now
	if b.tokens >= 1 {
		b.tokens--
		l.buckets[key] = b
		return 0
	}
	l.buckets[key] = b
	return time.Duration(math.Ceil((1 - b.tokens) / l.rate * float64(time.Second)))
}

type requestLimits struct{ ip, user, auth *limiter }

func newRequestLimits(o RateLimitOptions) *requestLimits {
	return &requestLimits{
		ip:   newLimiter(o.RequestsPerSecond, o.Burst, o.MaxKeys, o.IdleTTL, time.Now),
		user: newLimiter(o.RequestsPerSecond, o.Burst, o.MaxKeys, o.IdleTTL, time.Now),
		auth: newLimiter(o.AuthRequestsPerSecond, o.AuthBurst, o.MaxKeys, o.IdleTTL, time.Now),
	}
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return "unknown"
}

func (h *handler) limited(w http.ResponseWriter, r *http.Request, l *limiter, key, scope string) bool {
	delay := l.allow(key)
	if delay == 0 {
		return false
	}
	seconds := max(1, int(math.Ceil(delay.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	logger.FromContext(r.Context(), h.log).WarnContext(r.Context(), "request rate limited",
		slog.String("event", "http.rate_limited"), slog.String("scope", scope), slog.Int("retry_after_seconds", seconds))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "request rate exceeded; retry later")
	return true
}

func (h *handler) limitIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Probe and documentation routes must remain available during throttling.
		if h.limits != nil && strings.HasPrefix(r.URL.Path, "/api/") && h.limited(w, r, h.limits.ip, peerIP(r), "ip") {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) limitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Login and registration share a budget, before bcrypt/database work.
		if h.limits != nil && h.limited(w, r, h.limits.auth, peerIP(r), "auth") {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) limitUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.limits != nil && h.limited(w, r, h.limits.user, strconv.FormatInt(actor(r), 10), "user") {
			return
		}
		next.ServeHTTP(w, r)
	})
}
