package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPWebhook delivers events as a JSON POST to a configured URL.
// One delivery attempt per event; no retries (the user is expected
// to wire owl behind an alertmanager / receiver that handles its
// own retry semantics if they need it). The body is the JSON
// serialisation of Event.
type HTTPWebhook struct {
	URL    string
	Client *http.Client
}

// NewHTTPWebhook returns a webhook posting to url with a 5 s timeout.
func NewHTTPWebhook(url string) *HTTPWebhook {
	return &HTTPWebhook{
		URL: url,
		Client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Send POSTs e as JSON to the webhook URL.
func (h *HTTPWebhook) Send(ctx context.Context, e Event) error {
	if h.URL == "" {
		return fmt.Errorf("webhook url is empty")
	}
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "owl-alerter/0.0")

	resp, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(preview))
	}
	return nil
}
