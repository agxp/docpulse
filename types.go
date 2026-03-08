package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// --- Job Status ---

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusIngesting  JobStatus = "ingesting"
	JobStatusChunking   JobStatus = "chunking"
	JobStatusExtracting JobStatus = "extracting"
	JobStatusAssembling JobStatus = "assembling"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// --- Document Format ---

type DocumentFormat string

const (
	FormatPDFNative  DocumentFormat = "pdf_native"
	FormatPDFScanned DocumentFormat = "pdf_scanned"
	FormatDOCX       DocumentFormat = "docx"
	FormatImage      DocumentFormat = "image"
	FormatUnknown    DocumentFormat = "unknown"
)

// --- Model Tier ---

type ModelTier string

const (
	ModelTierFast   ModelTier = "fast"   // gpt-4o-mini — cheap, handles simple schemas
	ModelTierStrong ModelTier = "strong" // gpt-4o — expensive, handles complex extraction
)

// --- Core Entities ---

type Tenant struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	APIKeyHash string    `json:"-"` // never serialize
	RateLimit  int       `json:"rate_limit"`  // requests per minute
	ByteLimit  int64     `json:"byte_limit"`  // bytes per hour
	CreatedAt  time.Time `json:"created_at"`
}

type Job struct {
	ID               uuid.UUID        `json:"id"`
	TenantID         uuid.UUID        `json:"tenant_id"`
	Status           JobStatus        `json:"status"`
	DocumentURL      string           `json:"-"` // internal storage URL
	DocumentFormat   DocumentFormat   `json:"document_format,omitempty"`
	DocumentSizeBytes int64           `json:"document_size_bytes"`
	Schema           ExtractionSchema `json:"schema"`
	Result           json.RawMessage  `json:"result,omitempty"`
	ConfidenceScores map[string]float64 `json:"confidence_scores,omitempty"`
	ModelUsed        ModelTier        `json:"model_used,omitempty"`
	CostUSD          float64          `json:"cost_usd,omitempty"`
	ErrorMessage     string           `json:"error_message,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	CompletedAt      *time.Time       `json:"completed_at,omitempty"`
}

// ExtractionSchema is the user-defined schema describing what to extract.
// Users submit this as JSON Schema (draft-07 subset).
//
// Example:
//
//	{
//	  "type": "object",
//	  "properties": {
//	    "vendor":     { "type": "string" },
//	    "total":      { "type": "number" },
//	    "line_items": { "type": "array", "items": { "type": "object", "properties": {
//	      "description": { "type": "string" },
//	      "amount":      { "type": "number" }
//	    }}}
//	  },
//	  "required": ["vendor", "total"]
//	}
type ExtractionSchema struct {
	Raw json.RawMessage `json:"raw"`
}

type Chunk struct {
	ID       uuid.UUID `json:"id"`
	JobID    uuid.UUID `json:"job_id"`
	Sequence int       `json:"sequence"`
	Content  string    `json:"content"`
	PageStart int      `json:"page_start,omitempty"`
	PageEnd   int      `json:"page_end,omitempty"`
	Status   JobStatus `json:"status"`
}

type Webhook struct {
	ID       uuid.UUID `json:"id"`
	TenantID uuid.UUID `json:"tenant_id"`
	URL      string    `json:"url"`
	Secret   string    `json:"-"` // for HMAC signing
	Active   bool      `json:"active"`
}

type WebhookDelivery struct {
	ID           uuid.UUID `json:"id"`
	WebhookID    uuid.UUID `json:"webhook_id"`
	JobID        uuid.UUID `json:"job_id"`
	Attempt      int       `json:"attempt"`
	Status       string    `json:"status"` // "success", "failed", "pending"
	ResponseCode int      `json:"response_code,omitempty"`
	NextRetryAt  *time.Time `json:"next_retry_at,omitempty"`
}

// --- API Request/Response Types ---

type ExtractRequest struct {
	Schema   json.RawMessage `json:"schema"`
	Webhook  string          `json:"webhook,omitempty"`   // optional callback URL
	Priority string          `json:"priority,omitempty"`  // "normal" or "high"
	// Document is uploaded as multipart form data, not in JSON body
}

type ExtractResponse struct {
	JobID  uuid.UUID `json:"job_id"`
	Status JobStatus `json:"status"`
	Poll   string    `json:"poll_url"`
}

type JobResponse struct {
	Job
	PollURL string `json:"poll_url,omitempty"` // only if still processing
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

// --- Extraction Result Types ---

type FieldConfidence struct {
	Value      interface{} `json:"value"`
	Confidence float64     `json:"confidence"` // 0.0 = missing, 0.5 = ambiguous, 1.0 = clean
	Source     string      `json:"source,omitempty"` // which chunk this came from
}

type ExtractionResult struct {
	Fields   map[string]FieldConfidence `json:"fields"`
	Metadata ExtractionMetadata         `json:"metadata"`
}

type ExtractionMetadata struct {
	TotalChunks    int       `json:"total_chunks"`
	ProcessedChunks int      `json:"processed_chunks"`
	ModelUsed      ModelTier `json:"model_used"`
	TotalTokensIn  int       `json:"total_tokens_in"`
	TotalTokensOut int       `json:"total_tokens_out"`
	CostUSD        float64   `json:"cost_usd"`
	CacheHit       bool      `json:"cache_hit"`
}
