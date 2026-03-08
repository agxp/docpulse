# DocPulse — Document Intelligence API

Multi-tenant document extraction platform. Submit any document + a JSON schema describing what to extract → get back structured JSON with per-field confidence scores.

## Quickstart

```bash
# 1. Start dependencies
docker-compose up -d

# 2. Set environment
cp .env.example .env
# Edit .env — add your OPENAI_API_KEY

source .env

# 3. Run migrations + create dev tenant
make migrate
make seed    # prints your API key — save it

# 4. Start API and worker (separate terminals)
make run-api
make run-worker
```

## Usage

```bash
# Submit an extraction job
curl -X POST http://localhost:8080/v1/extract \
  -H "Authorization: Bearer di_your_key_here" \
  -F "document=@invoice.pdf" \
  -F 'schema={
    "type": "object",
    "properties": {
      "vendor": {"type": "string"},
      "invoice_number": {"type": "string"},
      "total": {"type": "number"},
      "line_items": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "description": {"type": "string"},
            "amount": {"type": "number"}
          }
        }
      }
    },
    "required": ["vendor", "total"]
  }'

# Response:
# {"job_id": "abc-123", "status": "pending", "poll_url": "/v1/jobs/abc-123"}

# Poll for results
curl http://localhost:8080/v1/jobs/abc-123 \
  -H "Authorization: Bearer di_your_key_here"
```

## Architecture

```
Client → API (Go/chi) → PostgreSQL (job queue)
                              ↓
                         Worker Pool
                    ┌────────┼────────┐
                    │        │        │
                 Ingest   Chunk    Extract
                    │        │        │
               PDF/OCR   Semantic  LLM Router
               DOCX      Boundary  (fast/strong)
                    │        │        │
                    └────────┼────────┘
                              ↓
                     Result Assembly
                     + Confidence Scoring
                              ↓
                     Job Complete / Webhook
```

**Key decisions:**
- Async-first: jobs never block HTTP connections
- FOR UPDATE SKIP LOCKED: safe concurrent job claiming without a separate queue
- Two-tier LLM routing: cheap model for simple schemas, strong model for complex ones + automatic escalation on validation failure
- Content-hash cache: SHA-256(document + schema) catches exact duplicates at zero cost
- Magic-byte format detection: more robust than trusting file extensions

## Project Structure

```
cmd/api/          — HTTP server entry point
cmd/worker/       — Job processor entry point
internal/
  api/            — HTTP handlers and routing
  api/middleware/  — Auth, logging
  auth/           — API key generation and hashing
  config/         — Environment-based configuration
  database/       — PostgreSQL stores (jobs, tenants)
  domain/         — Core types shared across packages
  extraction/     — Chunking engine
  ingestion/      — Format detection, text extraction (PDF/OCR/DOCX)
  jobs/           — Worker loop and job processing pipeline
  llm/            — Model routing and structured extraction
  storage/        — Object storage interface (local/S3)
  webhook/        — Webhook delivery with HMAC signing + retries
migrations/       — SQL schema migrations
scripts/          — Dev utilities (seed tenant)
```

## Stack

Go 1.22 · PostgreSQL 16 · Redis 7 · OpenAI API · Fly.io

**System dependencies** (for text extraction):
- `poppler-utils` — pdftotext for native PDFs
- `tesseract-ocr` — OCR for scanned PDFs and images
- `pandoc` — DOCX to text conversion
