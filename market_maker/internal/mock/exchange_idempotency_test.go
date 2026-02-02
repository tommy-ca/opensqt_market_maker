package mock

import (
	"context"
	"testing"

	"market_maker/internal/pb"
)

// Verifies that duplicate client_order_id does not create multiple orders
func TestMockExchange_IdempotentClientOrderID(t *testing.T) {
	ex := NewMockExchange("test")
	req := newPlaceOrderRequest("BTCUSDT", "client-123")

	order1, err := ex.PlaceOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("first place failed: %v", err)
	}

	order2, err := ex.PlaceOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("second place failed: %v", err)
	}

	if order1.OrderId != order2.OrderId {
		t.Fatalf("expected same order id, got %d vs %d", order1.OrderId, order2.OrderId)
	}
}

func newPlaceOrderRequest(symbol, clientID string) *pb.PlaceOrderRequest {
	return &pb.PlaceOrderRequest{
		Symbol:        symbol,
		ClientOrderId: clientID,
	}
}
