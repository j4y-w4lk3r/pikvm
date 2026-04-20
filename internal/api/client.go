// Package api is the PiKVM HTTP + WebSocket client. All calls to
// /api/* go through Do (or NewRequest/RunRequest for the rare case that
// needs custom headers). The transport is a single lazily-initialized
// *http.Client process-wide so TCP+TLS connections are reused.
package api

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"sync"
	"time"

	"pikvm/internal/config"
)

var (
	clientOnce sync.Once
	client     *http.Client
)

// httpClient returns the shared *http.Client (lazily built on first use).
func httpClient() *http.Client {
	clientOnce.Do(func() {
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxIdleConns:        8,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   false, // PiKVM (kvmd) is HTTP/1.1 only
			},
		}
	})
	return client
}

// NewRequest builds an authenticated, context-bound request to
// config.BaseURL+endpoint. Caller is responsible for sending it via
// RunRequest AND for calling the returned cancel func (typically deferred
// until after resp.Body is closed).
func NewRequest(method, endpoint string, body io.Reader, timeout time.Duration) (*http.Request, context.CancelFunc, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	req, err := http.NewRequestWithContext(ctx, method, config.BaseURL+endpoint, body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.SetBasicAuth(config.User, config.Pass)
	return req, cancel, nil
}

// RunRequest sends a request built by NewRequest and wires the supplied
// cancel func into resp.Body so closing the body also frees the per-call
// context (no goroutine leak).
func RunRequest(req *http.Request, cancel context.CancelFunc) (*http.Response, error) {
	resp, err := httpClient().Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// Do is the common case: build + send. Caller MUST close resp.Body.
// Pass timeout <= 0 to use the 10s default.
func Do(method, endpoint string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, cancel, err := NewRequest(method, endpoint, body, timeout)
	if err != nil {
		return nil, err
	}
	return RunRequest(req, cancel)
}

// cancelBody calls the per-request cancel func when the body is closed.
type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelBody) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
