// Package order provides order execution functionality with rate limiting and retry logic
package order

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/telemetry"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// OrderExecutor implements the IOrderExecutor interface
type OrderExecutor struct {
	exchange core.IExchange
	logger   core.ILogger

	// Rate limiting (25 orders/second with burst capacity)
	rateLimiter *rate.Limiter

	// Retry configuration
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration

	// Post-only degradation
	postOnlyMaxFailures int

	// Lifecycle control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu sync.RWMutex

	// Health status
	lastFailure     atomic.Value // holds time.Time
	errorTimestamps []time.Time
	errorIndex      int // Current write index for ring buffer
	errorCapacity   int // Max capacity for error tracking
	errorMu         sync.Mutex
	stopChan        chan struct{}

	// OTel
	tracer       trace.Tracer
	orderCounter metric.Int64Counter
	retryCounter metric.Int64Counter
	failCounter  metric.Int64Counter
}

// NewOrderExecutor creates a new order executor instance
func NewOrderExecutor(exchange core.IExchange, logger core.ILogger) *OrderExecutor {
	ctx, cancel := context.WithCancel(context.Background())

	tracer := telemetry.GetTracer("order-executor")
	meter := telemetry.GetMeter("order-executor")

	orderCounter, _ := meter.Int64Counter("order_placements_total",
		metric.WithDescription("Total number of orders placed"))
	retryCounter, _ := meter.Int64Counter("order_retries_total",
		metric.WithDescription("Total number of order placement retries"))
	failCounter, _ := meter.Int64Counter("order_failures_total",
		metric.WithDescription("Total number of order placement failures"))

	oe := &OrderExecutor{
		exchange:            exchange,
		logger:              logger.WithField("component", "order_executor"),
		rateLimiter:         rate.NewLimiter(rate.Limit(25), 30), // 25/sec with burst of 30
		maxRetries:          5,
		baseDelay:           500 * time.Millisecond, // 500ms base delay
		maxDelay:            10 * time.Second,       // Max 10s delay
		postOnlyMaxFailures: 3,
		ctx:                 ctx,
		cancel:              cancel,
		tracer:              tracer,
		orderCounter:        orderCounter,
		retryCounter:        retryCounter,
		failCounter:         failCounter,
		errorCapacity:       1000,
		errorTimestamps:     make([]time.Time, 0, 1000),
		stopChan:            make(chan struct{}),
	}
	oe.lastFailure.Store(time.Time{})
	return oe
}

// SetRateLimit updates the rate limit
func (oe *OrderExecutor) SetRateLimit(limit float64, burst int) {
	oe.mu.Lock()
	defer oe.mu.Unlock()
	oe.rateLimiter = rate.NewLimiter(rate.Limit(limit), burst)
}

// PlaceOrder places a single order with rate limiting and retry logic
func (oe *OrderExecutor) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	ctx, span := oe.tracer.Start(ctx, "PlaceOrder",
		trace.WithAttributes(
			attribute.String("symbol", req.Symbol),
			attribute.String("side", string(req.Side)),
		),
	)
	defer span.End()

	oe.mu.RLock()
	execCtx := oe.ctx
	oe.mu.RUnlock()

	if execCtx == nil {
		return nil, fmt.Errorf("order executor is not initialized")
	}

	return oe.placeOrderWithRetry(ctx, req, 0)
}

// BatchPlaceOrders places multiple orders with rate limiting
func (oe *OrderExecutor) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	oe.mu.RLock()
	execCtx := oe.ctx
	oe.mu.RUnlock()

	if execCtx == nil {
		return nil, false
	}

	var results []*pb.Order
	marginError := false

	for _, req := range orders {
		order, err := oe.placeOrderWithRetry(ctx, req, 0)
		if err != nil {
			oe.logger.Error("Failed to place order in batch",
				"symbol", req.Symbol,
				"side", req.Side,
				"error", err.Error())

			// Check for margin error to fail fast
			if strings.Contains(err.Error(), "margin") || strings.Contains(err.Error(), "insufficient") {
				marginError = true
			}
			continue
		}

		if order != nil {
			results = append(results, order)
		}
	}

	return results, marginError
}

