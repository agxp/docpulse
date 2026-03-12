package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// rateCounter is the subset of RedisCache used for rate limiting.
// Defined as an interface so tests can inject a stub.
type rateCounter interface {
	RateIncr(ctx context.Context, key string, window time.Duration) (int64, error)
}

// RateLimit enforces a fixed-window per-tenant request limit (requests per minute).
// Must be placed after Auth so the tenant is available in context.
func RateLimit(counter rateCounter, requestsPerMinute int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenant := TenantFromContext(r.Context())
			if tenant == nil {
				next.ServeHTTP(w, r)
				return
			}

			window := time.Now().Unix() / 60
			key := fmt.Sprintf("rl:%s:%d", tenant.ID, window)

			count, err := counter.RateIncr(r.Context(), key, time.Minute)
			if err != nil {
				// On Redis failure, let the request through rather than block legitimate traffic.
				next.ServeHTTP(w, r)
				return
			}

			if count > int64(requestsPerMinute) {
				http.Error(w,
					`{"error":"rate limit exceeded","code":"rate_limited"}`,
					http.StatusTooManyRequests,
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
