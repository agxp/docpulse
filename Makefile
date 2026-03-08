.PHONY: tidy build run-api run-worker migrate seed test lint

tidy:
	go mod tidy

build:
	go build ./cmd/api
	go build ./cmd/worker

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

migrate:
	psql $$DATABASE_URL -f migrations/001_initial_schema.sql

seed:
	go run scripts/seed.go

test:
	go test ./...

lint:
	golangci-lint run ./...
