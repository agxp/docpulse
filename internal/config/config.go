package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DB      DBConfig
	Redis   RedisConfig
	API     APIConfig
	LLM     LLMConfig
	Worker  WorkerConfig
	Storage StorageConfig
}

type DBConfig struct {
	URL string
}

type RedisConfig struct {
	URL string
}

type APIConfig struct {
	Port              string
	RateLimitPerMinute int
}

type LLMConfig struct {
	OpenAIKey           string
	FastModel           string
	StrongModel         string
	MaxRetries          int
	ComplexityThreshold int
}

type WorkerConfig struct {
	Concurrency    int
	PollInterval   time.Duration
	MaxJobDuration time.Duration
	CacheTTL       time.Duration
}

type StorageConfig struct {
	LocalDir string
}

func Load() Config {
	return Config{
		DB: DBConfig{
			URL: getEnv("DATABASE_URL", "postgres://docpulse:docpulse@localhost:5432/docpulse?sslmode=disable"),
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL", "redis://localhost:6379"),
		},
		API: APIConfig{
			Port:               getEnv("PORT", "8080"),
			RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 60),
		},
		LLM: LLMConfig{
			OpenAIKey:           os.Getenv("OPENAI_API_KEY"),
			FastModel:           getEnv("LLM_FAST_MODEL", "gpt-4o-mini"),
			StrongModel:         getEnv("LLM_STRONG_MODEL", "gpt-4o"),
			MaxRetries:          getEnvInt("LLM_MAX_RETRIES", 2),
			ComplexityThreshold: getEnvInt("LLM_COMPLEXITY_THRESHOLD", 10),
		},
		Worker: WorkerConfig{
			Concurrency:    getEnvInt("WORKER_CONCURRENCY", 4),
			PollInterval:   getEnvDuration("WORKER_POLL_INTERVAL", 2*time.Second),
			MaxJobDuration: getEnvDuration("WORKER_MAX_JOB_DURATION", 10*time.Minute),
			CacheTTL:       getEnvDuration("WORKER_CACHE_TTL", 24*time.Hour),
		},
		Storage: StorageConfig{
			LocalDir: getEnv("STORAGE_LOCAL_DIR", "/tmp/docpulse"),
		},
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