// BatchCancelOrders cancels multiple orders
func (oe *OrderExecutor) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	oe.mu.RLock()
	execCtx := oe.ctx
	oe.mu.RUnlock()

	if execCtx == nil {
		return fmt.Errorf("order executor is not initialized")
	}

	for _, orderID := range orderIDs {
		if err := oe.cancelOrderWithRetry(ctx, symbol, orderID, 0, useMargin); err != nil {
			oe.logger.Error("Failed to cancel order in batch",
				"symbol", symbol,
				"order_id", orderID,
				"error", err.Error())
		}
	}

	return nil
}

// CheckHealth returns an error if the order executor is unhealthy
func (oe *OrderExecutor) CheckHealth() error {
	oe.mu.RLock()
	defer oe.mu.RUnlock()

	if oe.ctx.Err() != nil {
		return fmt.Errorf("order executor context cancelled")
	}

	// Use updated ring buffer count method
	errCount := oe.getRecentErrorCount(5 * time.Minute)

	if errCount > 50 {
		return fmt.Errorf("high error rate: %d errors in last 5 minutes", errCount)
	}

	return nil
}

// SetErrorCapacity updates the maximum number of errors to track
func (oe *OrderExecutor) SetErrorCapacity(capacity int) {
	oe.errorMu.Lock()
	defer oe.errorMu.Unlock()

	if capacity <= 0 {
		capacity = 1000
	}

	// If decreasing capacity, we might need to truncate
	if len(oe.errorTimestamps) > capacity {
		oe.errorTimestamps = oe.errorTimestamps[:capacity]
	}

	oe.errorCapacity = capacity
	if oe.errorIndex >= capacity {
		oe.errorIndex = 0
	}
}

// recordError adds an error timestamp to track recent errors (Ring Buffer)
func (oe *OrderExecutor) recordError() {
	oe.errorMu.Lock()
	defer oe.errorMu.Unlock()

	// Safety fallback
	if oe.errorCapacity == 0 {
		oe.errorCapacity = 1000
	}

	if len(oe.errorTimestamps) < oe.errorCapacity {
		oe.errorTimestamps = append(oe.errorTimestamps, time.Now())
	} else {
		oe.errorTimestamps[oe.errorIndex] = time.Now()
		oe.errorIndex = (oe.errorIndex + 1) % oe.errorCapacity
	}
}

// getRecentErrorCount returns number of errors within duration
func (oe *OrderExecutor) getRecentErrorCount(duration time.Duration) int {
	oe.errorMu.Lock()
	defer oe.errorMu.Unlock()

	cutoff := time.Now().Add(-duration)
	count := 0
	for _, t := range oe.errorTimestamps {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

// placeOrderWithRetry attempts to place an order with retry logic
func (oe *OrderExecutor) placeOrderWithRetry(ctx context.Context, req *pb.PlaceOrderRequest, attempt int) (*pb.Order, error) {
	// Apply rate limiting
	if err := oe.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait failed: %w", err)
	}

	oe.orderCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("symbol", req.Symbol),
		attribute.String("side", req.Side.String()),
	))

	order, err := oe.exchange.PlaceOrder(ctx, req)
	if err == nil {
		return order, nil
	}

	// Log failure
	oe.logger.Warn("Order placement failed",
		"symbol", req.Symbol,
		"side", req.Side,
		"error", err.Error(),
		"attempt", attempt+1)

	oe.failCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("symbol", req.Symbol),
		attribute.String("side", req.Side.String()),
		attribute.String("error", err.Error()),
	))

	oe.recordError()

	// Check if we should retry
	if attempt >= oe.maxRetries {
		return nil, fmt.Errorf("max retries exceeded: %w", err)
	}

	// Check for fatal errors (non-retriable)
	if strings.Contains(err.Error(), "insufficient funds") || strings.Contains(err.Error(), "margin") || strings.Contains(err.Error(), "INVALID_SYMBOL") {
		return nil, err
	}

	// Handle Post-only rejection by retrying with non-post-only limit order if allowed
	if oe.isPostOnlyError(err) && req.PostOnly {
		oe.logger.Info("Post-only rejected, retrying with standard limit order", "symbol", req.Symbol)
		limitReq := pb.PlaceOrderRequest{
			Symbol:        req.Symbol,
			Side:          req.Side,
			Type:          req.Type,
			Quantity:      req.Quantity,
			Price:         req.Price,
			PostOnly:      false,
			TimeInForce:   pb.TimeInForce_TIME_IN_FORCE_GTC,
			ClientOrderId: req.ClientOrderId,
			UseMargin:     req.UseMargin,
		}

		return oe.placeOrderWithRetry(ctx, &limitReq, attempt+1)
	}

	// Calculate retry delay with exponential backoff
	delay := oe.calculateRetryDelay(attempt)
	oe.retryCounter.Add(ctx, 1)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(delay):
		return oe.placeOrderWithRetry(ctx, req, attempt+1)
	}
}

