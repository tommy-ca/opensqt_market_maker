// Package base provides common functionality for exchange adapters
package base

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/websocket"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// SignRequestFunc is a function type for exchange-specific request signing
type SignRequestFunc func(req *http.Request, body []byte) error

// ParseErrorFunc is a function type for exchange-specific error parsing
type ParseErrorFunc func(body []byte) error

// MapOrderStatusFunc is a function type for exchange-specific order status mapping
type MapOrderStatusFunc func(rawStatus string) pb.OrderStatus

// BaseAdapter provides common functionality for all exchange adapters
type BaseAdapter struct {
	Name       string
	Config     *config.ExchangeConfig
	Logger     core.ILogger
	HTTPClient *http.Client

	// Exchange-specific functions to be set by concrete implementations
	SignRequestFunc MapSignRequestFunc
	ParseError      ParseErrorFunc
	MapOrderStatus  MapOrderStatusFunc
}

// MapSignRequestFunc is a function type for exchange-specific request signing
type MapSignRequestFunc func(req *http.Request, body []byte) error

// NewBaseAdapter creates a new base adapter with common configuration
func NewBaseAdapter(name string, cfg *config.ExchangeConfig, logger core.ILogger) *BaseAdapter {
	return &BaseAdapter{
		Name:   name,
		Config: cfg,
		Logger: logger.WithField("exchange", name),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		},
	}
}

// GetName returns the exchange name
func (b *BaseAdapter) GetName() string {
	return b.Name
}

// SetSignRequest sets the exchange-specific request signing function
func (b *BaseAdapter) SetSignRequest(fn MapSignRequestFunc) {
	b.SignRequestFunc = fn
}

// SetParseError sets the exchange-specific error parsing function
func (b *BaseAdapter) SetParseError(fn ParseErrorFunc) {
	b.ParseError = fn
}

// SetMapOrderStatus sets the exchange-specific order status mapping function
func (b *BaseAdapter) SetMapOrderStatus(fn MapOrderStatusFunc) {
	b.MapOrderStatus = fn
}

// GetConfig returns the exchange configuration
func (b *BaseAdapter) GetConfig() *config.ExchangeConfig {
	return b.Config
}

// GetLogger returns the logger instance
func (b *BaseAdapter) GetLogger() core.ILogger {
	return b.Logger
}

// GetHTTPClient returns the HTTP client instance
func (b *BaseAdapter) GetHTTPClient() *http.Client {
	return b.HTTPClient
}

// ExecuteRequest executes an HTTP request with common error handling
func (b *BaseAdapter) ExecuteRequest(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Call exchange-specific signing if set
	if b.SignRequestFunc != nil {
		if err := b.SignRequestFunc(req, body); err != nil {
			return nil, fmt.Errorf("failed to sign request: %w", err)
		}
	}

	resp, err := b.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Call exchange-specific error parser if set
		if b.ParseError != nil {
			if parseErr := b.ParseError(respBody); parseErr != nil {
				return nil, parseErr
			}
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// StartPollingStream starts a generic polling-based stream
func (b *BaseAdapter) StartPollingStream(
	ctx context.Context,
	fetchFunc func(context.Context) (interface{}, error),
	callback func(interface{}),
	interval time.Duration,
	streamName string,
) error {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				b.Logger.Info(streamName+" stream stopped", "reason", ctx.Err())
				return
			case <-ticker.C:
				data, err := fetchFunc(ctx)
				if err != nil {
					b.Logger.Warn(streamName+" polling failed", "error", err)
					continue
				}
				callback(data)
			}
		}
	}()

	b.Logger.Info(streamName + " stream started")
	return nil
}

// StartWebSocketStream starts a WebSocket stream with common lifecycle management
func (b *BaseAdapter) StartWebSocketStream(
	ctx context.Context,
	wsURL string,
	onMessage func([]byte),
	onConnected func(),
	streamName string,
) error {
	client := websocket.NewClient(wsURL, onMessage, b.Logger)

	if onConnected != nil {
		client.SetOnConnected(onConnected)
	}

	client.Start()

	go func() {
		<-ctx.Done()
		b.Logger.Info(streamName + " WebSocket stopping")
		client.Stop()
	}()

	b.Logger.Info(streamName + " WebSocket started")
	return nil
}

// SafeMapOrderStatus maps exchange-specific order status to protobuf status
func (b *BaseAdapter) SafeMapOrderStatus(rawStatus string) pb.OrderStatus {
	if b.MapOrderStatus != nil {
		return b.MapOrderStatus(rawStatus)
	}
	// Default mapping if not set
	return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
}

// ParseDecimal safely parses a string to decimal
func (b *BaseAdapter) ParseDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		b.Logger.Warn("failed to parse decimal", "value", s, "error", err)
		return decimal.Zero
	}
	return d
}

// ParseTimestamp safely parses a timestamp in milliseconds
func (b *BaseAdapter) ParseTimestamp(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
