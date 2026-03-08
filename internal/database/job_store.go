package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arman/docpulse/internal/domain"
)

type JobStore struct {
	db *pgxpool.Pool
}

func NewJobStore(db *pgxpool.Pool) *JobStore {
	return &JobStore{db: db}
}

func (s *JobStore) Create(ctx context.Context, job *domain.Job) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO jobs (id, tenant_id, status, document_url, document_format, document_size_bytes, schema, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		job.ID, job.TenantID, job.Status, job.DocumentURL, job.DocumentFormat,
		job.DocumentSizeBytes, job.Schema.Raw, job.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("creating job: %w", err)
	}
	return nil
}

func (s *JobStore) Get(ctx context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, status, document_url, document_format, document_size_bytes,
		       schema, result, confidence_scores, model_used, cost_usd, error_message,
		       created_at, completed_at
		FROM jobs
		WHERE id = $1 AND tenant_id = $2`, jobID, tenantID)

	return scanJob(row)
}

func (s *JobStore) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.Job, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, status, document_url, document_format, document_size_bytes,
		       schema, result, confidence_scores, model_used, cost_usd, error_message,
		       created_at, completed_at
		FROM jobs
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

// ClaimNext claims the next pending job using FOR UPDATE SKIP LOCKED.
// Returns nil, nil if no jobs are pending.
func (s *JobStore) ClaimNext(ctx context.Context) (*domain.Job, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		SELECT id, tenant_id, status, document_url, document_format, document_size_bytes,
		       schema, result, confidence_scores, model_used, cost_usd, error_message,
		       created_at, completed_at
		FROM jobs
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`)

	job, err := scanJob(row)
	if err != nil {
		return nil, nil // no rows
	}

	_, err = tx.Exec(ctx, `UPDATE jobs SET status = 'ingesting' WHERE id = $1`, job.ID)
	if err != nil {
		return nil, fmt.Errorf("claiming job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing claim: %w", err)
	}

	job.Status = domain.JobStatusIngesting
	return job, nil
}

func (s *JobStore) UpdateStatus(ctx context.Context, jobID uuid.UUID, status domain.JobStatus) {
	s.db.Exec(ctx, `UPDATE jobs SET status = $1 WHERE id = $2`, status, jobID)
}

func (s *JobStore) Fail(ctx context.Context, jobID uuid.UUID, msg string) {
	s.db.Exec(ctx, `UPDATE jobs SET status = 'failed', error_message = $1 WHERE id = $2`, msg, jobID)
}

func (s *JobStore) Complete(ctx context.Context, jobID uuid.UUID, result json.RawMessage, confidence map[string]float64, model domain.ModelTier, cost float64) error {
	confJSON, err := json.Marshal(confidence)
	if err != nil {
		return fmt.Errorf("marshaling confidence: %w", err)
	}
	now := time.Now()
	_, err = s.db.Exec(ctx, `
		UPDATE jobs
		SET status = 'completed', result = $1, confidence_scores = $2,
		    model_used = $3, cost_usd = $4, completed_at = $5
		WHERE id = $6`,
		result, confJSON, model, cost, now, jobID,
	)
	if err != nil {
		return fmt.Errorf("completing job: %w", err)
	}
	return nil
}

// scanJob scans a single row into a Job. Works with both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (*domain.Job, error) {
	var j domain.Job
	var schemaRaw, resultRaw, confRaw []byte
	var modelUsed, docFormat, docURL, errMsg *string
	var completedAt *time.Time

	err := row.Scan(
		&j.ID, &j.TenantID, &j.Status, &docURL, &docFormat, &j.DocumentSizeBytes,
		&schemaRaw, &resultRaw, &confRaw, &modelUsed, &j.CostUSD, &errMsg,
		&j.CreatedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}

	j.Schema.Raw = schemaRaw
	j.Result = resultRaw
	if docURL != nil {
		j.DocumentURL = *docURL
	}
	if docFormat != nil {
		j.DocumentFormat = domain.DocumentFormat(*docFormat)
	}
	if modelUsed != nil {
		j.ModelUsed = domain.ModelTier(*modelUsed)
	}
	if errMsg != nil {
		j.ErrorMessage = *errMsg
	}
	j.CompletedAt = completedAt

	if confRaw != nil {
		json.Unmarshal(confRaw, &j.ConfidenceScores)
	}

	return &j, nil
}
