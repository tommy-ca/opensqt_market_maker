// Package risk provides risk management and safety monitoring
package risk

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
)

// RiskMonitor implements the IRiskMonitor interface
type RiskMonitor struct {
	exchange core.IExchange
	logger   core.ILogger

	// Configuration
	monitorSymbols    []string
	interval          string
	volumeMultiplier  float64
	averageWindow     int
	recoveryThreshold int
	globalStrategy    string // "Any" or "All"

	// State
	triggered   int32 // atomic bool
	symbolStats map[string]*SymbolStats

	// Lifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.RWMutex
	reportInt time.Duration
	pool      *concurrency.WorkerPool

	// Subscribers
	subscribers []chan<- *pb.RiskAlert
}

// SymbolStats tracks statistics for a monitored symbol
type SymbolStats struct {
	Candles       []*pb.Candle
	AverageVolume float64
	AveragePrice  float64
	LastPrice     decimal.Decimal
	IsTriggered   bool
	LastUpdate    time.Time
	ATR           decimal.Decimal
	PositionSize  decimal.Decimal
	NotionalValue decimal.Decimal
	UnrealizedPnL decimal.Decimal
	Leverage      decimal.Decimal
	RiskScore     decimal.Decimal
	mu            sync.RWMutex
}

// NewRiskMonitor creates a new risk monitor
func NewRiskMonitor(
	exchange core.IExchange,
	logger core.ILogger,
	monitorSymbols []string,
	interval string,
	volumeMultiplier float64,
	averageWindow int,
	recoveryThreshold int,
	globalStrategy string,
	pool *concurrency.WorkerPool,
) *RiskMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	if globalStrategy == "" {
		globalStrategy = "All" // Default to legacy behavior for stability
	}

	rm := &RiskMonitor{
		exchange:          exchange,
		logger:            logger.WithField("component", "risk_monitor"),
		monitorSymbols:    monitorSymbols,
		interval:          interval,
		volumeMultiplier:  volumeMultiplier,
		averageWindow:     averageWindow,
		recoveryThreshold: recoveryThreshold,
		globalStrategy:    globalStrategy,
		symbolStats:       make(map[string]*SymbolStats),
		ctx:               ctx,
		cancel:            cancel,
		reportInt:         60 * time.Second,
		pool:              pool,
	}

	// Initialize stats for each symbol immediately
	for _, symbol := range rm.monitorSymbols {
		rm.symbolStats[symbol] = &SymbolStats{
			Candles: make([]*pb.Candle, 0, rm.averageWindow+1),
		}
	}

	return rm
}

// Start begins risk monitoring
func (rm *RiskMonitor) Start(ctx context.Context) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.logger.Info("Starting risk monitor",
		"symbols", rm.monitorSymbols,
		"interval", rm.interval,
		"multiplier", rm.volumeMultiplier,
		"strategy", rm.globalStrategy)

	// Preload historical data
	rm.preloadHistory(ctx)

	// Start K-line stream
	err := rm.exchange.StartKlineStream(ctx, rm.monitorSymbols, rm.interval, rm.handleKlineUpdate)
	if err != nil {
		return fmt.Errorf("failed to start kline stream: %w", err)
	}

	// Start reporting loop
	go rm.reportLoop()

	return nil
}

func (rm *RiskMonitor) preloadHistory(ctx context.Context) {
	for _, symbol := range rm.monitorSymbols {
		candles, err := rm.exchange.GetHistoricalKlines(ctx, symbol, rm.interval, rm.averageWindow+1)
		if err != nil {
			rm.logger.Warn("Failed to preload history", "symbol", symbol, "error", err)
			continue
		}

		if len(candles) > 0 {
			stats := rm.symbolStats[symbol]
			stats.mu.Lock()
			stats.Candles = candles
			stats.mu.Unlock()
			rm.logger.Info("Preloaded history", "symbol", symbol, "count", len(candles))
		}
	}
}

// Stop stops risk monitoring
func (rm *RiskMonitor) Stop() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.logger.Info("Stopping risk monitor")
	rm.cancel()

	return rm.exchange.StopKlineStream()
}

// CheckHealth returns an error if the risk monitor is unhealthy
func (rm *RiskMonitor) CheckHealth() error {
	if rm.ctx.Err() != nil {
		return fmt.Errorf("risk monitor context cancelled")
	}

	if rm.IsTriggered() {
		return fmt.Errorf("risk monitor is in TRIGGERED state")
	}

	return nil
}

