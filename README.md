# DocPulse — Document Intelligence API

Multi-tenant document extraction platform. Submit any document + a JSON schema describing what to extract → get back structured JSON with per-field confidence scores.

## Quickstart

```bash
# 1. Start dependencies
docker-compose up -d

# 2. Set environment
cp .env.example .env
# Edit .env — add your OPENAI_API_KEY

set -a && source .env && set +a

# 3. Run migrations + create dev tenant
make migrate    # requires psql installed locally
make seed       # prints your API key — save it

# 4. Start API and worker (separate terminals)
make run-api
make run-worker
```

## Usage

### Submit an extraction job

```bash
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
```

### Poll for results

```bash
curl http://localhost:8080/v1/jobs/abc-123 \
  -H "Authorization: Bearer di_your_key_here"
```

### List jobs

```bash
curl "http://localhost:8080/v1/jobs?limit=20&offset=0" \
  -H "Authorization: Bearer di_your_key_here"

# Response:
# {"jobs": [...], "limit": 20, "offset": 0}
```

Default limit is 20, max is 100. No total count is returned (not yet implemented).

### Webhooks

Register a URL to receive a POST when a job completes. The secret is generated server-side and shown **once** — store it to verify signatures.

```bash
# Register
curl -X POST http://localhost:8080/v1/webhooks \
  -H "Authorization: Bearer di_your_key_here" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/webhook"}'

# Response includes the secret — save it:
# {"id": "...", "url": "...", "secret": "abc123...", "active": true}

# Delete
curl -X DELETE http://localhost:8080/v1/webhooks/{id} \
  -H "Authorization: Bearer di_your_key_here"
```

Each delivery is a `POST` with:
- `Content-Type: application/json` — body is the full job object
- `X-DocPulse-Signature: sha256=<hmac>` — HMAC-SHA256 of the body using your secret

Verify the signature on your server:

```python
import hmac, hashlib

def verify(secret: str, body: bytes, header: str) -> bool:
    expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, header)
```

Failed deliveries are retried up to 5 times with exponential backoff.

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
- HMAC-signed webhooks: recipients can verify payload integrity

## Project Structure

```
cmd/api/          — HTTP server entry point
cmd/worker/       — Job processor entry point
internal/
  api/            — HTTP handlers and routing
  api/middleware/  — Auth, logging
  auth/           — API key generation and hashing
  config/         — Environment-based configuration
  database/       — PostgreSQL stores (jobs, tenants, webhooks)
  domain/         — Core types shared across packages
  extraction/     — Chunking engine
  ingestion/      — Format detection, text extraction (PDF/OCR/DOCX)
  jobs/           — Worker loop and job processing pipeline
  llm/            — Model routing and structured extraction
  storage/        — Object storage interface (local filesystem only)
  webhook/        — Webhook delivery with HMAC signing + retries
migrations/       — SQL schema migrations
scripts/          — Dev utilities (seed tenant)
```

## Stack

Go 1.24 · PostgreSQL 16 · Redis 7 · OpenAI API · Fly.io

**System dependencies** (for text extraction):
- `poppler-utils` — pdftotext for native PDFs
- `tesseract-ocr` — OCR for scanned PDFs and images
- `pandoc` — DOCX to text conversion

## Known limitations

- **Storage**: only local filesystem (`LocalStore`) is implemented. S3 support is stubbed but not built.
- **Schema validation**: validates structure (type=object, properties present, each property has a type), but does not implement the full JSON Schema specification.
- **Job list pagination**: `limit`/`offset` work and response includes a `total` count, but there is no cursor-based pagination.
- **Worker cache**: Redis-backed with a configurable TTL (`WORKER_CACHE_TTL`, default 24h), but no LRU eviction beyond TTL.
- **`make migrate`**: runs `psql` directly — requires `psql` installed on your machine, not just Docker.
