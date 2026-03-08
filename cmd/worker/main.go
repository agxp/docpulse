package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/arman/docint/internal/config"
	"github.com/arman/docint/internal/database"
	"github.com/arman/docint/internal/extraction"
	"github.com/arman/docint/internal/ingestion"
	"github.com/arman/docint/internal/jobs"
	"github.com/arman/docint/internal/llm"
	"github.com/arman/docint/internal/storage"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DB.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()

	store, err := storage.NewLocalStore(cfg.Storage.LocalDir)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize storage")
	}

	jobStore := database.NewJobStore(db)
	extractor := ingestion.NewTextExtractor()
	chunker := extraction.NewChunker(extraction.DefaultChunkConfig())
	router := llm.NewRouter(cfg.LLM)

	worker := jobs.NewWorker(jobStore, store, extractor, chunker, router, cfg.Worker)

	runCtx, runCancel := context.WithCancel(context.Background())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Info().Msg("received shutdown signal")
		runCancel()
	}()

	if err := worker.Run(runCtx); err != nil {
		log.Fatal().Err(err).Msg("worker error")
	}
}
