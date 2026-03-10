package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agxp/docpulse/internal/domain"
)

type WebhookStore struct {
	db *pgxpool.Pool
}

func NewWebhookStore(db *pgxpool.Pool) *WebhookStore {
	return &WebhookStore{db: db}
}

func (s *WebhookStore) Create(ctx context.Context, w *domain.Webhook) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO webhooks (id, tenant_id, url, secret, active)
		VALUES ($1, $2, $3, $4, $5)`,
		w.ID, w.TenantID, w.URL, w.Secret, w.Active,
	)
	if err != nil {
		return fmt.Errorf("creating webhook: %w", err)
	}
	return nil
}

func (s *WebhookStore) ListActive(ctx context.Context, tenantID uuid.UUID) ([]domain.Webhook, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, url, secret, active
		FROM webhooks
		WHERE tenant_id = $1 AND active = TRUE`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("listing webhooks: %w", err)
	}
	defer rows.Close()

	var hooks []domain.Webhook
	for rows.Next() {
		var w domain.Webhook
		if err := rows.Scan(&w.ID, &w.TenantID, &w.URL, &w.Secret, &w.Active); err != nil {
			return nil, fmt.Errorf("scanning webhook: %w", err)
		}
		hooks = append(hooks, w)
	}
	return hooks, rows.Err()
}

func (s *WebhookStore) Delete(ctx context.Context, tenantID, webhookID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE webhooks SET active = FALSE
		WHERE id = $1 AND tenant_id = $2`, webhookID, tenantID)
	if err != nil {
		return fmt.Errorf("deleting webhook: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("webhook not found")
	}
	return nil
}
