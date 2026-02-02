package arbitrage_test

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/trading/arbitrage"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

type mockLogger struct {
	core.ILogger
}

func (m *mockLogger) Debug(msg string, args ...interface{})            {}
func (m *mockLogger) Info(msg string, args ...interface{})             {}
func (m *mockLogger) Warn(msg string, args ...interface{})             {}
func (m *mockLogger) Error(msg string, args ...interface{})            {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }

func TestLegManager_SyncAndNeutrality(t *testing.T) {
	mockEx := mock.NewMockExchange("test_ex")
	exchanges := map[string]core.IExchange{"test_ex": mockEx}

	logger := &mockLogger{}
	mgr := arbitrage.NewLegManager(exchanges, logger)

	// 1. Initial State
	assert.False(t, mgr.HasOpenPosition("BTCUSDT"))

	// 2. Mock some positions on exchange
	mockEx.SetPosition("BTCUSDT", decimal.NewFromInt(1))

	err := mgr.SyncState(context.Background(), "test_ex", "BTCUSDT")
	assert.NoError(t, err)
	assert.True(t, mgr.HasOpenPosition("BTCUSDT"))
	assert.False(t, mgr.IsDeltaNeutral("BTCUSDT"))

	// 3. Add second leg
	mockEx2 := mock.NewMockExchange("test_ex_2")
	exchanges["test_ex_2"] = mockEx2
	mgr = arbitrage.NewLegManager(exchanges, logger)

	mockEx.SetPosition("BTCUSDT", decimal.NewFromInt(1))
	mockEx2.SetPosition("BTCUSDT", decimal.NewFromInt(-1))

	mgr.SyncState(context.Background(), "test_ex", "BTCUSDT")
	mgr.SyncState(context.Background(), "test_ex_2", "BTCUSDT")

	assert.True(t, mgr.HasOpenPosition("BTCUSDT"))
	assert.True(t, mgr.IsDeltaNeutral("BTCUSDT"))
}
