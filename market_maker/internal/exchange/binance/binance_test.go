package binance

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

func TestStartPriceStream(t *testing.T) {
	// Setup mock WS server
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Send a mock bookTicker update
		// {"e":"bookTicker","u":123,"s":"BTCUSDT","b":"45000.00","B":"10","a":"45001.00","A":"10","T":123456789,"E":123456789}
		msg := `{"e":"bookTicker","s":"BTCUSDT","b":"45000.00","B":"10","a":"45001.00","A":"10","T":123456789,"E":123456789}`
		_ = c.WriteMessage(websocket.TextMessage, []byte(msg))

		// Keep connection open
		time.Sleep(1 * time.Second)
	}))
	defer server.Close()

	// Convert http URL to ws URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	cfg := &config.ExchangeConfig{
		APIKey: "test", SecretKey: "test",
		BaseURL: wsURL, // Hack: Pass WS URL as BaseURL to trick adapter to use it for testing?
		// Actually the adapter should probably take a separate WS URL or derive it.
		// Let's assume we modify adapter to use BaseURL for WS if it starts with ws/wss,
		// or replace http/https with ws/wss.
	}
	// We need to pass the HTTP URL for BaseURL so other things work, but for this test
	// we want it to use the mock server for WS.
	// If the adapter derives ws from http, we pass server.URL.
	cfg.BaseURL = server.URL

	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

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

func TestStartOrderStream(t *testing.T) {
	// Setup mock server handling both HTTP and WS
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle ListenKey generation
		if r.Method == "POST" && r.URL.Path == "/fapi/v1/listenKey" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"listenKey": "test_listen_key"}`))
			return
		}

		// Handle WS connection
		if r.URL.Path == "/test_listen_key" {
			c, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer c.Close()

			// Send mock order update
			// Standard Binance ORDER_TRADE_UPDATE payload (simplified)
			msg := `{
				"e": "ORDER_TRADE_UPDATE",
				"E": 1568879465651,
				"T": 1568879465650,
				"o": {
					"s": "BTCUSDT",
					"c": "client_oid_1",
					"S": "BUY",
					"o": "LIMIT",
					"f": "GTC",
					"q": "1.000",
					"p": "9000.00",
					"ap": "0",
					"sp": "0",
					"x": "NEW",
					"X": "NEW",
					"i": 4293153,
					"l": "0",
					"z": "0",
					"L": "0",
					"n": "0",
					"N": "USDT",
					"T": 1568879465650,
					"t": 0,
					"b": "0",
					"a": "0",
					"m": false,
					"R": false,
					"wt": "CONTRACT_PRICE",
					"ot": "LIMIT",
					"ps": "BOTH",
					"cp": false,
					"rp": "0",
					"pP": false,
					"si": 0,
					"ss": 0
				}
			}`
			_ = c.WriteMessage(websocket.TextMessage, []byte(msg))
			time.Sleep(1 * time.Second)
			return
		}
	}))
	defer server.Close()

	// wsURL is used implicitly by BaseURL conversion logic in adapter
	// wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.ExchangeConfig{APIKey: "test", SecretKey: "test", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

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

func TestPlaceOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/order" {
			t.Errorf("Expected path /fapi/v1/order, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected method POST, got %s", r.Method)
		}

		// Check headers
		if r.Header.Get("X-MBX-APIKEY") != "test_key" {
			t.Errorf("Expected API key header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"orderId": 12345, "symbol": "BTCUSDT", "status": "NEW", "price": "45000.50", "origQty": "1.5", "updateTime": 123456789}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{APIKey: "test_key", SecretKey: "test_secret", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

	req := &pb.PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Type:          pb.OrderType_ORDER_TYPE_LIMIT,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromFloat(1.5)),
		Price:         pbu.FromGoDecimal(decimal.NewFromFloat(45000.50)),
		ClientOrderId: "test_oid",
	}

	order, err := exchange.PlaceOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	if order.OrderId != 12345 {
		t.Errorf("Expected 12345, got %d", order.OrderId)
	}
	if order.Status != pb.OrderStatus_ORDER_STATUS_NEW {
		t.Errorf("Expected NEW, got %s", order.Status)
	}
}

func TestCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/order" {
			t.Errorf("Expected path /fapi/v1/order, got %s", r.URL.Path)
		}
		if r.Method != "DELETE" {
			t.Errorf("Expected method DELETE, got %s", r.Method)
		}

		q := r.URL.Query()
		if q.Get("symbol") != "BTCUSDT" {
			t.Errorf("Expected symbol BTCUSDT")
		}
		if q.Get("orderId") != "12345" {
			t.Errorf("Expected orderId 12345")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"orderId": 12345, "symbol": "BTCUSDT", "status": "CANCELED"}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{APIKey: "test_key", SecretKey: "test_secret", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

	err := exchange.CancelOrder(context.Background(), "BTCUSDT", 12345, false)
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestGetAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v2/account" {
			t.Errorf("Expected path /fapi/v2/account, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"totalWalletBalance": "10000.50",
			"totalMarginBalance": "10000.50",
			"availableBalance": "5000.00",
			"positions": [
				{
					"symbol": "BTCUSDT",
					"positionAmt": "0.5",
					"entryPrice": "40000.0",
					"leverage": "10"
				}
			]
		}`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{APIKey: "test_key", SecretKey: "test_secret", BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

	acc, err := exchange.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}

	if !pbu.ToGoDecimal(acc.TotalWalletBalance).Equal(decimal.NewFromFloat(10000.50)) {
		t.Errorf("Expected wallet balance 10000.50, got %v", acc.TotalWalletBalance)
	}
	if len(acc.Positions) != 1 {
		t.Errorf("Expected 1 position, got %d", len(acc.Positions))
	}
	if acc.Positions[0].Symbol != "BTCUSDT" {
		t.Errorf("Expected symbol BTCUSDT, got %s", acc.Positions[0].Symbol)
	}
}

