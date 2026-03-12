package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/agxp/docpulse/internal/cache"
	"github.com/agxp/docpulse/internal/config"
	"github.com/agxp/docpulse/internal/database"
	"github.com/agxp/docpulse/internal/domain"
	"github.com/agxp/docpulse/internal/extraction"
	"github.com/agxp/docpulse/internal/ingestion"
	"github.com/agxp/docpulse/internal/llm"
	"github.com/agxp/docpulse/internal/storage"
	"github.com/agxp/docpulse/internal/webhook"
)

// Worker polls for pending jobs and processes them through the full pipeline:
// ingest → chunk → extract → assemble → complete → webhook
type Worker struct {
	jobs      *database.JobStore
	webhooks  *database.WebhookStore
	deliverer *webhook.Deliverer
	store     storage.ObjectStore
	extractor *ingestion.TextExtractor
	chunker   *extraction.Chunker
	router    *llm.Router
	cfg       config.WorkerConfig
	cache     cache.Cache
}

func NewWorker(
	jobs *database.JobStore,
	webhooks *database.WebhookStore,
	deliverer *webhook.Deliverer,
	store storage.ObjectStore,
	extractor *ingestion.TextExtractor,
	chunker *extraction.Chunker,
	router *llm.Router,
	c cache.Cache,
	cfg config.WorkerConfig,
) *Worker {
	return &Worker{
		jobs:      jobs,
		webhooks:  webhooks,
		deliverer: deliverer,
		store:     store,
		extractor: extractor,
		chunker:   chunker,
		router:    router,
		cache:     c,
		cfg:       cfg,
	}
}

// Run starts the worker loop. It blocks until the context is cancelled.
// Multiple workers can run concurrently — ClaimNext uses FOR UPDATE SKIP LOCKED.
func (w *Worker) Run(ctx context.Context) error {
	log.Info().Int("concurrency", w.cfg.Concurrency).Msg("worker started")

	sem := make(chan struct{}, w.cfg.Concurrency)

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("worker shutting down, waiting for in-flight jobs...")
			for i := 0; i < w.cfg.Concurrency; i++ {
				sem <- struct{}{}
			}
			log.Info().Msg("worker stopped")
			return nil
		default:
		}

		job, err := w.jobs.ClaimNext(ctx)
		if err != nil {
			log.Error().Err(err).Msg("error claiming job")
			time.Sleep(w.cfg.PollInterval)
			continue
		}
		if job == nil {
			time.Sleep(w.cfg.PollInterval)
			continue
		}

		sem <- struct{}{}
		go func(j *domain.Job) {
			defer func() { <-sem }()

			jobCtx, cancel := context.WithTimeout(ctx, w.cfg.MaxJobDuration)
			defer cancel()

			if err := w.processJob(jobCtx, j); err != nil {
				log.Error().
					Err(err).
					Str("job_id", j.ID.String()).
					Msg("job processing failed")
				w.jobs.Fail(ctx, j.ID, err.Error())
			}
		}(job)
	}
}

func (w *Worker) processJob(ctx context.Context, job *domain.Job) error {
	logger := log.With().Str("job_id", job.ID.String()).Logger()
	start := time.Now()

	// --- Step 1: Download document ---
	logger.Info().Msg("downloading document")
	docData, err := w.store.Download(ctx, extractStorageKey(job.DocumentURL))
	if err != nil {
		return fmt.Errorf("downloading document: %w", err)
	}

	// --- Step 2: Check content hash cache ---
	cacheKey := contentHash(docData, job.Schema.Raw)
	if cached := w.checkCache(ctx, cacheKey); cached != nil {
		logger.Info().Msg("cache hit — returning cached result")
		if err := w.jobs.Complete(ctx, job.ID, cached, map[string]float64{"_cache_hit": 1.0}, domain.ModelTierFast, 0); err != nil {
			return err
		}
		if completed, err := w.jobs.GetByID(ctx, job.ID); err == nil {
			w.fireWebhooks(ctx, *completed)
		}
		return nil
	}

	// --- Step 3: Extract text ---
	logger.Info().Str("format", string(job.DocumentFormat)).Msg("extracting text")
	w.jobs.UpdateStatus(ctx, job.ID, domain.JobStatusIngesting)

	text, err := w.extractor.Extract(ctx, docData, job.DocumentFormat)
	if err != nil {
		return fmt.Errorf("text extraction: %w", err)
	}

	if text == "" {
		return fmt.Errorf("no text extracted from document")
	}

	logger.Info().Int("text_length", len(text)).Msg("text extracted")

	// --- Step 4: Chunk ---
	logger.Info().Msg("chunking document")
	w.jobs.UpdateStatus(ctx, job.ID, domain.JobStatusChunking)

	chunks := w.chunker.Chunk(job.ID, text)
	logger.Info().Int("chunk_count", len(chunks)).Msg("document chunked")

	// --- Step 5: Extract from each chunk ---
	logger.Info().Msg("running LLM extraction")
	w.jobs.UpdateStatus(ctx, job.ID, domain.JobStatusExtracting)

	var (
		allResults []map[string]interface{}
		totalIn    int
		totalOut   int
		totalCost  float64
		modelUsed  domain.ModelTier
	)

	for i, chunk := range chunks {
		req := llm.ExtractionRequest{
			ChunkText:   chunk.Content,
			Schema:      job.Schema.Raw,
			ChunkIndex:  i,
			TotalChunks: len(chunks),
		}

		resp, err := w.router.Extract(ctx, req)
		if err != nil {
			logger.Warn().
				Err(err).
				Int("chunk", i).
				Msg("chunk extraction failed, continuing")
			continue
		}

		allResults = append(allResults, resp.Fields)
		totalIn += resp.TokensIn
		totalOut += resp.TokensOut
		modelUsed = resp.ModelUsed

		if resp.ModelUsed == domain.ModelTierStrong {
			totalCost += float64(resp.TokensIn)*2.5/1_000_000 + float64(resp.TokensOut)*10.0/1_000_000
		} else {
			totalCost += float64(resp.TokensIn)*0.15/1_000_000 + float64(resp.TokensOut)*0.60/1_000_000
		}
	}

	if len(allResults) == 0 {
		return fmt.Errorf("no chunks produced results")
	}

	// --- Step 6: Assemble results ---
	logger.Info().Msg("assembling results")
	w.jobs.UpdateStatus(ctx, job.ID, domain.JobStatusAssembling)

	merged, confidence := mergeResults(allResults, job.Schema.Raw)

	resultJSON, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("marshaling result: %w", err)
	}

	w.setCache(ctx, cacheKey, resultJSON)

	logger.Info().
		Dur("duration", time.Since(start)).
		Float64("cost_usd", totalCost).
		Int("tokens_in", totalIn).
		Int("tokens_out", totalOut).
		Str("model", string(modelUsed)).
		Msg("job completed")

	if err := w.jobs.Complete(ctx, job.ID, resultJSON, confidence, modelUsed, totalCost); err != nil {
		return err
	}

	// Fetch the completed job to pass full state to webhooks
	completed, err := w.jobs.GetByID(ctx, job.ID)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to fetch completed job for webhook delivery")
		return nil
	}

	w.fireWebhooks(ctx, *completed)
	return nil
}