// IsTriggered returns true if risk controls are triggered
func (rm *RiskMonitor) IsTriggered() bool {
	return atomic.LoadInt32(&rm.triggered) == 1
}

func (rm *RiskMonitor) GetVolatilityFactor(symbol string) float64 {
	return 0.0 // Placeholder
}

func (rm *RiskMonitor) GetATR(symbol string) decimal.Decimal {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if stats, exists := rm.symbolStats[symbol]; exists {
		stats.mu.RLock()
		defer stats.mu.RUnlock()
		return stats.ATR
	}
	return decimal.Zero
}

// GetAllSymbols returns the list of monitored symbols
func (rm *RiskMonitor) GetAllSymbols() []string {
	return rm.monitorSymbols
}

// GetMetrics returns risk metrics for a specific symbol as a protobuf message
func (rm *RiskMonitor) GetMetrics(symbol string) *pb.SymbolRiskMetrics {
	rm.mu.RLock()
	stats, exists := rm.symbolStats[symbol]
	rm.mu.RUnlock()

	if !exists {
		return nil
	}

	stats.mu.RLock()
	defer stats.mu.RUnlock()

	return &pb.SymbolRiskMetrics{
		Symbol:        symbol,
		LimitBreach:   stats.IsTriggered,
		LimitType:     "volatility_anomaly",
		RiskScore:     pbu.FromGoDecimal(stats.RiskScore),
		PositionSize:  pbu.FromGoDecimal(stats.PositionSize),
		NotionalValue: pbu.FromGoDecimal(stats.NotionalValue),
		UnrealizedPnl: pbu.FromGoDecimal(stats.UnrealizedPnL),
		Leverage:      pbu.FromGoDecimal(stats.Leverage),
	}
}

// GetInternalStats returns the internal statistics for a specific symbol
func (rm *RiskMonitor) GetInternalStats(symbol string) *SymbolStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.symbolStats[symbol]
}

// Reset clears the triggered state of the risk monitor
func (rm *RiskMonitor) Reset() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	atomic.StoreInt32(&rm.triggered, 0)
	for _, stats := range rm.symbolStats {
		stats.mu.Lock()
		stats.IsTriggered = false
		stats.mu.Unlock()
	}

	rm.logger.Info("Risk monitor reset manually")
	rm.broadcastAlert(&pb.RiskAlert{
		AlertType: "risk_monitor_reset",
		Severity:  "info",
		Message:   "Risk monitor reset manually.",
		Timestamp: time.Now().Unix(),
	})
	return nil
}

// GetTriggeredSymbols returns a list of symbols currently triggering risk controls
func (rm *RiskMonitor) GetTriggeredSymbols() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var triggered []string
	for symbol, stats := range rm.symbolStats {
		stats.mu.RLock()
		if stats.IsTriggered {
			triggered = append(triggered, symbol)
		}
		stats.mu.RUnlock()
	}
	return triggered
}

// Subscribe adds a channel to receive risk alerts
func (rm *RiskMonitor) Subscribe(ch chan<- *pb.RiskAlert) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.subscribers = append(rm.subscribers, ch)
}

// Unsubscribe removes a channel from risk alerts
func (rm *RiskMonitor) Unsubscribe(ch chan<- *pb.RiskAlert) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for i, sub := range rm.subscribers {
		if sub == ch {
			rm.subscribers = append(rm.subscribers[:i], rm.subscribers[i+1:]...)
			break
		}
	}
}

func (rm *RiskMonitor) broadcastAlert(alert *pb.RiskAlert) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	for _, sub := range rm.subscribers {
		select {
		case sub <- alert:
		default:
			// Subscriber slow, skip or handle overflow
		}
	}
}

// HandleKlineUpdate processes incoming K-line data (exported for testing)
func (rm *RiskMonitor) HandleKlineUpdate(candle *pb.Candle) {
	rm.handleKlineUpdate(candle)
}

