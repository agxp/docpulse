package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agxp/docpulse/internal/domain"
)

type TenantStore struct {
	db *pgxpool.Pool
}

func NewTenantStore(db *pgxpool.Pool) *TenantStore {
	return &TenantStore{db: db}
}

func (s *TenantStore) GetByAPIKeyHash(ctx context.Context, keyHash string) (*domain.Tenant, error) {
	var t domain.Tenant
	err := s.db.QueryRow(ctx, `
		SELECT id, name, api_key_hash, rate_limit, byte_limit, created_at
		FROM tenants
		WHERE api_key_hash = $1`, keyHash).
		Scan(&t.ID, &t.Name, &t.APIKeyHash, &t.RateLimit, &t.ByteLimit, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("looking up tenant: %w", err)
	}
	return &t, nil
}

func (s *TenantStore) Create(ctx context.Context, t *domain.Tenant) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO tenants (id, name, api_key_hash, rate_limit, byte_limit, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID, t.Name, t.APIKeyHash, t.RateLimit, t.ByteLimit, t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("creating tenant: %w", err)
	}
	return nil
}

// CreateIfNotExists inserts the tenant, silently ignoring conflicts on api_key_hash.
func (s *TenantStore) CreateIfNotExists(ctx context.Context, t *domain.Tenant) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO tenants (id, name, api_key_hash, rate_limit, byte_limit, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (api_key_hash) DO NOTHING`,
		t.ID, t.Name, t.APIKeyHash, t.RateLimit, t.ByteLimit, t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("upserting tenant: %w", err)
	}
	return nil
}
