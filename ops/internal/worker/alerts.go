package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Alerter posts Slack-compatible JSON to one outgoing webhook. A nil Alerter
// (no URL configured) silently drops alerts.
type Alerter struct {
	url string
	hc  *http.Client
}

// NewAlerter builds an Alerter for the given webhook URL (may be empty).
func NewAlerter(url string) *Alerter {
	return &Alerter{url: url, hc: &http.Client{Timeout: 10 * time.Second}}
}

// Send posts a text alert. It never blocks the worker for long and ignores
// delivery errors beyond logging by the caller.
func (a *Alerter) Send(ctx context.Context, text string) {
	if a == nil || a.url == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.hc.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
