# Build stage
FROM golang:1.24-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/worker ./cmd/worker

# Shared runtime base with text extraction tools
FROM debian:bookworm-slim AS runtime-base
RUN apt-get update && apt-get install -y \
    ca-certificates \
    poppler-utils \
    tesseract-ocr \
    pandoc \
    && rm -rf /var/lib/apt/lists/*

# API image
FROM runtime-base AS api
COPY --from=builder /bin/api /app/api
WORKDIR /app
EXPOSE 8080
CMD ["/app/api"]

# Worker image
FROM runtime-base AS worker
COPY --from=builder /bin/worker /app/worker
WORKDIR /app
CMD ["/app/worker"]
