package risk

import (
	"context"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestRiskMonitor_AnomalyDetection(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}

	monitor := NewRiskMonitor(
		exchange, logger,
		[]string{"BTCUSDT"},
		"1m",
		2.0,   // Multiplier 2.0
		10,    // Average window
		1,     // Recovery threshold
		"Any", // Strategy
		nil,   // No worker pool for testing
	)

	ctx := context.Background()
	monitor.Start(ctx)
	defer monitor.Stop()

	// Simulate normal traffic
	for i := 0; i < 10; i++ {
		candle := &pb.Candle{
			Symbol:    "BTCUSDT",
			Volume:    pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
			Close:     pbu.FromGoDecimal(decimal.NewFromFloat(100.0)), // Price stable
			IsClosed:  true,
			Timestamp: time.Now().UnixMilli(),
		}
		monitor.handleKlineUpdate(candle)
	}

	if monitor.IsTriggered() {
		t.Error("Monitor triggered on normal traffic")
	}

	// Simulate anomaly (Volume > 200 AND Price Drop)
	// Price needs to be < AveragePrice (100.0)
	anomaly := &pb.Candle{
		Symbol:    "BTCUSDT",
		Volume:    pbu.FromGoDecimal(decimal.NewFromFloat(300.0)),
		Close:     pbu.FromGoDecimal(decimal.NewFromFloat(90.0)), // Price Drop
		IsClosed:  true,
		Timestamp: time.Now().UnixMilli(),
	}
	monitor.handleKlineUpdate(anomaly)

	// Wait a bit for async update
	time.Sleep(50 * time.Millisecond)

	if !monitor.IsTriggered() {
		t.Error("Monitor failed to trigger on anomaly")
	}

	// Simulate recovery
	normal := &pb.Candle{
		Symbol:    "BTCUSDT",
		Volume:    pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
		IsClosed:  true,
		Timestamp: time.Now().UnixMilli(),
	}
	monitor.handleKlineUpdate(normal)

	time.Sleep(50 * time.Millisecond)

	if monitor.IsTriggered() {
		t.Error("Monitor failed to recover")
	}
}

func TestRiskMonitor_UnclosedCandleDetection(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}

	monitor := NewRiskMonitor(
		exchange, logger,
		[]string{"BTCUSDT"},
		"1m",
		2.0,
		5,
		1,
		"Any",
		nil,
	)

	ctx := context.Background()
	monitor.Start(ctx)
	defer monitor.Stop()

	// Fill window
	for i := 0; i < 6; i++ {
		candle := &pb.Candle{
			Symbol:   "BTCUSDT",
			Volume:   pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
			Close:    pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
			IsClosed: true,
		}
		monitor.handleKlineUpdate(candle)
	}

	// Unclosed anomaly
	anomaly := &pb.Candle{
		Symbol:   "BTCUSDT",
		Volume:   pbu.FromGoDecimal(decimal.NewFromFloat(300.0)),
		Close:    pbu.FromGoDecimal(decimal.NewFromFloat(90.0)),
		IsClosed: false,
	}
	monitor.handleKlineUpdate(anomaly)

	time.Sleep(50 * time.Millisecond)
	if !monitor.IsTriggered() {
		t.Error("Monitor failed to trigger on unclosed anomaly")
	}
}
