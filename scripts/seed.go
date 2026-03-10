//go:build ignore

// seed creates a dev tenant and prints the API key. Run with:
//
//	go run scripts/seed.go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/agxp/docpulse/internal/auth"
	"github.com/agxp/docpulse/internal/config"
	"github.com/agxp/docpulse/internal/database"
	"github.com/agxp/docpulse/internal/domain"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DB.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()

	rawKey, keyHash, err := auth.GenerateAPIKey()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to generate API key")
	}

	tenant := &domain.Tenant{
		ID:         uuid.New(),
		Name:       "dev",
		APIKeyHash: keyHash,
		RateLimit:  1000,
		ByteLimit:  1 << 30, // 1 GB
		CreatedAt:  time.Now(),
	}

	store := database.NewTenantStore(db)
	if err := store.Create(ctx, tenant); err != nil {
		fmt.Fprintf(os.Stderr, "error creating tenant: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Tenant created: %s\n", tenant.ID)
	fmt.Printf("API key (save this — shown once): %s\n", rawKey)
}
