package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/agxp/docpulse/internal/auth"
	"github.com/agxp/docpulse/internal/database"
	"github.com/agxp/docpulse/internal/domain"
)

type contextKey string

const tenantKey contextKey = "tenant"

// Auth validates the Bearer API key and injects the resolved Tenant into the request context.
func Auth(tenants *database.TenantStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := r.Header.Get("Authorization")
			if !strings.HasPrefix(bearer, "Bearer ") {
				http.Error(w, `{"error":"missing or malformed Authorization header","code":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			rawKey := strings.TrimPrefix(bearer, "Bearer ")
			keyHash := auth.HashAPIKey(rawKey)

			tenant, err := tenants.GetByAPIKeyHash(r.Context(), keyHash)
			if err != nil || tenant == nil {
				http.Error(w, `{"error":"invalid API key","code":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), tenantKey, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantFromContext retrieves the Tenant injected by the Auth middleware.
func TenantFromContext(ctx context.Context) *domain.Tenant {
	t, _ := ctx.Value(tenantKey).(*domain.Tenant)
	return t
}
