package binance

import (
	"context"
	"market_maker/internal/config"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBinanceMapping_AccountAndPositions(t *testing.T) {
	// Sample raw response from /fapi/v2/account
	rawResponse := `{
		"totalWalletBalance": "12345.67",
		"totalMarginBalance": "12000.00",
		"availableBalance": "5000.50",
		"positions": [
			{
				"symbol": "BTCUSDT",
				"positionAmt": "1.234",
				"entryPrice": "45000.12345",
				"markPrice": "45100.50",
				"unRealizedProfit": "123.456",
				"leverage": "20",
				"marginType": "isolated",
				"isolatedWallet": "100.0",
				"liquidationPrice": "42000.0"
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rawResponse))
	}))
	defer server.Close()

	logger, _ := logging.NewZapLogger("INFO")
	cfg := &config.ExchangeConfig{BaseURL: server.URL, APIKey: "test", SecretKey: "test"}
	ex, err := NewBinanceExchange(cfg, logger, nil)
	assert.NoError(t, err)

	acc, err := ex.GetAccount(context.Background())
	assert.NoError(t, err)

	// Verify Account Mappings (Decimal precision)
	assert.Equal(t, "12345.67", pbu.ToGoDecimal(acc.TotalWalletBalance).String())
	assert.Equal(t, "12000", pbu.ToGoDecimal(acc.TotalMarginBalance).String())
	assert.Equal(t, "5000.5", pbu.ToGoDecimal(acc.AvailableBalance).String())

	// Verify Position Mappings
	assert.Len(t, acc.Positions, 1)
	pos := acc.Positions[0]
	assert.Equal(t, "BTCUSDT", pos.Symbol)
	assert.Equal(t, "1.234", pbu.ToGoDecimal(pos.Size).String())
	assert.Equal(t, "45000.12345", pbu.ToGoDecimal(pos.EntryPrice).String())
	assert.Equal(t, "45100.5", pbu.ToGoDecimal(pos.MarkPrice).String())
	assert.Equal(t, "123.456", pbu.ToGoDecimal(pos.UnrealizedPnl).String())
	assert.Equal(t, int32(20), pos.Leverage)
	assert.Equal(t, "isolated", pos.MarginType)
	assert.Equal(t, "100", pbu.ToGoDecimal(pos.IsolatedMargin).String())
	assert.Equal(t, "42000", pbu.ToGoDecimal(pos.LiquidationPrice).String())
}

func TestBinanceMapping_OrderUpdates(t *testing.T) {
	// Sample raw response for a filled order
	rawOrder := `{
		"orderId": 123456,
		"clientOrderId": "test_cid",
		"symbol": "BTCUSDT",
		"side": "SELL",
		"type": "LIMIT",
		"status": "FILLED",
		"price": "50000.00",
		"origQty": "2.000",
		"executedQty": "2.000",
		"avgPrice": "50000.00",
		"updateTime": 1568879465650
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rawOrder))
	}))
	defer server.Close()

	logger, _ := logging.NewZapLogger("INFO")
	cfg := &config.ExchangeConfig{BaseURL: server.URL, APIKey: "test", SecretKey: "test"}
	ex, err := NewBinanceExchange(cfg, logger, nil)
	assert.NoError(t, err)

	order, err := ex.GetOrder(context.Background(), "BTCUSDT", 123456, "", false)
	assert.NoError(t, err)

	assert.Equal(t, int64(123456), order.OrderId)
	assert.Equal(t, "test_cid", order.ClientOrderId)
	assert.Equal(t, pb.OrderSide_ORDER_SIDE_SELL, order.Side)
	assert.Equal(t, pb.OrderType_ORDER_TYPE_LIMIT, order.Type)
	assert.Equal(t, pb.OrderStatus_ORDER_STATUS_FILLED, order.Status)
	assert.Equal(t, "50000", pbu.ToGoDecimal(order.Price).String())
	assert.Equal(t, "2", pbu.ToGoDecimal(order.Quantity).String())
	assert.Equal(t, "2", pbu.ToGoDecimal(order.ExecutedQty).String())
	assert.Equal(t, "50000", pbu.ToGoDecimal(order.AvgPrice).String())
}
