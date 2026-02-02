package binancespot

import (
	"context"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/pbu"
	"testing"

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

func TestBinanceSpotExchange_Initialization(t *testing.T) {
	cfg := &config.ExchangeConfig{
		APIKey:  "test_key",
		BaseURL: "https://api.binance.com",
	}
	logger := &MockLogger{}
	pool := concurrency.NewWorkerPool(concurrency.PoolConfig{}, logger)

	exchange := NewBinanceSpotExchange(cfg, logger, pool)
	assert.NotNil(t, exchange)
	assert.Equal(t, "binance_spot", exchange.GetName())
}

func TestBinanceSpotExchange_Constants(t *testing.T) {
	cfg := &config.ExchangeConfig{}
	logger := &MockLogger{}
	pool := concurrency.NewWorkerPool(concurrency.PoolConfig{}, logger)
	exchange := NewBinanceSpotExchange(cfg, logger, pool)

	assert.Equal(t, "BTC", exchange.GetBaseAsset())
	assert.Equal(t, "USDT", exchange.GetQuoteAsset())
	assert.Equal(t, 2, exchange.GetPriceDecimals())
	assert.Equal(t, 5, exchange.GetQuantityDecimals())
}

func TestBinanceSpotExchange_GetFundingRate(t *testing.T) {
	cfg := &config.ExchangeConfig{}
	logger := &MockLogger{}
	pool := concurrency.NewWorkerPool(concurrency.PoolConfig{}, logger)
	exchange := NewBinanceSpotExchange(cfg, logger, pool)

	rate, err := exchange.GetFundingRate(context.Background(), "BTCUSDT")
	assert.NoError(t, err)
	assert.NotNil(t, rate)
	assert.Equal(t, "binance_spot", rate.Exchange)
	assert.Equal(t, "BTCUSDT", rate.Symbol)

	// Rate should be zero
	r := pbu.ToGoDecimal(rate.Rate)
	assert.True(t, r.IsZero())
}
