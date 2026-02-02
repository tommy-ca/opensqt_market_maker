// Package http provides a reusable HTTP client with signing capabilities
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Signer is an interface for signing requests
type Signer interface {
	SignRequest(req *http.Request) error
}

// Client is a wrapper around http.Client with convenience methods
type Client struct {
	client  *http.Client
	baseURL string
	signer  Signer
}

// NewClient creates a new HTTP client
func NewClient(baseURL string, timeout time.Duration, signer Signer) *Client {
	return &Client{
		client: &http.Client{
			Timeout: timeout,
		},
		baseURL: baseURL,
		signer:  signer,
	}
}

// Get sends a GET request
func (c *Client) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()

	return c.do(req)
}

// Post sends a POST request
func (c *Client) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.do(req)
}

// Delete sends a DELETE request
func (c *Client) Delete(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()

	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	// Sign request if signer is available
	if c.signer != nil {
		if err := c.signer.SignRequest(req); err != nil {
			return nil, fmt.Errorf("failed to sign request: %w", err)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, string(body))
	}

	return body, nil
}
