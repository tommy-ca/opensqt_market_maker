// Package monitor provides price monitoring functionality with WebSocket support
package monitor

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
)

// PriceMonitor implements the IPriceMonitor interface
type PriceMonitor struct {
	exchange core.IExchange
	symbol   string
	logger   core.ILogger

	// Price storage (atomic for concurrent access)
	lastPrice       atomic.Value // holds decimal.Decimal
	lastPriceChange atomic.Value // holds *pb.PriceChange
	isRunning       int32        // atomic bool

	// Price change broadcasting
	subscribers []chan *pb.PriceChange

	// Configuration
	sendInterval time.Duration

	// Lifecycle control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Connection management
	reconnectDelay time.Duration
	maxRetries     int

	mu sync.RWMutex

	// Health status
	lastUpdate atomic.Value // holds time.Time
}

// NewPriceMonitor creates a new price monitor instance
func NewPriceMonitor(exchange core.IExchange, symbol string, logger core.ILogger) *PriceMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	pm := &PriceMonitor{
		exchange:       exchange,
		symbol:         symbol,
		logger:         logger.WithField("component", "price_monitor").WithField("symbol", symbol),
		subscribers:    make([]chan *pb.PriceChange, 0),
		sendInterval:   50 * time.Millisecond,
		ctx:            ctx,
		cancel:         cancel,
		reconnectDelay: 5 * time.Second,
		maxRetries:     10,
	}
	pm.lastPrice.Store(decimal.Zero)
	pm.lastPriceChange.Store((*pb.PriceChange)(nil))
	pm.lastUpdate.Store(time.Time{})
	return pm
}

// Start begins price monitoring
func (pm *PriceMonitor) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.isRunningAtomic() {
		return fmt.Errorf("price monitor is already running")
	}

	pm.logger.Info("Starting price monitor")
	atomic.StoreInt32(&pm.isRunning, 1)

	pm.wg.Add(1)
	go pm.priceMonitoringLoop(ctx)

	pm.wg.Add(1)
	go pm.priceBroadcastingLoop(ctx)

	pm.logger.Info("Price monitor started successfully")
	return nil
}

// Stop stops price monitoring
func (pm *PriceMonitor) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.isRunningAtomic() {
		return nil
	}

	pm.logger.Info("Stopping price monitor")
	atomic.StoreInt32(&pm.isRunning, 0)
	pm.cancel()

	done := make(chan struct{})
	go func() {
		pm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		pm.logger.Info("Price monitor stopped successfully")
	case <-time.After(10 * time.Second):
		pm.logger.Warn("Price monitor stop timed out")
	}

	return nil
}

// GetLatestPrice returns the latest price atomically
func (pm *PriceMonitor) GetLatestPrice() (decimal.Decimal, error) {
	price := pm.lastPrice.Load().(decimal.Decimal)
	if price.IsZero() {
		return decimal.Zero, fmt.Errorf("no price available")
	}
	return price, nil
}

// GetLatestPriceChange returns the latest price change
func (pm *PriceMonitor) GetLatestPriceChange() (*pb.PriceChange, error) {
	val := pm.lastPriceChange.Load()
	if val == nil {
		return nil, fmt.Errorf("no price change available")
	}
	change := val.(*pb.PriceChange)
	if change == nil {
		return nil, fmt.Errorf("no price change available")
	}
	return change, nil
}

// SubscribePriceChanges returns a channel for price change notifications
func (pm *PriceMonitor) SubscribePriceChanges() <-chan *pb.PriceChange {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	ch := make(chan *pb.PriceChange, 10)
	pm.subscribers = append(pm.subscribers, ch)

	return ch
}

// priceMonitoringLoop handles WebSocket connection and price updates
func (pm *PriceMonitor) priceMonitoringLoop(ctx context.Context) {
	defer pm.wg.Done()
	pm.logger.Info("Starting price monitoring loop")

	for {
		select {
		case <-ctx.Done():
			pm.logger.Info("Price monitoring loop stopped")
			return
		default:
			if err := pm.connectAndMonitor(ctx); err != nil {
				pm.logger.Error("Price monitoring failed", "error", err.Error())
				select {
				case <-ctx.Done():
					return
				case <-time.After(pm.reconnectDelay):
					continue
				}
			}
		}
	}
}

