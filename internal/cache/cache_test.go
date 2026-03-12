package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func newTestCache(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := NewRedisCache("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	return c, mr
}

// --- Get / Set ---

func TestCache_GetMissReturnsErrMiss(t *testing.T) {
	c, _ := newTestCache(t)
	_, err := c.Get(context.Background(), "nonexistent")
	if !errors.Is(err, ErrMiss) {
		t.Errorf("expected ErrMiss, got %v", err)
	}
}

func TestCache_SetThenGet(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	if err := c.Set(ctx, "key1", []byte("value1"), time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := c.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got) != "value1" {
		t.Errorf("expected %q, got %q", "value1", got)
	}
}

func TestCache_SetOverwritesExisting(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	c.Set(ctx, "key", []byte("first"), time.Minute)
	c.Set(ctx, "key", []byte("second"), time.Minute)

	got, _ := c.Get(ctx, "key")
	if string(got) != "second" {
		t.Errorf("expected %q after overwrite, got %q", "second", got)
	}
}

func TestCache_DifferentKeysIndependent(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	c.Set(ctx, "a", []byte("aval"), time.Minute)
	c.Set(ctx, "b", []byte("bval"), time.Minute)

	a, _ := c.Get(ctx, "a")
	b, _ := c.Get(ctx, "b")

	if string(a) != "aval" || string(b) != "bval" {
		t.Errorf("key isolation broken: a=%q b=%q", a, b)
	}
}

func TestCache_ExpiredKeyReturnsMiss(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()

	c.Set(ctx, "expiring", []byte("val"), time.Second)
	mr.FastForward(2 * time.Second)

	_, err := c.Get(ctx, "expiring")
	if !errors.Is(err, ErrMiss) {
		t.Errorf("expected ErrMiss after TTL expiry, got %v", err)
	}
}

func TestCache_StoresBinaryData(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	payload := []byte{0x00, 0xFF, 0x7B, 0x22, 0x7D} // includes null bytes and braces
	c.Set(ctx, "bin", payload, time.Minute)

	got, _ := c.Get(ctx, "bin")
	if string(got) != string(payload) {
		t.Errorf("binary payload corrupted: got %v", got)
	}
}

// --- RateIncr ---

func TestRateIncr_FirstCallReturnsOne(t *testing.T) {
	c, _ := newTestCache(t)
	n, err := c.RateIncr(context.Background(), "rl:tenant1:1000", time.Minute)
	if err != nil {
		t.Fatalf("RateIncr failed: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 on first call, got %d", n)
	}
}

func TestRateIncr_IncrementsOnSubsequentCalls(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	c.RateIncr(ctx, "rl:t:1", time.Minute)
	c.RateIncr(ctx, "rl:t:1", time.Minute)
	n, _ := c.RateIncr(ctx, "rl:t:1", time.Minute)

	if n != 3 {
		t.Errorf("expected 3 after 3 increments, got %d", n)
	}
}

func TestRateIncr_DifferentKeysIndependent(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	c.RateIncr(ctx, "rl:a:1", time.Minute)
	c.RateIncr(ctx, "rl:a:1", time.Minute)
	n, _ := c.RateIncr(ctx, "rl:b:1", time.Minute)

	if n != 1 {
		t.Errorf("expected b counter to be 1, got %d", n)
	}
}

func TestRateIncr_KeyExpiresAfterWindow(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()

	c.RateIncr(ctx, "rl:t:1", time.Second)
	mr.FastForward(2 * time.Second)

	// After expiry a new increment starts from 1 again
	n, _ := c.RateIncr(ctx, "rl:t:1", time.Second)
	if n != 1 {
		t.Errorf("expected counter to reset after window, got %d", n)
	}
}
