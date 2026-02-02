package bybit

import (
	"context"
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
)

func TestBybitStartPriceStream(t *testing.T) {
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
		if !strings.Contains(string(msg), `"op":"subscribe"`) {
			t.Errorf("Expected subscribe op, got %s", string(msg))
		}
		if !strings.Contains(string(msg), `"tickers.BTCUSDT"`) {
			t.Errorf("Expected tickers subscription, got %s", string(msg))
		}

		// Send ticker update
		// Bybit V5 Ticker format
		updateMsg := `{
			"topic": "tickers.BTCUSDT",
			"type": "snapshot",
			"ts": 1610000000000,
			"data": {
				"symbol": "BTCUSDT",
				"lastPrice": "45000",
				"highPrice24h": "46000",
				"lowPrice24h": "44000",
				"volume24h": "1000",
				"turnover24h": "45000000"
			}
		}`
		c.WriteMessage(websocket.TextMessage, []byte(updateMsg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{BaseURL: wsURL}

	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBybitExchange(cfg, logger)

	priceChan := make(chan *pb.PriceChange, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := exchange.StartPriceStream(ctx, []string{"BTCUSDT"}, func(change *pb.PriceChange) {
		priceChan <- change
	})
	if err != nil {
		t.Fatalf("StartPriceStream failed: %v", err)
	}

	select {
	case change := <-priceChan:
		if change.Symbol != "BTCUSDT" {
			t.Errorf("Expected BTCUSDT, got %s", change.Symbol)
		}
		if !pbu.ToGoDecimal(change.Price).Equal(decimal.NewFromInt(45000)) {
			t.Errorf("Expected 45000, got %v", change.Price)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for price update")
	}
}

func TestBybitStartOrderStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Read auth message
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.Contains(string(msg), `"op":"auth"`) {
			t.Errorf("Expected auth op, got %s", string(msg))
		}

		// Read subscribe message
		_, msg, err = c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.Contains(string(msg), `"op":"subscribe"`) {
			t.Errorf("Expected subscribe op, got %s", string(msg))
		}
		if !strings.Contains(string(msg), `"order"`) {
			t.Errorf("Expected order channel, got %s", string(msg))
		}

		// Send order update
		updateMsg := `{
			"topic": "order",
			"id": "12345",
			"creationTime": 1610000000000,
			"data": [
				{
					"category": "linear",
					"symbol": "BTCUSDT",
					"orderId": "123456",
					"orderLinkID": "test_oid",
					"side": "Buy",
					"orderType": "Limit",
					"price": "45000",
					"qty": "1",
					"orderStatus": "New",
					"cumExecQty": "0",
					"cumExecValue": "0",
					"cumExecFee": "0",
					"createdTime": "1610000000000",
					"updatedTime": "1610000000000"
				}
			]
		}`
		c.WriteMessage(websocket.TextMessage, []byte(updateMsg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{APIKey: "key", SecretKey: "secret", BaseURL: wsURL}

	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBybitExchange(cfg, logger)

	orderChan := make(chan *pb.OrderUpdate, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := exchange.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
		orderChan <- update
	})
	if err != nil {
		t.Fatalf("StartOrderStream failed: %v", err)
	}

	select {
	case update := <-orderChan:
		if update.Symbol != "BTCUSDT" {
			t.Errorf("Expected BTCUSDT, got %s", update.Symbol)
		}
		if update.Status != pb.OrderStatus_ORDER_STATUS_NEW {
			t.Errorf("Expected NEW, got %s", update.Status)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for order update")
	}
}

func TestBybitPlaceOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/order/create" {
			t.Errorf("Expected path /v5/order/create, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"orderId": "123456",
				"orderLinkID": "test_oid"
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: "test_secret",
		BaseURL:   server.URL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBybitExchange(cfg, logger)

	req := &pb.PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Type:          pb.OrderType_ORDER_TYPE_LIMIT,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromInt(1)),
		Price:         pbu.FromGoDecimal(decimal.NewFromInt(50000)),
		ClientOrderId: "test_oid",
	}

	order, err := exchange.PlaceOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	if order.OrderId != 123456 {
		t.Errorf("Expected OrderId 123456, got %d", order.OrderId)
	}
}

func TestBybitCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/order/cancel" {
			t.Errorf("Expected path /v5/order/cancel, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"orderId": "123456"
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{APIKey: "key", SecretKey: "secret", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBybitExchange(cfg, logger)

	err := exchange.CancelOrder(context.Background(), "BTCUSDT", 123456, false)
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestBybitGetAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/account/wallet-balance" {
			t.Errorf("Expected path /v5/account/wallet-balance, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"retCode": 0,
			"result": {
				"list": [
					{
						"totalEquity": "10000.5",
						"totalAvailableBalance": "5000.0",
						"coin": [
							{
								"coin": "USDT",
								"equity": "10000.5",
								"walletBalance": "10000.5",
								"availableToWithdraw": "5000.0"
							}
						]
					}
				]
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{APIKey: "key", SecretKey: "secret", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBybitExchange(cfg, logger)

	acc, err := exchange.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}

	if !pbu.ToGoDecimal(acc.TotalWalletBalance).Equal(decimal.NewFromFloat(10000.5)) {
		t.Errorf("Expected 10000.5, got %v", acc.TotalWalletBalance)
	}
}

func TestBybitSignRequest(t *testing.T) {
	cfg := &config.ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: "test_secret",
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBybitExchange(cfg, logger)

	req, _ := http.NewRequest("GET", "https://api.bybit.com/v5/account/wallet-balance", nil)

	err := exchange.SignRequest(req, "")
	if err != nil {
		t.Fatalf("SignRequest failed: %v", err)
	}

	if req.Header.Get("X-BAPI-API-KEY") != "test_key" {
		t.Error("Missing X-BAPI-API-KEY")
	}
	if req.Header.Get("X-BAPI-SIGN") == "" {
		t.Error("Missing X-BAPI-SIGN")
	}
	if req.Header.Get("X-BAPI-TIMESTAMP") == "" {
		t.Error("Missing X-BAPI-TIMESTAMP")
	}
	if req.Header.Get("X-BAPI-RECV-WINDOW") == "" {
		t.Error("Missing X-BAPI-RECV-WINDOW")
	}
}

func TestBybitGetSymbolInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/market/instruments-info" {
			t.Errorf("Expected path /v5/market/instruments-info, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"retCode": 0,
			"result": {
				"list": [
					{
						"symbol": "BTCUSDT",
						"baseCoin": "BTC",
						"quoteCoin": "USDT",
						"priceScale": "2",
						"priceFilter": {
							"tickSize": "0.10"
						},
						"lotSizeFilter": {
							"qtyStep": "0.001",
							"minOrderQty": "0.001"
						}
					}
				]
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("DEBUG")
	ex := NewBybitExchange(cfg, logger)
	ctx := context.Background()

	info, err := ex.GetSymbolInfo(ctx, "BTCUSDT")
	if err != nil {
		t.Fatalf("GetSymbolInfo failed: %v", err)
	}

	if info.Symbol != "BTCUSDT" {
		t.Errorf("Expected BTCUSDT, got %s", info.Symbol)
	}
	if info.PricePrecision != 2 {
		t.Errorf("Expected PricePrecision 2, got %d", info.PricePrecision)
	}
	// qtyStep 0.001 -> precision 3
	if info.QuantityPrecision != 3 {
		t.Errorf("Expected QuantityPrecision 3, got %d", info.QuantityPrecision)
	}
	if !pbu.ToGoDecimal(info.TickSize).Equal(decimal.NewFromFloat(0.1)) {
		t.Errorf("Expected TickSize 0.1, got %v", info.TickSize)
	}
}
