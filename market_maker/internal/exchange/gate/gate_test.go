package gate

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"market_maker/internal/config"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateStartPriceStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Read subscribe message
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		assert.Contains(t, string(msg), `"channel":"futures.tickers"`, "Expected tickers subscription")

		// Send ticker update
		// Gate V4 format
		updateMsg := `{
			"time": 1610000000,
			"channel": "futures.tickers",
			"event": "update",
			"result": [
				{
					"contract": "BTC_USDT",
					"last": "45000",
					"change_percentage": "1.2",
					"funding_rate": "0.0001",
					"mark_price": "45005",
					"index_price": "45004",
					"total_volume": "100000",
					"volume_24h": "100000",
					"volume_24h_btc": "2",
					"volume_24h_usd": "90000",
					"low_24h": "44000",
					"high_24h": "46000"
				}
			]
		}`
		_ = c.WriteMessage(websocket.TextMessage, []byte(updateMsg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	// wsURL hack
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{BaseURL: wsURL}

	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewGateExchange(cfg, logger)

	priceChan := make(chan *pb.PriceChange, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := exchange.StartPriceStream(ctx, []string{"BTC_USDT"}, func(change *pb.PriceChange) {
		priceChan <- change
	})
	require.NoError(t, err, "StartPriceStream failed")

	select {
	case change := <-priceChan:
		assert.Equal(t, "BTC_USDT", change.Symbol)
		assert.True(t, pbu.ToGoDecimal(change.Price).Equal(decimal.NewFromInt(45000)), "Expected 45000, got %v", change.Price)
	case <-time.After(2 * time.Second):
		assert.Fail(t, "Timed out waiting for price update")
	}
}

func TestGateStartOrderStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Read subscribe message
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		// Expect futures.orders subscription with auth
		// Check for "auth" and "sign"
		assert.Contains(t, string(msg), `"channel":"futures.orders"`, "Expected orders subscription")
		assert.Contains(t, string(msg), `"auth"`, "Expected auth payload")

		// Send order update
		updateMsg := `{
			"time": 1610000000,
			"channel": "futures.orders",
			"event": "update",
			"result": [
				{
					"id": 12345,
					"contract": "BTC_USDT",
					"create_time": 1610000000,
					"fill_price": "45000",
					"price": "45000",
					"status": "finished",
					"finish_as": "filled",
					"text": "t-client_oid"
				}
			]
		}`
		_ = c.WriteMessage(websocket.TextMessage, []byte(updateMsg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: "test_secret",
		BaseURL:   wsURL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewGateExchange(cfg, logger)

	orderChan := make(chan *pb.OrderUpdate, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := exchange.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		orderChan <- update
	})
	require.NoError(t, err, "StartOrderStream failed")

	select {
	case update := <-orderChan:
		assert.Equal(t, "BTC_USDT", update.Symbol)
		assert.Equal(t, pb.OrderStatus_ORDER_STATUS_FILLED, update.Status)
	case <-time.After(2 * time.Second):
		assert.Fail(t, "Timed out waiting for order update")
	}
}

func TestGatePlaceOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/futures/usdt/orders", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id": 123456,
			"text": "test_oid",
			"contract": "BTC_USDT",
			"size": 1,
			"price": "50000",
			"status": "open",
			"create_time": 1610000000
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: "test_secret",
		BaseURL:   server.URL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewGateExchange(cfg, logger)

	// Pre-seed multiplier to avoid extra API call in PlaceOrder
	exchange.quantoMultiplier["BTC_USDT"] = decimal.NewFromInt(1)
	exchange.quantoMultiplier["BTCUSDT"] = decimal.NewFromInt(1)

	req := &pb.PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Type:          pb.OrderType_ORDER_TYPE_LIMIT,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromInt(1)),
		Price:         pbu.FromGoDecimal(decimal.NewFromInt(50000)),
		ClientOrderId: "test_oid",
	}

	order, err := exchange.PlaceOrder(context.Background(), req)
	require.NoError(t, err, "PlaceOrder failed")

	assert.Equal(t, int64(123456), order.OrderId)
}

func TestGateGetAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/futures/usdt/accounts", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"total": "10000",
			"available": "5000",
			"point": "0",
			"currency": "USDT"
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: "test_secret",
		BaseURL:   server.URL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewGateExchange(cfg, logger)

	acc, err := exchange.GetAccount(context.Background())
	require.NoError(t, err, "GetAccount failed")

	assert.True(t, pbu.ToGoDecimal(acc.AvailableBalance).Equal(decimal.NewFromInt(5000)), "Expected AvailableBalance 5000, got %v", acc.AvailableBalance)
	assert.True(t, pbu.ToGoDecimal(acc.TotalWalletBalance).Equal(decimal.NewFromInt(10000)), "Expected TotalWalletBalance 10000, got %v", acc.TotalWalletBalance)
}

func TestGateCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/futures/usdt/orders/12345", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": 12345,
			"status": "finished",
			"finish_as": "cancelled"
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: "test_secret",
		BaseURL:   server.URL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewGateExchange(cfg, logger)

	err := exchange.CancelOrder(context.Background(), "BTC_USDT", 12345, false)
	require.NoError(t, err, "CancelOrder failed")
}

func TestGateSignREST(t *testing.T) {
	secretKey := "test_secret"
	cfg := &config.ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: config.Secret(secretKey),
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewGateExchange(cfg, logger)

	timestamp := int64(123456789)
	method := "POST"
	urlPath := "/api/v4/futures/usdt/orders"
	queryString := "param=value"
	body := `{"symbol":"BTC_USDT"}`

	// 3. Call SignREST directly
	signature := exchange.SignREST(method, urlPath, queryString, body, timestamp)

	// 4. Calculate expected signature manually to verify correctness
	// Payload format: method\nurl\nquery\nhex(sha512(body))\ntimestamp
	hasher := sha512.New()
	hasher.Write([]byte(body))
	bodyHash := hex.EncodeToString(hasher.Sum(nil))

	payload := fmt.Sprintf("%s\n%s\n%s\n%s\n%d",
		method, urlPath, queryString, bodyHash, timestamp)

	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write([]byte(payload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// 5. Assert
	assert.Equal(t, expectedSignature, signature, "Signature mismatch")
}

func TestGateGetSymbolInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/futures/usdt/contracts", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{
				"name": "BTC_USDT",
				"order_price_round": "0.1",
				"order_size_min": "1"
			}
		]`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("DEBUG")
	ex := NewGateExchange(cfg, logger)
	ctx := context.Background()

	// Test with Gate style symbol
	info, err := ex.GetSymbolInfo(ctx, "BTC_USDT")
	require.NoError(t, err, "GetSymbolInfo BTC_USDT failed")
	assert.Equal(t, "BTC_USDT", info.Symbol)
	assert.Equal(t, int32(1), info.PricePrecision)
	assert.Equal(t, int32(0), info.QuantityPrecision)

	// Test with normalized symbol
	info2, err := ex.GetSymbolInfo(ctx, "BTCUSDT")
	require.NoError(t, err, "GetSymbolInfo BTCUSDT failed")
	assert.Equal(t, "BTC_USDT", info2.Symbol)
}
