package monitor

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"sync"
)

// RegimeMonitor tracks market conditions and determines the current trading regime
type RegimeMonitor struct {
	exchange core.IExchange
	logger   core.ILogger
	symbol   string

	currentRegime pb.MarketRegime
	mu            sync.RWMutex
}

func NewRegimeMonitor(exch core.IExchange, logger core.ILogger, symbol string) *RegimeMonitor {
	return &RegimeMonitor{
		exchange:      exch,
		logger:        logger.WithField("component", "regime_monitor").WithField("symbol", symbol),
		symbol:        symbol,
		currentRegime: pb.MarketRegime_MARKET_REGIME_RANGE,
	}
}

func (rm *RegimeMonitor) Start(ctx context.Context) error {
	rm.logger.Info("Starting Regime Monitor")

	// Subscribe to Klines for indicator calculation
	// Standard 1m klines
	err := rm.exchange.StartKlineStream(ctx, []string{rm.symbol}, "1m", func(candle *pb.Candle) {
		rm.handleKlineUpdate(candle)
	})
	if err != nil {
		return err
	}

	return nil
}

func (rm *RegimeMonitor) GetRegime() pb.MarketRegime {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.currentRegime
}

func (rm *RegimeMonitor) handleKlineUpdate(candle *pb.Candle) {
	if !candle.IsClosed {
		return
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Simplified Regime Detection Logic
	// RSI > 70 -> Bull Trend
	// RSI < 30 -> Bear Trend
	// Else -> Range

	// Placeholder for real RSI calculation
	// For now, let's just use price move relative to 20-period SMA?
	// Or just keep it as RANGE unless we have real math.

	// TODO: Implement lightweight Technical Analysis (RSI/SMA)

	newRegime := pb.MarketRegime_MARKET_REGIME_RANGE

	// Log transition
	if newRegime != rm.currentRegime {
		rm.logger.Warn("Regime Changed", "old", rm.currentRegime.String(), "new", newRegime.String())
		rm.currentRegime = newRegime
	}
}

func (rm *RegimeMonitor) UpdateFromIndicators(rsi float64, trendScore float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	newRegime := pb.MarketRegime_MARKET_REGIME_RANGE
	if rsi > 70 {
		newRegime = pb.MarketRegime_MARKET_REGIME_BULL_TREND
	} else if rsi < 30 {
		newRegime = pb.MarketRegime_MARKET_REGIME_BEAR_TREND
	}

	if newRegime != rm.currentRegime {
		rm.logger.Warn("Regime Changed (Manual Update)", "old", rm.currentRegime.String(), "new", newRegime.String())
		rm.currentRegime = newRegime
	}
}