// connectAndMonitor establishes WebSocket connection and monitors price updates
func (pm *PriceMonitor) connectAndMonitor(ctx context.Context) error {
	pm.logger.Info("Connecting to price stream")
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	priceCh := make(chan *pb.PriceChange, 100)
	err := pm.exchange.StartPriceStream(connCtx, []string{pm.symbol}, func(change *pb.PriceChange) {
		select {
		case priceCh <- change:
		default:
			pm.logger.Warn("Price change channel full, dropping update")
		}
	})

	if err != nil {
		return fmt.Errorf("failed to start price stream: %w", err)
	}

	pm.logger.Info("Price stream connected successfully")

	for {
		select {
		case <-connCtx.Done():
			pm.logger.Info("Price monitoring connection cancelled")
			return nil
		case priceChange, ok := <-priceCh:
			if !ok {
				pm.logger.Warn("Price channel closed")
				return fmt.Errorf("price channel closed")
			}
			pm.handlePriceUpdate(priceChange)
		}
	}
}

// handlePriceUpdate processes incoming price updates
func (pm *PriceMonitor) handlePriceUpdate(change *pb.PriceChange) {
	pm.lastPrice.Store(pbu.ToGoDecimal(change.Price))
	pm.lastPriceChange.Store(change)
	pm.lastUpdate.Store(time.Now())

	pm.logger.Debug("Price updated", "price", change.Price, "timestamp", change.Timestamp)
}

// priceBroadcastingLoop broadcasts price changes to subscribers
func (pm *PriceMonitor) priceBroadcastingLoop(ctx context.Context) {
	defer pm.wg.Done()
	pm.logger.Info("Starting price broadcasting loop")

	ticker := time.NewTicker(pm.sendInterval)
	defer ticker.Stop()

	var lastBroadcast *pb.PriceChange

	for {
		select {
		case <-ctx.Done():
			pm.logger.Info("Price broadcasting loop stopped")
			return
		case <-ticker.C:
			if !pm.isRunningAtomic() {
				continue
			}
			priceChange, err := pm.GetLatestPriceChange()
			if err != nil {
				continue
			}

			// Broadcast only if changed (pointer check is sufficient as we store new pointer on update)
			// Or check content if pointer reused (unlikely given handlePriceUpdate creates new struct implicitly passed by value?
			// Wait, handlePriceUpdate receives value, takes address `&change`.
			// `change` is a local copy. So address is distinct stack address?
			// `Store(&change)` stores pointer to stack variable? UNSAFE!
			// Go compiler might heap allocate it if it escapes.
			// But to be safe and logic correct: check timestamp or price equality.

			shouldBroadcast := false
			if lastBroadcast == nil {
				shouldBroadcast = true
			} else if priceChange != lastBroadcast {
				// Different pointer? Or deep compare?
				// If we store pointer, and `handlePriceUpdate` stores new pointer.
				// Yes.
				shouldBroadcast = true
			}

			if shouldBroadcast {
				pm.broadcastPriceChange(priceChange)
				lastBroadcast = priceChange
			}
		}
	}
}

// broadcastPriceChange sends price change to all subscribers
func (pm *PriceMonitor) broadcastPriceChange(change *pb.PriceChange) {
	pm.mu.RLock()
	subscribers := make([]chan *pb.PriceChange, len(pm.subscribers))
	copy(subscribers, pm.subscribers)
	pm.mu.RUnlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- change:
		default:
			pm.logger.Warn("Subscriber channel full, dropping price change")
		}
	}
}

// CheckHealth returns an error if the price monitor is unhealthy
func (pm *PriceMonitor) CheckHealth() error {
	if !pm.isRunningAtomic() {
		return fmt.Errorf("price monitor is not running")
	}

	last := pm.lastUpdate.Load().(time.Time)
	if last.IsZero() {
		return fmt.Errorf("no price updates received yet")
	}

	if time.Since(last) > 1*time.Minute {
		return fmt.Errorf("stale price data: last update %s ago", time.Since(last))
	}

	return nil
}

// isRunningAtomic returns the running state atomically
func (pm *PriceMonitor) isRunningAtomic() bool {
	return atomic.LoadInt32(&pm.isRunning) == 1
}
