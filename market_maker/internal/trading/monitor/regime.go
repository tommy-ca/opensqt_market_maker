package monitor

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"sync"

	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// Standard periods for indicators
	rsiPeriod   = 14
	atrPeriod   = 14
	historySize = 100 // Keep enough history for calculations
)

// RegimeMonitor tracks market conditions and determines the current trading regime
type RegimeMonitor struct {
	exchange core.IExchange
	logger   core.ILogger
	symbol   string

	currentRegime pb.MarketRegime
	mu            sync.RWMutex

	// Indicators
	candles []*pb.Candle
	rsi     decimal.Decimal
	atr     decimal.Decimal
}

func NewRegimeMonitor(exch core.IExchange, logger core.ILogger, symbol string) *RegimeMonitor {
	return &RegimeMonitor{
		exchange:      exch,
		logger:        logger.WithField("component", "regime_monitor").WithField("symbol", symbol),
		symbol:        symbol,
		currentRegime: pb.MarketRegime_MARKET_REGIME_RANGE,
		candles:       make([]*pb.Candle, 0, historySize),
		rsi:           decimal.Zero,
		atr:           decimal.Zero,
	}
}

func (rm *RegimeMonitor) Start(ctx context.Context) error {
	rm.logger.Info("Starting Regime Monitor")

	// Preload historical data
	rm.preloadHistory(ctx)

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

func (rm *RegimeMonitor) preloadHistory(ctx context.Context) {
	// Fetch enough candles to calculate initial indicators
	// We need at least historySize
	candles, err := rm.exchange.GetHistoricalKlines(ctx, rm.symbol, "1m", historySize)
	if err != nil {
		rm.logger.Warn("Failed to preload history for RegimeMonitor", "error", err)
		return
	}

	if len(candles) > 0 {
		rm.mu.Lock()
		rm.candles = candles
		rm.mu.Unlock()

		// Calculate initial indicators
		rm.calculateIndicators()
		rm.logger.Info("Preloaded history for RegimeMonitor", "count", len(candles))
	}
}

func (rm *RegimeMonitor) Stop() error {
	rm.logger.Info("Stopping Regime Monitor")
	if rm.exchange != nil {
		return rm.exchange.StopKlineStream()
	}
	return nil
}

func (rm *RegimeMonitor) GetRegime() pb.MarketRegime {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.currentRegime
}

func (rm *RegimeMonitor) handleKlineUpdate(candle *pb.Candle) {
	if candle == nil {
		return
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Update candle history
	if candle.IsClosed {
		// Append new closed candle
		rm.candles = append(rm.candles, candle)
		// Maintain history size
		if len(rm.candles) > historySize {
			rm.candles = rm.candles[len(rm.candles)-historySize:]
		}
	}

	if !candle.IsClosed {
		return
	}

	// Calculate indicators based on updated history
	rm.calculateIndicatorsLocked()

	// Regime Detection Logic
	// RSI > 70 -> Bull Trend
	// RSI < 30 -> Bear Trend
	// Else -> Range

	newRegime := pb.MarketRegime_MARKET_REGIME_RANGE

	// Use the calculated RSI
	rsiVal, _ := rm.rsi.Float64()

	if rsiVal > 70 {
		newRegime = pb.MarketRegime_MARKET_REGIME_BULL_TREND
	} else if rsiVal < 30 {
		newRegime = pb.MarketRegime_MARKET_REGIME_BEAR_TREND
	}

	// Log transition
	if newRegime != rm.currentRegime {
		rm.logger.Warn("Regime Changed",
			"old", rm.currentRegime.String(),
			"new", newRegime.String(),
			"rsi", rsiVal,
			"atr", rm.atr.String())

		// Update metrics
		metrics := telemetry.GetGlobalMetrics()
		if metrics != nil && metrics.RegimeChanges != nil {
			metrics.RegimeChanges.Add(context.Background(), 1,
				metric.WithAttributes(
					attribute.String("symbol", rm.symbol),
					attribute.String("old_regime", rm.currentRegime.String()),
					attribute.String("new_regime", newRegime.String()),
					attribute.String("trigger", "kline_update"),
				))
		}

		rm.currentRegime = newRegime
	}
}

func (rm *RegimeMonitor) calculateIndicators() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.calculateIndicatorsLocked()
}

func (rm *RegimeMonitor) calculateIndicatorsLocked() {
	if len(rm.candles) < rsiPeriod+1 {
		return
	}

	rm.calculateRSI()
	rm.calculateATR()
}

func (rm *RegimeMonitor) calculateRSI() {
	// RSI = 100 - 100 / (1 + RS)
	// RS = AvgGain / AvgLoss
	// First calculation: Simple Average
	// Subsequent: (PrevAvg * (n-1) + Current) / n

	// Need at least rsiPeriod + 1 candles to calculate change
	if len(rm.candles) <= rsiPeriod {
		return
	}

	var gains, losses decimal.Decimal

	// Calculate initial average for the first period
	// We go back rsiPeriod from the end

	// Calculate changes
	// Note: A proper EMA smoothing for RSI usually requires more history.
	// For simplicity and robustness with limited history, we can use SMA or Wilder's Smoothing.
	// Wilder's: AvgGain = (PrevAvgGain * (14-1) + CurrentGain) / 14

	// Let's recalculate from as much history as we have to be accurate
	// Or just calculate for the last window if we want SMA RSI (simpler)
	// Standard RSI uses Wilder's Smoothing.

	// To do Wilder's properly, we should ideally track the state.
	// But since we have the full history (up to historySize), we can iterate through it.

	// Calculate initial AvgGain/AvgLoss for the first 'rsiPeriod' changes
	// Identify the starting point where we can compute the first RSI
	// We need enough data.

	// Let's iterate from the beginning of our stored history

	if len(rm.candles) < rsiPeriod+1 {
		return
	}

	// 1. Calculate SMA for the first RSI period
	for i := 1; i <= rsiPeriod; i++ {
		current := rm.candles[i]
		prev := rm.candles[i-1]

		change := pbu.ToGoDecimal(current.Close).Sub(pbu.ToGoDecimal(prev.Close))
		if change.IsPositive() {
			gains = gains.Add(change)
		} else {
			losses = losses.Add(change.Abs())
		}
	}

	avgGain := gains.Div(decimal.NewFromInt(int64(rsiPeriod)))
	avgLoss := losses.Div(decimal.NewFromInt(int64(rsiPeriod)))

	// 2. Apply Wilder's smoothing for the rest
	for i := rsiPeriod + 1; i < len(rm.candles); i++ {
		current := rm.candles[i]
		prev := rm.candles[i-1]

		change := pbu.ToGoDecimal(current.Close).Sub(pbu.ToGoDecimal(prev.Close))
		var gain, loss decimal.Decimal
		if change.IsPositive() {
			gain = change
		} else {
			loss = change.Abs()
		}

		// AvgGain = (PreviousAvgGain * 13 + CurrentGain) / 14
		avgGain = (avgGain.Mul(decimal.NewFromInt(rsiPeriod - 1)).Add(gain)).Div(decimal.NewFromInt(rsiPeriod))
		avgLoss = (avgLoss.Mul(decimal.NewFromInt(rsiPeriod - 1)).Add(loss)).Div(decimal.NewFromInt(rsiPeriod))
	}

	if avgLoss.IsZero() {
		rm.rsi = decimal.NewFromInt(100)
	} else {
		rs := avgGain.Div(avgLoss)
		// RSI = 100 - (100 / (1 + RS))
		rm.rsi = decimal.NewFromInt(100).Sub(decimal.NewFromInt(100).Div(decimal.NewFromInt(1).Add(rs)))
	}
}

func (rm *RegimeMonitor) calculateATR() {
	// ATR = SMA(TR, Window) or Wilder's Smoothing
	// Let's use simple SMA for ATR as implemented in RiskMonitor or Wilder's if we want to be fancy.
	// RiskMonitor uses SMA: stats.ATR = trSum.Div(decimal.NewFromInt(int64(count)))
	// But wait, RiskMonitor's implementation seems to be a simple average over the window (SMA).
	// Let's stick to SMA for consistency with RiskMonitor,
	// OR use Wilder's since we are doing RSI properly.
	// Actually, RiskMonitor implementation:
	// It iterates the last N candles and averages the TR. That is SMA.

	if len(rm.candles) < atrPeriod+1 {
		return
	}

	var trSum decimal.Decimal
	count := 0

	// Calculate TR for the last 'atrPeriod' candles
	start := len(rm.candles) - atrPeriod
	if start < 1 {
		start = 1
	}

	for i := start; i < len(rm.candles); i++ {
		current := rm.candles[i]
		prev := rm.candles[i-1]

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
		rm.atr = trSum.Div(decimal.NewFromInt(int64(count)))
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

		// Update metrics
		metrics := telemetry.GetGlobalMetrics()
		if metrics != nil && metrics.RegimeChanges != nil {
			metrics.RegimeChanges.Add(context.Background(), 1,
				metric.WithAttributes(
					attribute.String("symbol", rm.symbol),
					attribute.String("old_regime", rm.currentRegime.String()),
					attribute.String("new_regime", newRegime.String()),
					attribute.String("trigger", "manual_update"),
				))
		}

		rm.currentRegime = newRegime
	}
}
