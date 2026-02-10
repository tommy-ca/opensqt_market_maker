package okx

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

func TestOKXStartPriceStream(t *testing.T) {
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
		if !strings.Contains(string(msg), `"channel":"tickers"`) {
			t.Errorf("Expected tickers subscription, got %s", string(msg))
		}

		// Send ticker update
		// OKX Ticker format
		updateMsg := `{
			"arg": {
				"channel": "tickers",
				"instId": "BTC-USDT-SWAP"
			},
			"data": [
				{
					"instId": "BTC-USDT-SWAP",
					"last": "45000.00",
					"ts": "1610000000000"
				}
			]
		}`
		_ = c.WriteMessage(websocket.TextMessage, []byte(updateMsg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	// wsURL hack
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{BaseURL: wsURL} // Helper to inject WS URL

	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}

	priceChan := make(chan *pb.PriceChange, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = exchange.StartPriceStream(ctx, []string{"BTC-USDT-SWAP"}, func(change *pb.PriceChange) {
		priceChan <- change
	})
	if err != nil {
		t.Fatalf("StartPriceStream failed: %v", err)
	}

	select {
	case change := <-priceChan:
		if change.Symbol != "BTC-USDT-SWAP" {
			t.Errorf("Expected BTC-USDT-SWAP, got %s", change.Symbol)
		}
		if !pbu.ToGoDecimal(change.Price).Equal(decimal.NewFromInt(45000)) {
			t.Errorf("Expected 45000, got %v", change.Price)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for price update")
	}
}

func TestOKXStartOrderStream(t *testing.T) {
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
		if !strings.Contains(string(msg), `"op":"login"`) {
			t.Errorf("Expected login, got %s", string(msg))
		}

		// Read subscribe message
		_, msg, err = c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.Contains(string(msg), `"channel":"orders"`) {
			t.Errorf("Expected orders subscription, got %s", string(msg))
		}

		// Send order update
		updateMsg := `{
			"arg": {
				"channel": "orders",
				"instType": "SWAP",
				"uid": "12345"
			},
			"data": [
				{
					"accFillSz": "0",
					"avgPx": "0",
					"cTime": "1610000000000",
					"clOrdID": "test_oid",
					"fillPx": "0",
					"fillSz": "0",
					"fillTime": "0",
					"instId": "BTC-USDT-SWAP",
					"instType": "SWAP",
					"lever": "10",
					"ordId": "123456",
					"ordType": "limit",
					"pnl": "0",
					"posSide": "net",
					"px": "45000",
					"rebate": "0",
					"rebateCcy": "USDT",
					"side": "buy",
					"state": "live",
					"sz": "1",
					"tag": "",
					"tdMode": "cross",
					"tgtCcy": "",
					"tradeId": "",
					"uTime": "1610000000000"
				}
			]
		}`
		_ = c.WriteMessage(websocket.TextMessage, []byte(updateMsg))
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{APIKey: "k", SecretKey: "s", Passphrase: "p", BaseURL: wsURL}

	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
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
		if update.Symbol != "BTC-USDT-SWAP" {
			t.Errorf("Expected BTC-USDT-SWAP, got %s", update.Symbol)
		}
		if update.Status != pb.OrderStatus_ORDER_STATUS_NEW {
			t.Errorf("Expected NEW, got %s", update.Status)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for order update")
	}
}

func TestOKXPlaceOrder(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v5/trade/order" {
			t.Errorf("Expected path /api/v5/trade/order, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{
					"ordId": "123456",
					"clOrdID": "test_oid",
					"tag": "",
					"sCode": "0",
					"sMsg": ""
				}
			]
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
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}

	req := &pb.PlaceOrderRequest{
		Symbol:        "BTC-USDT-SWAP",
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

func TestOKXCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v5/trade/cancel-order" {
			t.Errorf("Expected path /api/v5/trade/cancel-order, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "0",
			"data": [
				{
					"ordId": "123456",
					"sCode": "0"
				}
			]
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{APIKey: "key", SecretKey: "secret", Passphrase: "pass", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}

	err = exchange.CancelOrder(context.Background(), "BTC-USDT-SWAP", 123456, false)
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestOKXGetAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v5/account/balance" {
			t.Errorf("Expected path /api/v5/account/balance, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "0",
			"data": [
				{
					"totalEq": "10000.5",
					"details": [
						{
							"ccy": "USDT",
							"eq": "10000.5",
							"availEq": "5000.0"
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{APIKey: "key", SecretKey: "secret", Passphrase: "pass", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}

	acc, err := exchange.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}

	if !pbu.ToGoDecimal(acc.TotalWalletBalance).Equal(decimal.NewFromFloat(10000.5)) {
		t.Errorf("Expected 10000.5, got %v", acc.TotalWalletBalance)
	}
}

func TestOKXSignRequest(t *testing.T) {
	cfg := &config.ExchangeConfig{
		APIKey:     "test_key",
		SecretKey:  "test_secret",
		Passphrase: "test_passphrase",
	}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}

	req, _ := http.NewRequest("GET", "https://www.okx.com/api/v5/account/balance", nil)

	// We need to inject a fixed timestamp to verify signature, but SignRequest uses time.Now()
	// I'll test that headers are present and format is correct.
	// For exact signature verification, I would need to mock time or split the logic.

	err = exchange.SignRequest(req, "")
	if err != nil {
		t.Fatalf("SignRequest failed: %v", err)
	}

	if req.Header.Get("OK-ACCESS-KEY") != "test_key" {
		t.Error("Missing OK-ACCESS-KEY")
	}
	if req.Header.Get("OK-ACCESS-PASSPHRASE") != "test_passphrase" {
		t.Error("Missing OK-ACCESS-PASSPHRASE")
	}
	if req.Header.Get("OK-ACCESS-SIGN") == "" {
		t.Error("Missing OK-ACCESS-SIGN")
	}
	if req.Header.Get("OK-ACCESS-TIMESTAMP") == "" {
		t.Error("Missing OK-ACCESS-TIMESTAMP")
	}

	// Check timestamp format (ISO 8601)
	ts := req.Header.Get("OK-ACCESS-TIMESTAMP")
	// e.g. 2020-12-08T09:08:57.715Z
	// Check if it contains "T" and "Z"
	if len(ts) < 20 { // Basic length check
		t.Errorf("Invalid timestamp format: %s", ts)
	}
}

func TestOKXGetSymbolInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v5/public/instruments" {
			t.Errorf("Expected path /api/v5/public/instruments, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "0",
			"data": [
				{
					"instId": "BTC-USDT-SWAP",
					"baseCcy": "BTC",
					"quoteCcy": "USDT",
					"tickSz": "0.1",
					"lotSz": "1",
					"minSz": "1"
				}
			]
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("DEBUG")
	ex, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}
	ctx := context.Background()

	info, err := ex.GetSymbolInfo(ctx, "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("GetSymbolInfo failed: %v", err)
	}

	if info.Symbol != "BTC-USDT-SWAP" {
		t.Errorf("Expected BTC-USDT-SWAP, got %s", info.Symbol)
	}
	// tickSz 0.1 -> precision 1
	if info.PricePrecision != 1 {
		t.Errorf("Expected PricePrecision 1, got %d", info.PricePrecision)
	}
	// lotSz 1 -> precision 0
	if info.QuantityPrecision != 0 {
		t.Errorf("Expected QuantityPrecision 0, got %d", info.QuantityPrecision)
	}
	if !pbu.ToGoDecimal(info.TickSize).Equal(decimal.NewFromFloat(0.1)) {
		t.Errorf("Expected TickSize 0.1, got %v", info.TickSize)
	}
}

func TestOKXBatchPlaceOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v5/trade/batch-orders" {
			t.Errorf("Expected path /api/v5/trade/batch-orders, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "0",
			"msg": "",
			"data": [
				{"ordId": "2001", "clOrdID": "oid1", "sCode": "0"},
				{"ordId": "2002", "clOrdID": "oid2", "sCode": "0"}
			]
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}

	reqs := []*pb.PlaceOrderRequest{
		{Symbol: "BTC-USDT-SWAP", Side: pb.OrderSide_ORDER_SIDE_BUY, Type: pb.OrderType_ORDER_TYPE_LIMIT, Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1)), Price: pbu.FromGoDecimal(decimal.NewFromInt(45000)), ClientOrderId: "oid1"},
		{Symbol: "BTC-USDT-SWAP", Side: pb.OrderSide_ORDER_SIDE_BUY, Type: pb.OrderType_ORDER_TYPE_LIMIT, Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1)), Price: pbu.FromGoDecimal(decimal.NewFromInt(45100)), ClientOrderId: "oid2"},
	}

	orders, success := exchange.BatchPlaceOrders(context.Background(), reqs)
	if !success {
		t.Fatal("BatchPlaceOrders reported failure")
	}
	if len(orders) != 2 {
		t.Fatalf("Expected 2 orders, got %d", len(orders))
	}
	if orders[0].OrderId != 2001 || orders[1].OrderId != 2002 {
		t.Errorf("Incorrect order IDs: %d, %d", orders[0].OrderId, orders[1].OrderId)
	}
}

func TestOKXBatchCancelOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v5/trade/cancel-batch-orders" {
			t.Errorf("Expected path /api/v5/trade/cancel-batch-orders, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "0",
			"data": [
				{"ordId": "2001", "sCode": "0"},
				{"ordId": "2002", "sCode": "0"}
			]
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange, err := NewOKXExchange(cfg, logger)
	if err != nil {
		t.Fatalf("NewOKXExchange failed: %v", err)
	}

	err = exchange.BatchCancelOrders(context.Background(), "BTC-USDT-SWAP", []int64{2001, 2002}, false)
	if err != nil {
		t.Fatalf("BatchCancelOrders failed: %v", err)
	}
}