func TestSignRequest(t *testing.T) {
	cfg := &config.ExchangeConfig{APIKey: "test_key", SecretKey: "test_secret"}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

	req, _ := http.NewRequest("GET", "https://fapi.binance.com/fapi/v1/account", nil)
	err := exchange.SignRequest(req, nil)
	if err != nil {
		t.Fatalf("SignRequest failed: %v", err)
	}

	if req.Header.Get("X-MBX-APIKEY") != "test_key" {
		t.Error("Missing API key header")
	}

	q := req.URL.Query()
	if q.Get("timestamp") == "" || q.Get("signature") == "" {
		t.Error("Missing timestamp or signature")
	}
}

func TestGetSymbolInfo(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/exchangeInfo" {
			t.Errorf("Expected path /fapi/v1/exchangeInfo, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Minimal valid response
		_, _ = w.Write([]byte(`{
			"symbols": [
				{
					"symbol": "BTCUSDT",
					"baseAsset": "BTC",
					"quoteAsset": "USDT",
					"pricePrecision": 2,
					"quantityPrecision": 3,
					"filters": [
						{"filterType": "PRICE_FILTER", "tickSize": "0.01"},
						{"filterType": "LOT_SIZE", "minQty": "0.001", "stepSize": "0.001"},
						{"filterType": "MIN_NOTIONAL", "notional": "5.0"}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	logger, _ := logging.NewZapLogger("DEBUG")
	cfg := &config.ExchangeConfig{
		BaseURL: server.URL,
	}

	ex := NewBinanceExchange(cfg, logger, nil)
	ctx := context.Background()
	symbol := "BTCUSDT"

	info, err := ex.GetSymbolInfo(ctx, symbol)
	if err != nil {
		t.Fatalf("GetSymbolInfo failed: %v", err)
	}

	// Verify Parity
	if info.Symbol != symbol {
		t.Errorf("Expected symbol %s, got %s", symbol, info.Symbol)
	}
	if info.PricePrecision != 2 {
		t.Errorf("Expected PricePrecision 2, got %d", info.PricePrecision)
	}
	if info.QuantityPrecision != 3 {
		t.Errorf("Expected QuantityPrecision 3, got %d", info.QuantityPrecision)
	}
	if info.BaseAsset != "BTC" {
		t.Errorf("Expected BaseAsset BTC, got %s", info.BaseAsset)
	}
	if info.QuoteAsset != "USDT" {
		t.Errorf("Expected QuoteAsset USDT, got %s", info.QuoteAsset)
	}
	// Verify filters
	if !pbu.ToGoDecimal(info.TickSize).Equal(decimal.NewFromFloat(0.01)) {
		t.Errorf("Expected TickSize 0.01, got %v", info.TickSize)
	}
}

func TestBatchPlaceOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/batchOrders" {
			t.Errorf("Expected path /fapi/v1/batchOrders, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected method POST, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"orderId": 1001, "symbol": "BTCUSDT", "status": "NEW", "price": "45000", "origQty": "1", "executedQty": "0"},
			{"orderId": 1002, "symbol": "BTCUSDT", "status": "NEW", "price": "45100", "origQty": "1", "executedQty": "0"}
		]`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

	reqs := []*pb.PlaceOrderRequest{
		{Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY, Type: pb.OrderType_ORDER_TYPE_LIMIT, Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1)), Price: pbu.FromGoDecimal(decimal.NewFromInt(45000))},
		{Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY, Type: pb.OrderType_ORDER_TYPE_LIMIT, Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1)), Price: pbu.FromGoDecimal(decimal.NewFromInt(45100))},
	}

	orders, success := exchange.BatchPlaceOrders(context.Background(), reqs)
	if !success {
		t.Fatal("BatchPlaceOrders reported failure")
	}
	if len(orders) != 2 {
		t.Fatalf("Expected 2 orders, got %d", len(orders))
	}
	if orders[0].OrderId != 1001 || orders[1].OrderId != 1002 {
		t.Errorf("Incorrect order IDs: %d, %d", orders[0].OrderId, orders[1].OrderId)
	}
}

func TestBatchCancelOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/batchOrders" {
			t.Errorf("Expected path /fapi/v1/batchOrders, got %s", r.URL.Path)
		}
		if r.Method != "DELETE" {
			t.Errorf("Expected method DELETE, got %s", r.Method)
		}

		q := r.URL.Query()
		if q.Get("symbol") != "BTCUSDT" {
			t.Errorf("Expected symbol BTCUSDT")
		}
		if q.Get("orderIdList") != "[1001,1002]" {
			t.Errorf("Expected orderIdList [1001,1002], got %s", q.Get("orderIdList"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"orderId": 1001, "status": "CANCELED"}, {"orderId": 1002, "status": "CANCELED"}]`))
	}))
	defer server.Close()

	cfg := &config.ExchangeConfig{BaseURL: server.URL}
	logger, _ := logging.NewZapLogger("INFO")
	exchange := NewBinanceExchange(cfg, logger, nil)

	err := exchange.BatchCancelOrders(context.Background(), "BTCUSDT", []int64{1001, 1002}, false)
	if err != nil {
		t.Fatalf("BatchCancelOrders failed: %v", err)
	}
}