// handleKlineUpdate processes incoming K-line data
func (rm *RiskMonitor) handleKlineUpdate(candle *pb.Candle) {
	if candle == nil {
		return
	}

	rm.mu.RLock()
	stats, exists := rm.symbolStats[candle.Symbol]
	rm.mu.RUnlock()

	if !exists {
		return
	}

	stats.mu.Lock()
	stats.LastPrice = pbu.ToGoDecimal(candle.Close)
	if candle.IsClosed {
		// Replace or append if it's a new closed candle
		if len(stats.Candles) > 0 && !stats.Candles[len(stats.Candles)-1].IsClosed {
			// Replace the previous unclosed candle
			stats.Candles[len(stats.Candles)-1] = candle
		} else {
			stats.Candles = append(stats.Candles, candle)
		}

		// Keep window size
		if len(stats.Candles) > rm.averageWindow+1 {
			stats.Candles = stats.Candles[len(stats.Candles)-(rm.averageWindow+1):]
		}
	} else {
		// Unclosed candle: update last or append
		if len(stats.Candles) > 0 && !stats.Candles[len(stats.Candles)-1].IsClosed {
			stats.Candles[len(stats.Candles)-1] = candle
		} else {
			stats.Candles = append(stats.Candles, candle)
		}
	}
	stats.LastUpdate = time.Now()
	stats.mu.Unlock()

	// Check for anomaly
	rm.checkAnomaly(candle.Symbol)

	// Calculate ATR
	rm.calculateATR(candle.Symbol)

	// Update global trigger state
	if rm.pool != nil {
		rm.pool.Submit(rm.updateGlobalTriggerState)
	} else {
		go rm.updateGlobalTriggerState()
	}
}

func (rm *RiskMonitor) checkAnomaly(symbol string) {
	rm.mu.RLock()
	stats := rm.symbolStats[symbol]
	rm.mu.RUnlock()

	stats.mu.Lock()
	defer stats.mu.Unlock()

	if len(stats.Candles) < rm.averageWindow+1 {
		return
	}

	currentCandle := stats.Candles[len(stats.Candles)-1]
	currentPrice := pbu.ToGoDecimal(currentCandle.Close).InexactFloat64()
	currentVolume := pbu.ToGoDecimal(currentCandle.Volume).InexactFloat64()

	var sumVol, sumPrice float64
	var count int
	for i := len(stats.Candles) - 2; i >= 0 && count < rm.averageWindow; i-- {
		if stats.Candles[i].IsClosed {
			sumVol += pbu.ToGoDecimal(stats.Candles[i].Volume).InexactFloat64()
			sumPrice += pbu.ToGoDecimal(stats.Candles[i].Close).InexactFloat64()
			count++
		}
	}

	if count < rm.averageWindow {
		return
	}

	avgVol := sumVol / float64(count)
	avgPrice := sumPrice / float64(count)

	stats.AverageVolume = avgVol
	stats.AveragePrice = avgPrice

	isVolumeSpike := currentVolume > avgVol*rm.volumeMultiplier
	isPriceDrop := currentPrice < avgPrice

	isAnomaly := isVolumeSpike && isPriceDrop
	stats.IsTriggered = isAnomaly

	if isAnomaly {
		rm.logger.Warn("Risk anomaly detected",
			"symbol", symbol,
			"volume", currentVolume,
			"avg_volume", avgVol,
			"price", currentPrice,
			"avg_price", avgPrice,
			"closed", currentCandle.IsClosed)

		rm.broadcastAlert(&pb.RiskAlert{
			Symbol:    symbol,
			AlertType: "volatility_anomaly",
			Severity:  "warning",
			Message:   fmt.Sprintf("Volatility anomaly detected for %s: Volume spike (%.2fx avg) and price drop", symbol, currentVolume/avgVol),
			Timestamp: time.Now().Unix(),
			Metadata: map[string]string{
				"volume":     fmt.Sprintf("%.2f", currentVolume),
				"avg_volume": fmt.Sprintf("%.2f", avgVol),
				"price":      fmt.Sprintf("%.2f", currentPrice),
				"avg_price":  fmt.Sprintf("%.2f", avgPrice),
			},
		})
	}
}

