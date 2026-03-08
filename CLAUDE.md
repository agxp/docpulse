# CLAUDE.md

## What this is

Multi-tenant document intelligence API. Users POST a document (PDF, DOCX, image) + a JSON Schema describing what to extract → async job processes it → structured JSON result with per-field confidence scores.

Built as a portfolio project targeting Series B/C startup interviews. The point is the systems work, not the ML.

## Architecture

Two separate binaries sharing internal packages:

- `cmd/api` — HTTP server (chi router), accepts uploads, creates jobs, serves results
- `cmd/worker` — Polls PostgreSQL for pending jobs, processes them through the pipeline

Pipeline: download → format detect → text extract (pdftotext/tesseract/pandoc) → chunk → LLM extract per chunk → merge results → complete job

No external job queue. PostgreSQL `FOR UPDATE SKIP LOCKED` handles concurrent job claiming. This is deliberate — simpler to operate at this scale and a defensible architecture choice.

## Stack

Go 1.22, PostgreSQL 16, Redis 7, OpenAI API (gpt-4o-mini / gpt-4o). Deploy target is Fly.io. Local dev uses docker-compose for Postgres + Redis.

System deps for text extraction: poppler-utils (pdftotext), tesseract-ocr, pandoc.

## Project layout

```
cmd/api/main.go           — server entry point, graceful shutdown
cmd/worker/main.go        — worker entry point, graceful shutdown
internal/
  domain/types.go         — all shared types, enums, request/response structs
  config/config.go        — env var loading, all configuration lives here
  database/db.go          — pgxpool connection
  database/job_store.go   — job CRUD + ClaimNext (the critical query)
  database/tenant_store.go
  api/router.go           — chi route definitions
  api/handlers.go         — HTTP handlers (extract, get job, list jobs, health)
  api/middleware/auth.go   — API key → tenant resolution via context
  api/middleware/logging.go
  auth/apikey.go          — key generation (crypto/rand) + SHA-256 hashing
  ingestion/detector.go   — magic byte format detection
  ingestion/extractor.go  — routes to pdftotext / tesseract / pandoc
  extraction/chunker.go   — paragraph-then-sentence splitting with overlap
  llm/router.go           — model selection, prompt construction, validation, retries
  jobs/worker.go          — full pipeline orchestrator + in-memory content-hash cache
  storage/store.go        — ObjectStore interface + local filesystem impl
  webhook/deliverer.go    — HMAC-signed delivery with exponential backoff
migrations/               — raw SQL, applied via psql
scripts/seed.go           — creates dev tenant, prints API key
```

## Key conventions

- All types in `domain/types.go`. Don't scatter type definitions across packages.
- Config is env-var only. No config files. `config.Load()` reads everything.
- Structured logging via zerolog everywhere. Use `log.With().Str("job_id", ...).Logger()` for context.
- Errors wrap with `fmt.Errorf("doing thing: %w", err)` — preserve the chain.
- API keys are hashed with SHA-256 before storage. Raw keys shown once at creation.
- Tenant is injected into request context by auth middleware. Access via `middleware.TenantFromContext(ctx)`.
- Jobs are tenant-scoped. Every job query includes tenant_id — no cross-tenant data access.

## Common tasks

```bash
# Run everything locally
docker-compose up -d
source .env
make migrate && make seed
make run-api     # terminal 1
make run-worker  # terminal 2

# Test an extraction
curl -X POST http://localhost:8080/v1/extract \
  -H "Authorization: Bearer di_YOUR_KEY" \
  -F "document=@test.pdf" \
  -F 'schema={"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}'

# Check job
curl http://localhost:8080/v1/jobs/JOB_ID -H "Authorization: Bearer di_YOUR_KEY"

# Run tests
make test
```

## LLM routing logic

In `llm/router.go`. Three complexity signals determine model tier:
1. Schema field count > threshold → strong model
2. Chunk text > 3000 chars → strong model
3. Nested arrays/objects in schema → strong model

Otherwise fast model. On validation failure (missing required fields), automatically retries with strong model. Temperature is 0.0 for deterministic extraction. Uses OpenAI JSON mode.

## Things that are stubbed / TODO

- `storage/store.go`: S3Store not implemented, only LocalStore exists
- Schema validation on submission is shallow (just checks valid JSON, not valid JSON Schema)
- Webhook endpoints not wired in router.go (handlers not written)
- Redis not actually used yet — rate limiting and caching are in-memory only
- `go.sum` doesn't exist yet — run `go mod tidy` first
- No tests written yet
- Worker cache is in-memory map — lost on restart, no eviction policy
- Pagination query param parsing in HandleListJobs is a TODO comment

## Things to not change without good reason

- The `FOR UPDATE SKIP LOCKED` pattern in `ClaimNext` — this is the concurrency primitive
- Async-first job model — don't add synchronous extraction
- Magic byte detection over file extension trust
- Two-tier model routing with automatic escalation
- Content-hash cache check before LLM calls
- HMAC webhook signing
