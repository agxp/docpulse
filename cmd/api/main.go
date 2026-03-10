package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/agxp/docpulse/internal/api"
	"github.com/agxp/docpulse/internal/config"
	"github.com/agxp/docpulse/internal/database"
	"github.com/agxp/docpulse/internal/storage"
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

	jobs := database.NewJobStore(db)
	tenants := database.NewTenantStore(db)

	baseURL := fmt.Sprintf("http://localhost:%s", cfg.API.Port)
	handlers := api.NewHandlers(jobs, store, baseURL)
	router := api.NewRouter(handlers, tenants)

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
