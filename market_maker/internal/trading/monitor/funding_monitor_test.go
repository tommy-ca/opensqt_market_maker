package monitor

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// MockLogger for testing
type MockLogger struct{}

func (m *MockLogger) Debug(msg string, fields ...interface{})               {}
func (m *MockLogger) Info(msg string, fields ...interface{})                {}
func (m *MockLogger) Warn(msg string, fields ...interface{})                {}
func (m *MockLogger) Error(msg string, fields ...interface{})               {}
func (m *MockLogger) Fatal(msg string, fields ...interface{})               {}
func (m *MockLogger) WithField(key string, value interface{}) core.ILogger  { return m }
func (m *MockLogger) WithFields(fields map[string]interface{}) core.ILogger { return m }

func TestFundingMonitor(t *testing.T) {
	// Setup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mockEx1 := mock.NewMockExchange("binance")
	mockEx2 := mock.NewMockExchange("bybit")

	exchanges := map[string]core.IExchange{
		"binance": mockEx1,
		"bybit":   mockEx2,
	}

	logger := &MockLogger{}
	monitor := NewFundingMonitor(exchanges, logger, "BTCUSDT")

	// Test: Start
	err := monitor.Start(ctx)
	assert.NoError(t, err)

	// Test: Initial GetRate (should be default from mock or zero if not yet received)
	// Mock returns 0.0001
	rate, err := monitor.GetRate("binance", "BTCUSDT")
	assert.NoError(t, err)
	assert.Equal(t, "0.0001", rate.String())

	rate2, err := monitor.GetRate("bybit", "BTCUSDT")
	assert.NoError(t, err)
	assert.Equal(t, "0.0001", rate2.String())

	// Test: Subscription
	subChan := monitor.Subscribe("", "")
	assert.NotNil(t, subChan) // Verify channel creation

	// Test: Filtered subscription only receives matching exchange/symbol
	filtered := monitor.Subscribe("binance", "BTCUSDT")
	other := monitor.Subscribe("bybit", "ETHUSDT")

	// Test: GetNextFundingTime
	nextTime, err := monitor.GetNextFundingTime("binance", "BTCUSDT")
	assert.NoError(t, err)
	assert.True(t, nextTime.After(time.Now()))

	// Test: staleness check (after forcing last update to an older time)
	time.Sleep(10 * time.Millisecond)
	assert.False(t, monitor.IsStale("binance", "BTCUSDT", time.Minute))

	// Force staleness
	monitorTestBackdate(monitor, "binance", "BTCUSDT", time.Now().Add(-2*time.Minute))
	assert.True(t, monitor.IsStale("binance", "BTCUSDT", time.Minute))

	// Push a manual update to exercise filtering
	update := &pb.FundingUpdate{Exchange: "binance", Symbol: "BTCUSDT"}
	monitor.handleUpdate(update)

	select {
	case <-other:
		t.Fatal("filtered subscriber for bybit/ETH should not receive binance/BTC update")
	default:
	}

	select {
	case <-filtered:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("filtered subscriber did not receive matching update")
	}

	_ = monitor.Stop()
}

// helper to backdate last update for staleness testing
func monitorTestBackdate(m *FundingMonitor, exchange, symbol string, ts time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.lastUpdate[exchange]; !ok {
		m.lastUpdate[exchange] = make(map[string]time.Time)
	}
	m.lastUpdate[exchange][symbol] = ts
}