// cancelOrderWithRetry attempts to cancel an order with retry logic
func (oe *OrderExecutor) cancelOrderWithRetry(ctx context.Context, symbol string, orderID int64, attempt int, useMargin bool) error {
	// Apply rate limiting
	if err := oe.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait failed: %w", err)
	}

	oe.logger.Debug("Canceling order",
		"symbol", symbol,
		"order_id", orderID,
		"attempt", attempt+1)

	err := oe.exchange.CancelOrder(ctx, symbol, orderID, useMargin)
	if err == nil {
		oe.logger.Info("Order canceled successfully", "order_id", orderID)
		return nil
	}

	oe.logger.Warn("Order cancellation failed",
		"symbol", symbol,
		"order_id", orderID,
		"error", err.Error(),
		"attempt", attempt+1)

	// Check if we should retry
	if attempt >= oe.maxRetries {
		return fmt.Errorf("max cancel retries exceeded: %w", err)
	}

	// Check for fatal cancellation errors
	if strings.Contains(err.Error(), "ORDER_STATUS_FILLED") || strings.Contains(err.Error(), "already filled") || strings.Contains(err.Error(), "not found") {
		return err // Fatal
	}

	// Calculate retry delay
	delay := oe.calculateRetryDelay(attempt)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return oe.cancelOrderWithRetry(ctx, symbol, orderID, attempt+1, useMargin)
	}
}

// isPostOnlyError checks if an error is related to Post-only order rejection
func (oe *OrderExecutor) isPostOnlyError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// Common Post-only error messages from exchanges
	postOnlyErrors := []string{
		"postOnly",
		"POST_ONLY",
		"would execute immediately",
		"immediate execution",
		"market order",
		"51020",  // OKX: Order failed due to Post Only rule
		"170193", // Bybit: Buy order price is higher than best ask (PostOnly)
		"170194", // Bybit: Sell order price is lower than best bid (PostOnly)
	}

	for _, check := range postOnlyErrors {
		if strings.Contains(errStr, check) {
			return true
		}
	}

	return false
}

// calculateRetryDelay calculates exponential backoff delay
func (oe *OrderExecutor) calculateRetryDelay(attempt int) time.Duration {
	// min(baseDelay * 2^attempt, maxDelay) + jitter
	delay := float64(oe.baseDelay) * math.Pow(2, float64(attempt))
	if delay > float64(oe.maxDelay) {
		delay = float64(oe.maxDelay)
	}

	// Add random jitter (±10%)
	jitter := (rand.Float64()*0.2 - 0.1) * delay
	return time.Duration(delay + jitter)
}

func (oe *OrderExecutor) Stop() {
	oe.cancel()
	close(oe.stopChan)
}
