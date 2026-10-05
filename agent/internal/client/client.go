// Package client is the agent's HTTP transport to the panel: register,
// heartbeat, long-poll claim, start, logs, result. It carries the bearer token
// and the protocol version header on every authenticated call.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// Client talks to one panel.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the bearer token for authenticated calls.
func WithToken(token string) Option { return func(c *Client) { c.token = token } }

// WithCAFile pins a private CA for verifying the panel's TLS certificate.
func WithCAFile(path string) Option {
	return func(c *Client) {
		if path == "" {
			return
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return
		}
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(pem) {
			c.hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
		}
	}
}

// New builds a Client for the panel at baseURL.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		hc:      &http.Client{Timeout: 0}, // per-request contexts set deadlines
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// SetToken updates the bearer token (after enrollment).
func (c *Client) SetToken(token string) { c.token = token }

func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(protocol.HeaderProtocol, strconv.Itoa(protocol.Version))
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUpgradeRequired {
		return resp.StatusCode, fmt.Errorf("panel requires a newer agent (protocol %d)", protocol.Version)
	}
	if out != nil && resp.StatusCode/100 == 2 && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}

// Register enrolls the server and returns the long-lived credential.
func (c *Client) Register(ctx context.Context, req protocol.RegisterRequest) (protocol.RegisterResponse, error) {
	var out protocol.RegisterResponse
	code, err := c.do(ctx, http.MethodPost, "/agent/v1/register", req, &out)
	if err != nil {
		return out, err
	}
	if code != http.StatusOK {
		return out, fmt.Errorf("register: panel returned %d", code)
	}
	return out, nil
}

// Heartbeat reports liveness and returns jobs to cancel.
func (c *Client) Heartbeat(ctx context.Context, req protocol.HeartbeatRequest) (protocol.HeartbeatResponse, error) {
	var out protocol.HeartbeatResponse
	code, err := c.do(ctx, http.MethodPost, "/agent/v1/heartbeat", req, &out)
	if err != nil {
		return out, err
	}
	if code != http.StatusOK {
		return out, fmt.Errorf("heartbeat: panel returned %d", code)
	}
	return out, nil
}

// NextJob long-polls for a job. ok is false when the poll returned 204 (idle).
func (c *Client) NextJob(ctx context.Context) (protocol.Job, bool, error) {
	var out protocol.Job
	// Give the long-poll room beyond the server's 30s wait.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	code, err := c.do(ctx, http.MethodGet, "/agent/v1/jobs/next", nil, &out)
	if err != nil {
		return out, false, err
	}
	if code == http.StatusNoContent {
		return out, false, nil
	}
	if code != http.StatusOK {
		return out, false, fmt.Errorf("jobs/next: panel returned %d", code)
	}
	return out, true, nil
}

// Start marks a job running.
func (c *Client) Start(ctx context.Context, jobID int64) error {
	code, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agent/v1/jobs/%d/start", jobID), nil, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("start: panel returned %d", code)
	}
	return nil
}

// SendLogs posts a batch of log lines. The caller enforces the batch size.
func (c *Client) SendLogs(ctx context.Context, jobID int64, lines []protocol.LogLine) error {
	code, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agent/v1/jobs/%d/logs", jobID),
		protocol.LogsRequest{Lines: lines}, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("logs: panel returned %d", code)
	}
	return nil
}

// SendResult posts the final result.
func (c *Client) SendResult(ctx context.Context, jobID int64, req protocol.ResultRequest) error {
	code, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/agent/v1/jobs/%d/result", jobID), req, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("result: panel returned %d", code)
	}
	return nil
}