func (rm *RiskMonitor) calculateATR(symbol string) {
	rm.mu.RLock()
	stats := rm.symbolStats[symbol]
	rm.mu.RUnlock()

	stats.mu.Lock()
	defer stats.mu.Unlock()

	if len(stats.Candles) < rm.averageWindow+1 {
		return
	}

	// Calculate True Range for the last N candles
	// TR = Max(H-L, Abs(H-PrevClose), Abs(L-PrevClose))
	// ATR = SMA(TR, Window)

	var trSum decimal.Decimal
	count := 0

	for i := len(stats.Candles) - 1; i > 0 && count < rm.averageWindow; i-- {
		current := stats.Candles[i]
		prev := stats.Candles[i-1]

		// Ensure we only use closed candles for history, or allow latest live candle?
		// Usually ATR is based on closed candles.
		// If current is not closed, we might want to skip it or use it as partial.
		// Let's use closed candles only for stability.
		if !current.IsClosed {
			continue
		}

		high := pbu.ToGoDecimal(current.High)
		low := pbu.ToGoDecimal(current.Low)
		prevClose := pbu.ToGoDecimal(prev.Close)

		tr1 := high.Sub(low)
		tr2 := high.Sub(prevClose).Abs()
		tr3 := low.Sub(prevClose).Abs()

		tr := tr1
		if tr2.GreaterThan(tr) {
			tr = tr2
		}
		if tr3.GreaterThan(tr) {
			tr = tr3
		}

		trSum = trSum.Add(tr)
		count++
	}

	if count > 0 {
		stats.ATR = trSum.Div(decimal.NewFromInt(int64(count)))
	}
}

// updateGlobalTriggerState checks all symbols and updates the global triggered state
func (rm *RiskMonitor) updateGlobalTriggerState() {
	rm.mu.RLock()
	triggeredCount := 0
	totalCount := len(rm.symbolStats)
	for _, s := range rm.symbolStats {
		s.mu.RLock()
		if s.IsTriggered {
			triggeredCount++
		}
		s.mu.RUnlock()
	}
	rm.mu.RUnlock()

	isCurrentlyTriggered := rm.IsTriggered()

	var shouldTrigger bool
	if rm.globalStrategy == "Any" {
		shouldTrigger = triggeredCount > 0
	} else {
		// Default to "All" - matches legacy exactly
		shouldTrigger = triggeredCount >= totalCount && totalCount > 0
	}

	if shouldTrigger && !isCurrentlyTriggered {
		rm.logger.Warn("Global risk triggered", "strategy", rm.globalStrategy, "triggered_count", triggeredCount)
		atomic.StoreInt32(&rm.triggered, 1)

		rm.broadcastAlert(&pb.RiskAlert{
			AlertType: "global_risk_triggered",
			Severity:  "critical",
			Message:   fmt.Sprintf("Global risk triggered using strategy %s. %d symbols triggered.", rm.globalStrategy, triggeredCount),
			Timestamp: time.Now().Unix(),
		})
	} else if !shouldTrigger && isCurrentlyTriggered {
		// Check recovery
		normalCount := totalCount - triggeredCount
		if normalCount >= rm.recoveryThreshold {
			rm.logger.Info("Global risk cleared", "normal_count", normalCount, "threshold", rm.recoveryThreshold)
			atomic.StoreInt32(&rm.triggered, 0)

			rm.broadcastAlert(&pb.RiskAlert{
				AlertType: "global_risk_cleared",
				Severity:  "info",
				Message:   "Global risk cleared.",
				Timestamp: time.Now().Unix(),
			})
		}
	}
}

func (rm *RiskMonitor) reportLoop() {
	ticker := time.NewTicker(rm.reportInt)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.reportStatus()
		}
	}
}

func (rm *RiskMonitor) reportStatus() {
	isTriggered := rm.IsTriggered()
	if isTriggered {
		rm.logger.Warn("Risk monitor status: TRIGGERED")
	} else {
		rm.logger.Info("Risk monitor status: Normal")
	}

	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for symbol, stats := range rm.symbolStats {
		stats.mu.RLock()
		if len(stats.Candles) > 0 {
			last := stats.Candles[len(stats.Candles)-1]
			rm.logger.Info("Symbol Status",
				"symbol", symbol,
				"price", last.Close,
				"avg_price", stats.AveragePrice,
				"vol", last.Volume,
				"avg_vol", stats.AverageVolume,
				"triggered", stats.IsTriggered)

			// Report metrics
			metrics := telemetry.GetGlobalMetrics()
			metrics.SetRiskTriggered(symbol, stats.IsTriggered)
		}
		stats.mu.RUnlock()
	}
}
