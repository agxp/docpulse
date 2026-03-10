package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/agxp/docpulse/internal/domain"
)

// --- sign ---

func TestSign_Format(t *testing.T) {
	sig := sign([]byte("payload"), "secret")
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("expected 'sha256=' prefix, got %q", sig)
	}
}

func TestSign_Deterministic(t *testing.T) {
	payload := []byte("hello world")
	if sign(payload, "secret") != sign(payload, "secret") {
		t.Error("sign is not deterministic")
	}
}

func TestSign_DifferentPayloadsDifferentSigs(t *testing.T) {
	if sign([]byte("a"), "secret") == sign([]byte("b"), "secret") {
		t.Error("different payloads produced the same signature")
	}
}

func TestSign_DifferentSecretsDifferentSigs(t *testing.T) {
	payload := []byte("same payload")
	if sign(payload, "secret1") == sign(payload, "secret2") {
		t.Error("different secrets produced the same signature")
	}
}

func TestSign_KnownHMAC(t *testing.T) {
	// HMAC-SHA256("", "") starts with b613679a — verify we're using the right algorithm
	sig := sign([]byte(""), "")
	if !strings.HasPrefix(sig, "sha256=b613679a") {
		t.Errorf("unexpected HMAC for empty inputs: %s", sig)
	}
}

func TestSign_HexLengthCorrect(t *testing.T) {
	sig := sign([]byte("data"), "key")
	// "sha256=" (7) + 64 hex chars = 71
	if len(sig) != 71 {
		t.Errorf("expected sig length 71, got %d: %s", len(sig), sig)
	}
}

// --- Deliver ---

func testJob() domain.Job {
	return domain.Job{
		ID:       uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		TenantID: uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		Status:   domain.JobStatusCompleted,
	}
}

func testHook(url string) domain.Webhook {
	return domain.Webhook{
		ID:     uuid.New(),
		URL:    url,
		Secret: "test-secret",
		Active: true,
	}
}

func readBody(r *http.Request) []byte {
	data, _ := io.ReadAll(r.Body)
	return data
}

func TestDeliver_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	if err := d.Deliver(context.Background(), testHook(srv.URL), testJob()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeliver_SetsContentTypeHeader(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	NewDeliverer().Deliver(context.Background(), testHook(srv.URL), testJob())

	if gotContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", gotContentType)
	}
}

func TestDeliver_SetsSignatureHeader(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-DocPulse-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := testHook(srv.URL)
	NewDeliverer().Deliver(context.Background(), hook, testJob())

	if !strings.HasPrefix(gotSig, "sha256=") {
		t.Errorf("expected sha256= signature header, got %q", gotSig)
	}
}

func TestDeliver_SignatureMatchesPayload(t *testing.T) {
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-DocPulse-Signature")
		gotBody = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := testHook(srv.URL)
	NewDeliverer().Deliver(context.Background(), hook, testJob())

	expected := sign(gotBody, hook.Secret)
	if gotSig != expected {
		t.Errorf("signature mismatch: got %q, want %q", gotSig, expected)
	}
}

func TestDeliver_BodyIsValidJSON(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	NewDeliverer().Deliver(context.Background(), testHook(srv.URL), testJob())

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Errorf("body is not valid JSON: %v — body: %s", err, body)
	}
}

func TestDeliver_BodyContainsJobID(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	job := testJob()
	NewDeliverer().Deliver(context.Background(), testHook(srv.URL), job)

	if !strings.Contains(string(body), job.ID.String()) {
		t.Errorf("body does not contain job ID %s: %s", job.ID, body)
	}
}

func fastDeliverer() *Deliverer {
	return &Deliverer{
		client:        &http.Client{Timeout: time.Second},
		initialBackoff: time.Millisecond,
	}
}

func TestDeliver_Non2xx_Retries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	fastDeliverer().Deliver(context.Background(), testHook(srv.URL), testJob())

	if calls.Load() != 5 {
		t.Errorf("expected 5 attempts (maxAttempts), got %d", calls.Load())
	}
}

func TestDeliver_EventualSuccess_StopsRetrying(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n >= 3 {
			w.WriteHeader(http.StatusOK) // succeed on 3rd attempt
		} else {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()

	if err := fastDeliverer().Deliver(context.Background(), testHook(srv.URL), testJob()); err != nil {
		t.Errorf("expected success after retries, got: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected exactly 3 calls, got %d", calls.Load())
	}
}

func TestDeliver_FailsAfterMaxAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := fastDeliverer().Deliver(context.Background(), testHook(srv.URL), testJob()); err == nil {
		t.Error("expected error after max attempts")
	}
}

func TestDeliver_UsesPostMethod(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	NewDeliverer().Deliver(context.Background(), testHook(srv.URL), testJob())

	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
}

func TestDeliver_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // always fail to trigger retry
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := fastDeliverer().Deliver(ctx, testHook(srv.URL), testJob())
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
