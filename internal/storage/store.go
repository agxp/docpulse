package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ObjectStore is the interface for document storage.
type ObjectStore interface {
	Upload(ctx context.Context, key string, data []byte) (url string, err error)
	Download(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// LocalStore stores documents on the local filesystem.
// For production, replace with S3Store.
type LocalStore struct {
	baseDir string
}

func NewLocalStore(baseDir string) (*LocalStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating storage directory: %w", err)
	}
	return &LocalStore{baseDir: baseDir}, nil
}

func (s *LocalStore) Upload(_ context.Context, key string, data []byte) (string, error) {
	path := filepath.Join(s.baseDir, key)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	return "file://" + path, nil
}

func (s *LocalStore) Download(_ context.Context, key string) ([]byte, error) {
	data, err := os.ReadFile(key)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return data, nil
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	if err := os.Remove(key); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}
	return nil
}
