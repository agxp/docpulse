package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/agxp/docpulse/internal/api/middleware"
	"github.com/agxp/docpulse/internal/domain"
)

// --- stubs ---

type stubJobStore struct {
	jobs  []domain.Job
	total int
	err   error
}

func (s *stubJobStore) Create(_ context.Context, _ *domain.Job) error { return s.err }
func (s *stubJobStore) Get(_ context.Context, _, _ uuid.UUID) (*domain.Job, error) {
	if len(s.jobs) == 0 {
		return nil, s.err
	}
	return &s.jobs[0], s.err
}
func (s *stubJobStore) List(_ context.Context, _ uuid.UUID, _, _ int) ([]domain.Job, error) {
	return s.jobs, s.err
}
func (s *stubJobStore) Count(_ context.Context, _ uuid.UUID) (int, error) {
	return s.total, s.err
}

type stubWebhookStore struct{}

func (s *stubWebhookStore) Create(_ context.Context, _ *domain.Webhook) error { return nil }
func (s *stubWebhookStore) Delete(_ context.Context, _, _ uuid.UUID) error    { return nil }

func tenantRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	tenant := &domain.Tenant{ID: uuid.New(), Name: "test"}
	return r.WithContext(middleware.WithTenant(r.Context(), tenant))
}

func newTestHandlers(jobs *stubJobStore) *Handlers {
	return &Handlers{
		jobs:     jobs,
		webhooks: &stubWebhookStore{},
		baseURL:  "http://localhost",
	}
}

// --- HandleListJobs ---

func TestHandleListJobs_IncludesTotal(t *testing.T) {
	store := &stubJobStore{
		jobs:  []domain.Job{{ID: uuid.New(), TenantID: uuid.New(), Status: domain.JobStatusCompleted}},
		total: 42,
	}
	h := newTestHandlers(store)

	w := httptest.NewRecorder()
	h.HandleListJobs(w, tenantRequest(t, "GET", "/v1/jobs"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	total, ok := resp["total"]
	if !ok {
		t.Fatal("response missing 'total' field")
	}
	// JSON numbers decode as float64
	if total.(float64) != 42 {
		t.Errorf("expected total=42, got %v", total)
	}
}

func TestHandleListJobs_TotalZeroWhenEmpty(t *testing.T) {
	store := &stubJobStore{jobs: nil, total: 0}
	h := newTestHandlers(store)

	w := httptest.NewRecorder()
	h.HandleListJobs(w, tenantRequest(t, "GET", "/v1/jobs"))

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["total"].(float64) != 0 {
		t.Errorf("expected total=0 for empty store, got %v", resp["total"])
	}
}

func TestHandleListJobs_IncludesLimitAndOffset(t *testing.T) {
	h := newTestHandlers(&stubJobStore{total: 5})

	r := tenantRequest(t, "GET", "/v1/jobs?limit=10&offset=5")
	w := httptest.NewRecorder()
	h.HandleListJobs(w, r)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["limit"].(float64) != 10 {
		t.Errorf("expected limit=10, got %v", resp["limit"])
	}
	if resp["offset"].(float64) != 5 {
		t.Errorf("expected offset=5, got %v", resp["offset"])
	}
}

func TestHandleListJobs_DefaultLimitAndOffset(t *testing.T) {
	h := newTestHandlers(&stubJobStore{})

	w := httptest.NewRecorder()
	h.HandleListJobs(w, tenantRequest(t, "GET", "/v1/jobs"))

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["limit"].(float64) != 20 {
		t.Errorf("expected default limit=20, got %v", resp["limit"])
	}
	if resp["offset"].(float64) != 0 {
		t.Errorf("expected default offset=0, got %v", resp["offset"])
	}
}

func TestHandleListJobs_LimitCappedAt100(t *testing.T) {
	h := newTestHandlers(&stubJobStore{})

	r := tenantRequest(t, "GET", "/v1/jobs?limit=999")
	w := httptest.NewRecorder()
	h.HandleListJobs(w, r)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["limit"].(float64) != 20 {
		t.Errorf("expected limit capped at default 20 for invalid value, got %v", resp["limit"])
	}
}

func TestHandleListJobs_JobsFieldPresent(t *testing.T) {
	store := &stubJobStore{
		jobs:  []domain.Job{{ID: uuid.New(), Status: domain.JobStatusPending}},
		total: 1,
	}
	h := newTestHandlers(store)

	w := httptest.NewRecorder()
	h.HandleListJobs(w, tenantRequest(t, "GET", "/v1/jobs"))

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	jobs, ok := resp["jobs"]
	if !ok {
		t.Fatal("response missing 'jobs' field")
	}
	if len(jobs.([]interface{})) != 1 {
		t.Errorf("expected 1 job, got %v", jobs)
	}
}
