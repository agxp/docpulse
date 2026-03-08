package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/arman/docint/internal/api/middleware"
	"github.com/arman/docint/internal/database"
	"github.com/arman/docint/internal/domain"
	"github.com/arman/docint/internal/ingestion"
	"github.com/arman/docint/internal/storage"
)

type Handlers struct {
	jobs    *database.JobStore
	store   storage.ObjectStore
	baseURL string
}

func NewHandlers(jobs *database.JobStore, store storage.ObjectStore, baseURL string) *Handlers {
	return &Handlers{jobs: jobs, store: store, baseURL: baseURL}
}

// HandleExtract accepts a multipart document + JSON schema and creates an async job.
func (h *Handlers) HandleExtract(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, "unauthorized", "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(50 << 20); err != nil { // 50 MB limit
		writeError(w, "request body too large or malformed", "bad_request", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("document")
	if err != nil {
		writeError(w, "missing document field", "bad_request", http.StatusBadRequest)
		return
	}
	defer file.Close()

	docData := make([]byte, header.Size)
	if _, err := file.Read(docData); err != nil {
		writeError(w, "failed to read document", "bad_request", http.StatusBadRequest)
		return
	}

	schemaRaw := r.FormValue("schema")
	if schemaRaw == "" {
		writeError(w, "missing schema field", "bad_request", http.StatusBadRequest)
		return
	}

	var schemaCheck map[string]interface{}
	if err := json.Unmarshal([]byte(schemaRaw), &schemaCheck); err != nil {
		writeError(w, "schema is not valid JSON", "bad_request", http.StatusBadRequest)
		return
	}

	format := ingestion.DetectFormat(docData)
	if format == domain.FormatUnknown {
		writeError(w, "unsupported document format", "bad_request", http.StatusBadRequest)
		return
	}

	jobID := uuid.New()
	storageKey := fmt.Sprintf("%s/%s/%s", tenant.ID, jobID, header.Filename)

	docURL, err := h.store.Upload(r.Context(), storageKey, docData)
	if err != nil {
		log.Error().Err(err).Msg("failed to upload document")
		writeError(w, "failed to store document", "internal_error", http.StatusInternalServerError)
		return
	}

	job := &domain.Job{
		ID:                jobID,
		TenantID:          tenant.ID,
		Status:            domain.JobStatusPending,
		DocumentURL:       docURL,
		DocumentFormat:    format,
		DocumentSizeBytes: header.Size,
		Schema:            domain.ExtractionSchema{Raw: json.RawMessage(schemaRaw)},
		CreatedAt:         time.Now(),
	}

	if err := h.jobs.Create(r.Context(), job); err != nil {
		log.Error().Err(err).Msg("failed to create job")
		writeError(w, "failed to create job", "internal_error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, domain.ExtractResponse{
		JobID:  jobID,
		Status: domain.JobStatusPending,
		Poll:   fmt.Sprintf("%s/v1/jobs/%s", h.baseURL, jobID),
	})
}

// HandleGetJob retrieves the status and result of a specific job.
func (h *Handlers) HandleGetJob(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	jobIDStr := chi.URLParam(r, "id")

	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		writeError(w, "invalid job ID", "bad_request", http.StatusBadRequest)
		return
	}

	job, err := h.jobs.Get(r.Context(), tenant.ID, jobID)
	if err != nil {
		writeError(w, "job not found", "not_found", http.StatusNotFound)
		return
	}

	resp := domain.JobResponse{Job: *job}
	if job.Status != domain.JobStatusCompleted && job.Status != domain.JobStatusFailed {
		resp.PollURL = fmt.Sprintf("%s/v1/jobs/%s", h.baseURL, jobID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleListJobs returns the tenant's jobs, newest first.
func (h *Handlers) HandleListJobs(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())

	limit := 20
	offset := 0

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	jobs, err := h.jobs.List(r.Context(), tenant.ID, limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("failed to list jobs")
		writeError(w, "failed to list jobs", "internal_error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":   jobs,
		"limit":  limit,
		"offset": offset,
	})
}

// HandleHealth returns 200 OK.
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, msg, code string, status int) {
	writeJSON(w, status, domain.ErrorResponse{Error: msg, Code: code})
}
