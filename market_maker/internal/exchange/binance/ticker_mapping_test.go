package binance

import (
	"context"
	"encoding/json"
	"market_maker/internal/config"
	"market_maker/pkg/pbu"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinanceExchange_GetTickers_Mapping(t *testing.T) {
	// Setup mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/ticker/24hr" {
			resp := []map[string]interface{}{
				{
					"symbol":             "BTCUSDT",
					"priceChange":        "100.0",
					"priceChangePercent": "0.22",
					"lastPrice":          "45000.0",
					"volume":             "1000.0",
					"quoteVolume":        "45000000.0",
					"closeTime":          int64(1738080000000),
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer ts.Close()

	cfg := &config.ExchangeConfig{
		BaseURL: ts.URL,
	}
	ex, err := NewBinanceExchange(cfg, &mockLogger{}, nil)
	require.NoError(t, err)

	tickers, err := ex.GetTickers(context.Background())
	require.NoError(t, err)
	require.Len(t, tickers, 1)

	t0 := tickers[0]
	assert.Equal(t, "BTCUSDT", t0.Symbol)
	assert.Equal(t, "100", pbu.ToGoDecimal(t0.PriceChange).String())
	assert.Equal(t, "0.0022", pbu.ToGoDecimal(t0.PriceChangePercent).String()) // 0.22 / 100
	assert.Equal(t, "45000", pbu.ToGoDecimal(t0.LastPrice).String())
	assert.Equal(t, "1000", pbu.ToGoDecimal(t0.Volume).String())
	assert.Equal(t, "45000000", pbu.ToGoDecimal(t0.QuoteVolume).String())
}
