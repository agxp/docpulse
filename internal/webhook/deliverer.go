package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/arman/docpulse/internal/domain"
)

// Deliverer sends HMAC-signed webhook payloads with exponential backoff.
type Deliverer struct {
	client *http.Client
}

func NewDeliverer() *Deliverer {
	return &Deliverer{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Deliver POSTs the job payload to the webhook URL, retrying with backoff on failure.
func (d *Deliverer) Deliver(ctx context.Context, webhook domain.Webhook, job domain.Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	sig := sign(payload, webhook.Secret)

	const maxAttempts = 5
	backoff := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-DocPulse-Signature", sig)

		resp, err := d.client.Do(req)
		if err != nil {
			log.Warn().Err(err).Int("attempt", attempt).Str("url", webhook.URL).Msg("webhook delivery failed")
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			log.Warn().Int("status", resp.StatusCode).Int("attempt", attempt).Msg("webhook returned non-2xx")
		}

		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}
	}

	return fmt.Errorf("webhook delivery failed after %d attempts", maxAttempts)
}

func sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
