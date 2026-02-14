package binancespot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
)

type nopLogger struct{}

func (n *nopLogger) Debug(string, ...interface{})               {}
func (n *nopLogger) Info(string, ...interface{})                {}
func (n *nopLogger) Warn(string, ...interface{})                {}
func (n *nopLogger) Error(string, ...interface{})               {}
func (n *nopLogger) Fatal(string, ...interface{})               {}
func (n *nopLogger) WithField(string, interface{}) core.ILogger { return n }
func (n *nopLogger) WithFields(map[string]interface{}) core.ILogger {
	return n
}

func TestPlaceOrderUsesMarginEndpointWhenFlagged(t *testing.T) {
	var gotPath string
	var gotClientOrderID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotClientOrderID = r.URL.Query().Get("newClientOrderId")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
            "orderId": 1,
            "symbol": "BTCUSDT",
            "status": "NEW",
            "clientOrderId": "` + gotClientOrderID + `",
            "price": "0",
            "origQty": "1",
            "executedQty": "0",
            "type": "MARKET",
            "side": "SELL",
            "transactTime": 123
        }`))
	}))
	defer server.Close()

	exch, err := NewBinanceSpotExchange(&config.ExchangeConfig{
		BaseURL:   server.URL,
		APIKey:    "key",
		SecretKey: "secret",
	}, &nopLogger{}, nil)
	if err != nil {
		t.Fatalf("NewBinanceSpotExchange failed: %v", err)
	}
	exch.HTTPClient = server.Client()

	_, err = exch.PlaceOrder(context.Background(), &pb.PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_SELL,
		Type:          pb.OrderType_ORDER_TYPE_MARKET,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromInt(1)),
		ClientOrderId: "cid-margin",
		UseMargin:     true,
	})
	if err != nil {
		t.Fatalf("PlaceOrder returned error: %v", err)
	}

	if gotPath != "/sapi/v1/margin/order" {
		t.Fatalf("expected margin endpoint, got %s", gotPath)
	}
	if gotClientOrderID != "cid-margin" {
		t.Fatalf("expected client order id to propagate, got %s", gotClientOrderID)
	}
}

func TestPlaceOrderUsesSpotEndpointWhenNotMargin(t *testing.T) {
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
            "orderId": 2,
            "symbol": "BTCUSDT",
            "status": "NEW",
            "clientOrderId": "cid-spot",
            "price": "0",
            "origQty": "1",
            "executedQty": "0",
            "type": "MARKET",
            "side": "BUY",
            "transactTime": 123
        }`))
	}))
	defer server.Close()

	exch, err := NewBinanceSpotExchange(&config.ExchangeConfig{
		BaseURL:   server.URL,
		APIKey:    "key",
		SecretKey: "secret",
	}, &nopLogger{}, nil)
	if err != nil {
		t.Fatalf("NewBinanceSpotExchange failed: %v", err)
	}
	exch.HTTPClient = server.Client()

	_, err = exch.PlaceOrder(context.Background(), &pb.PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Type:          pb.OrderType_ORDER_TYPE_MARKET,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromInt(1)),
		ClientOrderId: "cid-spot",
		UseMargin:     false,
	})
	if err != nil {
		t.Fatalf("PlaceOrder returned error: %v", err)
	}

	if gotPath != "/api/v3/order" {
		t.Fatalf("expected spot endpoint, got %s", gotPath)
	}
}
