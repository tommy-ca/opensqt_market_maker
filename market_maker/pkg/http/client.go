// Package http provides a reusable HTTP client with resilience features
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"market_maker/pkg/telemetry"
	"net/http"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// APIError represents an API error response
type APIError struct {
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error: status=%d body=%s", e.StatusCode, string(e.Body))
}

// Signer is an interface for signing requests
type Signer interface {
	SignRequest(req *http.Request) error
}

// Client is a wrapper around http.Client with resilience
type Client struct {
	client   *http.Client
	baseURL  string
	signer   Signer
	pipeline failsafe.Executor[*http.Response]

	// OTel
	tracer      trace.Tracer
	reqCounter  metric.Int64Counter
	errCounter  metric.Int64Counter
	latencyHist metric.Float64Histogram
}

// NewClient creates a new HTTP client with default resilience policies
func NewClient(baseURL string, timeout time.Duration, signer Signer) *Client {
	// Define retry policy
	retryPolicy := retrypolicy.NewBuilder[*http.Response]().
		HandleIf(func(resp *http.Response, err error) bool {
			// Retry on network errors or 5xx server errors
			if err != nil {
				return true
			}
			return resp.StatusCode >= 500 || resp.StatusCode == 429
		}).
		WithBackoff(100*time.Millisecond, 2*time.Second).
		WithMaxRetries(3).
		Build()

	// Define circuit breaker
	breaker := circuitbreaker.NewBuilder[*http.Response]().
		HandleIf(func(resp *http.Response, err error) bool {
			// Open circuit on consecutive 5xx errors
			if err != nil {
				return true
			}
			return resp.StatusCode >= 500
		}).
		WithFailureThresholdRatio(5, 10). // 5 failures out of 10
		WithDelay(10 * time.Second).
		Build()

	tracer := telemetry.GetTracer("http-client")
	meter := telemetry.GetMeter("http-client")

	reqCounter, _ := meter.Int64Counter("http_requests_total",
		metric.WithDescription("Total number of HTTP requests"))
	errCounter, _ := meter.Int64Counter("http_errors_total",
		metric.WithDescription("Total number of HTTP errors"))
	latencyHist, _ := meter.Float64Histogram("http_request_duration_seconds",
		metric.WithDescription("HTTP request latency in seconds"))

	return &Client{
		client: &http.Client{
			Timeout: timeout,
		},
		baseURL:     baseURL,
		signer:      signer,
		pipeline:    failsafe.With[*http.Response](retryPolicy, breaker),
		tracer:      tracer,
		reqCounter:  reqCounter,
		errCounter:  errCounter,
		latencyHist: latencyHist,
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

// Put sends a PUT request
func (c *Client) Put(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, nil)
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
	start := time.Now()
	ctx := req.Context()

	ctx, span := c.tracer.Start(ctx, fmt.Sprintf("%s %s", req.Method, req.URL.Path),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
		),
	)
	defer span.End()

	// Update request with new context
	req = req.WithContext(ctx)

	// Sign request if signer is available
	if c.signer != nil {
		if err := c.signer.SignRequest(req); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to sign request: %w", err)
		}
	}

	// Execute request with resilience pipeline
	resp, err := c.pipeline.GetWithExecution(func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
		return c.client.Do(req)
	})

	duration := time.Since(start).Seconds()
	c.reqCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", req.Method),
		attribute.String("path", req.URL.Path),
	))
	c.latencyHist.Record(ctx, duration, metric.WithAttributes(
		attribute.String("method", req.Method),
		attribute.String("path", req.URL.Path),
	))

	if err != nil {
		span.RecordError(err)
		c.errCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("method", req.Method),
			attribute.String("path", req.URL.Path),
			attribute.String("error", "pipeline_failed"),
		))
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		c.errCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("method", req.Method),
			attribute.String("path", req.URL.Path),
			attribute.Int("status", resp.StatusCode),
		))
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       body,
		}
	}

	return body, nil
}
