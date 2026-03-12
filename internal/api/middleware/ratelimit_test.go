package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/agxp/docpulse/internal/cache"
	"github.com/agxp/docpulse/internal/domain"
)

func newTestRateCache(t *testing.T) (*cache.RedisCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := cache.NewRedisCache("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	return c, mr
}

func rateLimitHandler(counter rateCounter, limit int) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return RateLimit(counter, limit)(ok)
}

func requestWithTenant(tenantID uuid.UUID) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	tenant := &domain.Tenant{ID: tenantID}
	return r.WithContext(WithTenant(r.Context(), tenant))
}

func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	c, _ := newTestRateCache(t)
	h := rateLimitHandler(c, 5)

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestWithTenant(uuid.New()))
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimit_BlocksOverLimit(t *testing.T) {
	c, _ := newTestRateCache(t)
	h := rateLimitHandler(c, 3)
	tenantID := uuid.New()

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestWithTenant(tenantID))
		if w.Code != http.StatusOK {
			t.Errorf("request %d under limit should be allowed, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithTenant(tenantID))
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("4th request over limit: expected 429, got %d", w.Code)
	}
}

func TestRateLimit_TenantsHaveSeparateBuckets(t *testing.T) {
	c, _ := newTestRateCache(t)
	h := rateLimitHandler(c, 2)

	tenantA := uuid.New()
	tenantB := uuid.New()

	// Exhaust tenant A
	h.ServeHTTP(httptest.NewRecorder(), requestWithTenant(tenantA))
	h.ServeHTTP(httptest.NewRecorder(), requestWithTenant(tenantA))
	wA := httptest.NewRecorder()
	h.ServeHTTP(wA, requestWithTenant(tenantA))
	if wA.Code != http.StatusTooManyRequests {
		t.Errorf("tenant A: expected 429, got %d", wA.Code)
	}

	// Tenant B should be unaffected
	wB := httptest.NewRecorder()
	h.ServeHTTP(wB, requestWithTenant(tenantB))
	if wB.Code != http.StatusOK {
		t.Errorf("tenant B should not be affected by tenant A's limit, got %d", wB.Code)
	}
}

func TestRateLimit_NoTenantPassesThrough(t *testing.T) {
	c, _ := newTestRateCache(t)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := RateLimit(c, 1)(ok)

	// Request without a tenant in context (e.g. /health)
	r := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("no-tenant request should pass through, got %d", w.Code)
	}
}

func TestRateLimit_RedisFailurePassesThrough(t *testing.T) {
	mr := miniredis.RunT(t)
	c, _ := cache.NewRedisCache("redis://" + mr.Addr())
	h := rateLimitHandler(c, 2)
	tenantID := uuid.New()

	mr.Close() // simulate Redis going down

	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithTenant(tenantID))
	if w.Code != http.StatusOK {
		t.Errorf("Redis failure should allow traffic through, got %d", w.Code)
	}
}

func TestRateLimit_ResponseBodyOnBlock(t *testing.T) {
	c, _ := newTestRateCache(t)
	h := rateLimitHandler(c, 1)
	tenantID := uuid.New()

	h.ServeHTTP(httptest.NewRecorder(), requestWithTenant(tenantID)) // consume limit

	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithTenant(tenantID))

	body := w.Body.String()
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if body == "" {
		t.Error("expected non-empty error body on rate limit response")
	}
}

// Compile-time check: *cache.RedisCache satisfies rateCounter.
var _ rateCounter = (*cache.RedisCache)(nil)

// Compile-time check: *redis.Client does NOT satisfy rateCounter (sanity).
var _ = (*redis.Client)(nil)
