package binance

import (
	"context"
	"encoding/json"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/pkg/pbu"
	"net/http"
	"net/http/httptest"
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

func TestBinanceExchange_GetFundingRate_Mapping(t *testing.T) {
	// Setup mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/premiumIndex" {
			resp := map[string]interface{}{
				"symbol":          "BTCUSDT",
				"lastFundingRate": "0.00010000",
				"nextFundingTime": int64(1738108800000), // ms
				"time":            int64(1738080000000), // ms
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer ts.Close()

	cfg := &config.ExchangeConfig{
		BaseURL: ts.URL,
	}
	ex := NewBinanceExchange(cfg, &mockLogger{}, nil)

	rate, err := ex.GetFundingRate(context.Background(), "BTCUSDT")
	require.NoError(t, err)

	assert.Equal(t, "binance", rate.Exchange)
	assert.Equal(t, "BTCUSDT", rate.Symbol)
	assert.Equal(t, "0.0001", pbu.ToGoDecimal(rate.Rate).String())
	assert.Equal(t, int64(1738108800000), rate.NextFundingTime)
	assert.Equal(t, int64(1738080000000), rate.Timestamp)
}

func TestBinanceExchange_GetFundingRates_Mapping(t *testing.T) {
	// Setup mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/premiumIndex" {
			resp := []map[string]interface{}{
				{
					"symbol":          "BTCUSDT",
					"lastFundingRate": "0.00010000",
					"nextFundingTime": int64(1738108800000),
					"time":            int64(1738080000000),
				},
				{
					"symbol":          "ETHUSDT",
					"lastFundingRate": "0.00020000",
					"nextFundingTime": int64(1738108800000),
					"time":            int64(1738080000000),
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer ts.Close()

	cfg := &config.ExchangeConfig{
		BaseURL: ts.URL,
	}
	ex := NewBinanceExchange(cfg, &mockLogger{}, nil)

	rates, err := ex.GetFundingRates(context.Background())
	require.NoError(t, err)
	require.Len(t, rates, 2)

	assert.Equal(t, "BTCUSDT", rates[0].Symbol)
	assert.Equal(t, "0.0001", pbu.ToGoDecimal(rates[0].Rate).String())
	assert.Equal(t, "ETHUSDT", rates[1].Symbol)
	assert.Equal(t, "0.0002", pbu.ToGoDecimal(rates[1].Rate).String())
}