// mergeResults combines extraction results from multiple chunks.
func mergeResults(results []map[string]interface{}, schema json.RawMessage) (map[string]interface{}, map[string]float64) {
	merged := make(map[string]interface{})
	confidence := make(map[string]float64)
	fieldSources := make(map[string]int)

	var schemaDef map[string]interface{}
	json.Unmarshal(schema, &schemaDef)
	props, _ := schemaDef["properties"].(map[string]interface{})

	for _, result := range results {
		for key, value := range result {
			if value == nil {
				continue
			}

			fieldSources[key]++

			existing, exists := merged[key]
			if !exists {
				merged[key] = value
				continue
			}

			// Array fields: concatenate across chunks
			if prop, ok := props[key].(map[string]interface{}); ok {
				if t, _ := prop["type"].(string); t == "array" {
					existingArr, ok1 := existing.([]interface{})
					newArr, ok2 := value.([]interface{})
					if ok1 && ok2 {
						merged[key] = append(existingArr, newArr...)
					}
					continue
				}
			}
			// Scalar fields: keep first non-null value
		}
	}

	for key := range props {
		val, exists := merged[key]
		switch {
		case !exists || val == nil:
			confidence[key] = 0.0
		case fieldSources[key] > 1:
			confidence[key] = 1.0
		default:
			confidence[key] = 0.75
		}
	}

	return merged, confidence
}

// --- Cache helpers ---

func contentHash(docData []byte, schema json.RawMessage) string {
	h := sha256.New()
	h.Write(docData)
	h.Write([]byte("||"))
	h.Write(schema)
	return "extraction:" + hex.EncodeToString(h.Sum(nil))
}

func (w *Worker) checkCache(ctx context.Context, key string) json.RawMessage {
	data, err := w.cache.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, cache.ErrMiss) {
			log.Warn().Err(err).Msg("cache get error")
		}
		return nil
	}
	return json.RawMessage(data)
}

func (w *Worker) setCache(ctx context.Context, key string, value json.RawMessage) {
	if err := w.cache.Set(ctx, key, []byte(value), w.cfg.CacheTTL); err != nil {
		log.Warn().Err(err).Msg("cache set error")
	}
}

// fireWebhooks delivers the completed job to all active tenant webhooks.
// Runs each delivery in its own goroutine so it never blocks the worker loop.
func (w *Worker) fireWebhooks(ctx context.Context, job domain.Job) {
	hooks, err := w.webhooks.ListActive(ctx, job.TenantID)
	if err != nil {
		log.Warn().Err(err).Str("job_id", job.ID.String()).Msg("failed to list webhooks")
		return
	}
	for _, hook := range hooks {
		hook := hook
		go func() {
			if err := w.deliverer.Deliver(ctx, hook, job); err != nil {
				log.Warn().Err(err).
					Str("job_id", job.ID.String()).
					Str("webhook_id", hook.ID.String()).
					Msg("webhook delivery failed")
			}
		}()
	}
}

func extractStorageKey(url string) string {
	if len(url) > 7 && url[:7] == "file://" {
		return url[7:]
	}
	return url
}
