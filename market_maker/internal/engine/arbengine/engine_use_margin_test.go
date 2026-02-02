package arbengine

import (
	"context"
	"testing"

	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"

	"github.com/shopspring/decimal"
)

type recordingExchange struct {
	*mock.MockExchange
	lastRequests []*pb.PlaceOrderRequest
}

func (r *recordingExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	r.lastRequests = append(r.lastRequests, req)
	order, err := r.MockExchange.PlaceOrder(ctx, req)
	if err == nil && order != nil {
		// Auto-fill for atomic neutrality tests
		order.ExecutedQty = req.Quantity
		order.Status = pb.OrderStatus_ORDER_STATUS_FILLED
	}
	return order, err
}

// Basic logger stub for package-local tests
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

func TestExecuteEntrySetsUseMarginForSpotShort(t *testing.T) {
	spot := &recordingExchange{MockExchange: mock.NewMockExchange("binance_spot")}
	perp := &recordingExchange{MockExchange: mock.NewMockExchange("binance_perp")}

	exchanges := map[string]core.IExchange{
		"binance_spot": spot,
		"binance_perp": perp,
	}

	eng := NewArbitrageEngine(exchanges, nil, nil, &nopLogger{}, EngineConfig{
		Symbol:        "BTCUSDT",
		SpotExchange:  "binance_spot",
		PerpExchange:  "binance_perp",
		OrderQuantity: decimal.NewFromInt(1),
	}).(*ArbitrageEngine)

	if err := eng.executeEntry(context.Background(), false, 123); err != nil {
		t.Fatalf("executeEntry failed: %v", err)
	}

	if len(spot.lastRequests) != 1 {
		t.Fatalf("expected 1 spot request, got %d", len(spot.lastRequests))
	}
	if !spot.lastRequests[0].UseMargin {
		t.Fatalf("expected spot request to set use_margin when shorting")
	}

	if len(perp.lastRequests) != 1 {
		t.Fatalf("expected 1 perp request, got %d", len(perp.lastRequests))
	}
	if perp.lastRequests[0].UseMargin {
		t.Fatalf("perp leg must not set use_margin")
	}
}

func TestExecuteEntryKeepsUseMarginFalseForSpotLong(t *testing.T) {
	spot := &recordingExchange{MockExchange: mock.NewMockExchange("binance_spot")}
	perp := &recordingExchange{MockExchange: mock.NewMockExchange("binance_perp")}

	exchanges := map[string]core.IExchange{
		"binance_spot": spot,
		"binance_perp": perp,
	}

	eng := NewArbitrageEngine(exchanges, nil, nil, &nopLogger{}, EngineConfig{
		Symbol:        "BTCUSDT",
		SpotExchange:  "binance_spot",
		PerpExchange:  "binance_perp",
		OrderQuantity: decimal.NewFromInt(1),
	}).(*ArbitrageEngine)

	if err := eng.executeEntry(context.Background(), true, 456); err != nil {
		t.Fatalf("executeEntry failed: %v", err)
	}

	if len(spot.lastRequests) != 1 {
		t.Fatalf("expected 1 spot request, got %d", len(spot.lastRequests))
	}
	if spot.lastRequests[0].UseMargin {
		t.Fatalf("expected spot request to keep use_margin false when not shorting")
	}
}
