package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// DispatchParams contains the arguments required to send a webhook HTTP POST.
type DispatchParams struct {
	DeliveryID string
	WebhookID  string
	URL        string
	Secret     string
	Headers    map[string]string
	EventType  string
	AccountID  string
	Payload    []byte
	Timeout    time.Duration
}

// DispatchResult contains the outcome of a webhook HTTP call.
type DispatchResult struct {
	StatusCode int
	Success    bool
	Err        error
}

// Dispatcher executes outbound HTTP requests.
type Dispatcher struct {
	client *http.Client
}

// NewDispatcher creates a new Dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		client: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
}

// Dispatch sends the HTTP POST request to the configured URL.
func (d *Dispatcher) Dispatch(ctx context.Context, p DispatchParams) DispatchResult {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.URL, bytes.NewReader(p.Payload))
	if err != nil {
		return DispatchResult{Err: fmt.Errorf("create request: %w", err)}
	}

	// 1. Set user custom headers first so they cannot override system/security headers
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}

	// 2. Set system and tracing headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "matfix-webhook/0.1.0")
	req.Header.Set("X-Matfix-Delivery-ID", p.DeliveryID)
	req.Header.Set("X-Matfix-Webhook-ID", p.WebhookID)
	req.Header.Set("X-Matfix-Event-Type", p.EventType)
	req.Header.Set("X-Matfix-Account-ID", p.AccountID)

	// 3. Compute and set HMAC signature if secret is present
	if p.Secret != "" {
		mac := hmac.New(sha256.New, []byte(p.Secret))
		mac.Write(p.Payload)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Matfix-Signature-256", sig)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return DispatchResult{Err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	isSuccess := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !isSuccess {
		return DispatchResult{
			StatusCode: resp.StatusCode,
			Success:    false,
			Err:        fmt.Errorf("server responded with status %d", resp.StatusCode),
		}
	}

	return DispatchResult{
		StatusCode: resp.StatusCode,
		Success:    true,
	}
}
