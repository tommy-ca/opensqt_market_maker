package binancespot

import (
	"context"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...interface{})               {}
func (m *mockLogger) Info(msg string, fields ...interface{})                {}
func (m *mockLogger) Warn(msg string, fields ...interface{})                {}
func (m *mockLogger) Error(msg string, fields ...interface{})               {}
func (m *mockLogger) Fatal(msg string, fields ...interface{})               {}
func (m *mockLogger) WithField(key string, value interface{}) core.ILogger  { return m }
func (m *mockLogger) WithFields(fields map[string]interface{}) core.ILogger { return m }

func TestBinanceSpotExchange_GetFundingRate_Mapping(t *testing.T) {
	cfg := &config.ExchangeConfig{}
	ex, err := NewBinanceSpotExchange(cfg, &mockLogger{}, nil)
	require.NoError(t, err)

	rate, err := ex.GetFundingRate(context.Background(), "BTCUSDT")
	require.NoError(t, err)

	assert.Equal(t, "binance_spot", rate.Exchange)
	assert.Equal(t, "BTCUSDT", rate.Symbol)
	assert.Equal(t, "0", pbu.ToGoDecimal(rate.Rate).String())
	assert.Equal(t, int64(0), rate.NextFundingTime)
	assert.True(t, rate.Timestamp > 0)
}
