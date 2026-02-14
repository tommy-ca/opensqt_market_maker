package bitget

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

func TestBitgetStartPriceStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Read subscribe message (simplified check)
		_, _, err = c.ReadMessage()
		if err != nil {
			return
		}

		// Send ticker update
		msg := `{
			"action": "snapshot",
			"arg": {
				"instType": "UMCBL",
				"channel": "ticker",
				"instId": "BTCUSDT"
			},
			"data": [
				{
					"instId": "BTCUSDT",
					"last": "45000.00",
					"open24h": "44000.00",
					"high24h": "46000.00",
					"low24h": "43000.00",
					"bestBid": "45000.00",
				"bestAsk": "45001.00",
				"ts": 1610000000000
			}
		]
	}`
		_ = c.WriteMessage(websocket.TextMessage, []byte(msg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	// wsURL hack
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{BaseURL: wsURL} // Use WS URL directly as BaseURL for simplicity in this test if adapter supports it
	// Adapter uses baseURL from config.

	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewBitgetExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewBitgetExchange failed: %v", err)
	}

	priceChan := make(chan *pb.PriceChange, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = exchange.StartPriceStream(ctx, []string{"BTCUSDT"}, func(change *pb.PriceChange) {
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

func TestBitgetStartOrderStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Read login message
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		// Verify op=login
		if !strings.Contains(string(msg), `"op":"login"`) {
			t.Errorf("Expected login message, got %s", string(msg))
		}

		// Read subscribe message
		_, msg, err = c.ReadMessage()
		if err != nil {
			return
		}
		// Verify channel=orders
		if !strings.Contains(string(msg), `"channel":"orders"`) {
			t.Errorf("Expected orders subscription, got %s", string(msg))
		}

		// Send order update
		// Bitget Order Update format
		updateMsg := `{
			"action": "snapshot",
			"arg": {
				"instType": "UMCBL",
				"channel": "orders",
				"instId": "default"
			},
			"data": [
				{
					"instId": "BTCUSDT",
					"ordId": "12345",
					"clOrdID": "test_oid",
					"sz": "1.0",
					"notional": "45000.0",
					"px": "45000.0",
					"side": "buy",
					"status": "new",
					"cTime": "1610000000000",
					"accFillSz": "0",
					"avgPx": "0"
				}
			]
		}`
		_ = c.WriteMessage(websocket.TextMessage, []byte(updateMsg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{
		APIKey:     "test_key",
		SecretKey:  "test_secret",
		Passphrase: "test_passphrase",
		BaseURL:    wsURL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewBitgetExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewBitgetExchange failed: %v", err)
	}

	orderChan := make(chan *pb.OrderUpdate, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = exchange.StartOrderStream(ctx, func(update *pb.OrderUpdate) {
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

func TestBitgetPlaceOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/mix/order/place-order" {
			t.Errorf("Expected path /api/v2/mix/order/place-order, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": {
				"orderId": "123456",
				"clientOID": "test_oid"
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{
		APIKey:     "test_key",
		SecretKey:  "test_secret",
		Passphrase: "test_passphrase",
		BaseURL:    server.URL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewBitgetExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewBitgetExchange failed: %v", err)
	}

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

func TestBitgetGetAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if strings.Contains(r.URL.Path, "/api/v2/mix/account/account") {
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": {
					"available": "5000.00",
					"accountEquity": "10000.00",
					"posMode": "hedge_mode",
					"marginMode": "crossed",
					"crossedMarginLeverage": "10"
				}
			}`))
		} else if strings.Contains(r.URL.Path, "/api/v2/mix/position/single-position") {
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "success",
				"data": []
			}`))
		} else {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{
		APIKey:     "test_key",
		SecretKey:  "test_secret",
		Passphrase: "test_passphrase",
		BaseURL:    server.URL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewBitgetExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewBitgetExchange failed: %v", err)
	}

	acc, err := exchange.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}

	if !pbu.ToGoDecimal(acc.AvailableBalance).Equal(decimal.NewFromFloat(5000.00)) {
		t.Errorf("Expected AvailableBalance 5000.00, got %v", acc.AvailableBalance)
	}
}

func TestBitgetCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/mix/order/cancel-order" {
			t.Errorf("Expected path /api/v2/mix/order/cancel-order, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": {
				"orderId": "123456",
				"clientOID": "test_oid"
			}
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{
		APIKey:     "test_key",
		SecretKey:  "test_secret",
		Passphrase: "test_passphrase",
		BaseURL:    server.URL,
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewBitgetExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewBitgetExchange failed: %v", err)
	}

	err = exchange.CancelOrder(context.Background(), "BTCUSDT", 123456, false)
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestSignRequest(t *testing.T) {
	cfg := &config.ExchangeConfig{
		APIKey:     "test_key",
		SecretKey:  "test_secret",
		Passphrase: "test_passphrase",
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewBitgetExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewBitgetExchange failed: %v", err)
	}

	req, _ := http.NewRequest("POST", "https://api.bitget.com/api/v2/mix/order/place-order", nil)
	exchange.SignRequest(req, "")

	if req.Header.Get("ACCESS-KEY") != "test_key" {
		t.Error("Missing ACCESS-KEY header")
	}
	if req.Header.Get("ACCESS-PASSPHRASE") != "test_passphrase" {
		t.Error("Missing ACCESS-PASSPHRASE header")
	}
	if req.Header.Get("ACCESS-SIGN") == "" {
		t.Error("Missing ACCESS-SIGN header")
	}
	if req.Header.Get("ACCESS-TIMESTAMP") == "" {
		t.Error("Missing ACCESS-TIMESTAMP header")
	}
}

func TestBitgetGetSymbolInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/mix/market/contracts" {
			t.Errorf("Expected path /api/v2/mix/market/contracts, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "success",
			"data": [
				{
					"symbol": "BTCUSDT",
					"baseCoin": "BTC",
					"quoteCoin": "USDT",
					"pricePlace": "2",
					"volumePlace": "3",
					"priceEndStep": "0.1",
					"minTradeNum": "0.001"
				}
			]
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("DEBUG")
	ex, err := NewBitgetExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewBitgetExchange failed: %v", err)
	}
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
	if info.QuantityPrecision != 3 {
		t.Errorf("Expected QuantityPrecision 3, got %d", info.QuantityPrecision)
	}
	if !pbu.ToGoDecimal(info.TickSize).Equal(decimal.NewFromFloat(0.1)) {
		t.Errorf("Expected TickSize 0.1, got %v", info.TickSize)
	}
}
