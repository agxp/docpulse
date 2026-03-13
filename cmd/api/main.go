package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/agxp/docpulse/internal/api"
	"github.com/agxp/docpulse/internal/auth"
	"github.com/agxp/docpulse/internal/cache"
	"github.com/agxp/docpulse/internal/config"
	"github.com/agxp/docpulse/internal/database"
	"github.com/agxp/docpulse/internal/domain"
	"github.com/agxp/docpulse/internal/storage"
	"github.com/agxp/docpulse/migrations"
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

	if err := runMigrations(db); err != nil {
		log.Fatal().Err(err).Msg("migrations failed")
	}

	if cfg.API.DevMode && cfg.API.DevAPIKey != "" {
		seedDevTenant(db, cfg.API.DevAPIKey)
		log.Info().Str("api_key", cfg.API.DevAPIKey).Msg("dev tenant ready")
	}

	redisCache, err := cache.NewRedisCache(cfg.Redis.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Redis")
	}

	store, err := storage.NewLocalStore(cfg.Storage.LocalDir)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize storage")
	}

	jobs := database.NewJobStore(db)
	tenants := database.NewTenantStore(db)
	webhooks := database.NewWebhookStore(db)

	baseURL := fmt.Sprintf("http://localhost:%s", cfg.API.Port)
	handlers := api.NewHandlers(jobs, webhooks, store, baseURL, cfg.API.DevAPIKey)
	router := api.NewRouter(handlers, tenants, redisCache, cfg.API.RateLimitPerMinute)

	srv := &http.Server{
		Addr:         ":" + cfg.API.Port,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("addr", srv.Addr).Msg("API server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down API server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("server forced to shutdown")
	}
	log.Info().Msg("server stopped")
}

func runMigrations(db *pgxpool.Pool) error {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("reading migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		sql, err := migrations.Files.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		if _, err := db.Exec(context.Background(), string(sql)); err != nil {
			return fmt.Errorf("applying %s: %w", e.Name(), err)
		}
		log.Info().Str("file", e.Name()).Msg("migration applied")
	}
	return nil
}

func seedDevTenant(db *pgxpool.Pool, rawKey string) {
	keyHash := auth.HashAPIKey(rawKey)
	tenant := &domain.Tenant{
		ID:         uuid.New(),
		Name:       "dev",
		APIKeyHash: keyHash,
		RateLimit:  1000,
		ByteLimit:  1 << 30,
		CreatedAt:  time.Now(),
	}
	store := database.NewTenantStore(db)
	if err := store.CreateIfNotExists(context.Background(), tenant); err != nil {
		log.Warn().Err(err).Msg("could not seed dev tenant")
	}
}
