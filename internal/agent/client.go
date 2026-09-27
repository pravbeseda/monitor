package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
)

// ingestPath is versioned like every endpoint the hub serves.
const ingestPath = "/api/v1/ingest"

// retryable tells a batch worth keeping from one that will never be accepted: a refused
// shape stays refused, while a busy or broken hub recovers.
func retryable(err error) bool {
	var status api.StatusError
	if errors.As(err, &status) {
		return status.Status == http.StatusTooManyRequests || status.Status >= http.StatusInternalServerError
	}
	return true
}

// HTTPClient posts to one hub with one node's token.
type HTTPClient struct {
	url   string
	token string
	http  *http.Client
}

// NewHTTPClient builds the client from the agent's two deployment settings.
func NewHTTPClient(hubURL, token string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		url:   strings.TrimSuffix(hubURL, "/") + ingestPath,
		token: token,
		http:  &http.Client{Timeout: timeout},
	}
}

// Send posts one request and returns the hub's answer, or an api.StatusError when the hub
// refused it.
func (c *HTTPClient) Send(ctx context.Context, request api.Request) (api.Response, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return api.Response{}, fmt.Errorf("encode the request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return api.Response{}, fmt.Errorf("build the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return api.Response{}, fmt.Errorf("post to %s: %w", c.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var refusal api.ErrorBody
		_ = json.NewDecoder(resp.Body).Decode(&refusal)
		return api.Response{}, api.StatusError{Status: resp.StatusCode, Message: refusal.Error}
	}

	var answer api.Response
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return api.Response{}, fmt.Errorf("decode the hub's answer: %w", err)
	}
	return answer, nil
}
