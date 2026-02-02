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

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBinanceWorkflow_IdempotentPlaceOrder(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.Header().Set("Content-Type", "application/json")

		if attempt == 1 {
			// Simulate network timeout or failure by closing connection
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}

		if attempt == 2 {
			// Second attempt returns Duplicate Order ID
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"code": -2012, "msg": "Duplicate order ID."}`))
			return
		}

		// Third attempt (triggered by GetOrder in retry loop)
		if r.Method == "GET" && r.URL.Path == "/fapi/v1/order" {
			assert.Equal(t, "test_oid_123", r.URL.Query().Get("origClientOrderId"))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"orderId": 123456, "clientOrderId": "test_oid_123", "status": "NEW", "price": "50000.00", "origQty": "1.0", "updateTime": 123456789}`))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logger, _ := logging.NewZapLogger("INFO")
	cfg := &config.ExchangeConfig{BaseURL: server.URL, APIKey: "test", SecretKey: "test"}
	ex := NewBinanceExchange(cfg, logger, nil)

	req := &pb.PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Type:          pb.OrderType_ORDER_TYPE_LIMIT,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromInt(1)),
		Price:         pbu.FromGoDecimal(decimal.NewFromInt(50000)),
		ClientOrderId: "test_oid_123",
	}

	order, err := ex.PlaceOrder(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, order)
	assert.Equal(t, int64(123456), order.OrderId)
	assert.Equal(t, "test_oid_123", order.ClientOrderId)
}
