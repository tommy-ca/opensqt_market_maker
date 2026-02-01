package pb

import (
	"testing"

	decimal "google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestPlaceOrderRequestUseMarginDefaultsToFalse(t *testing.T) {
	msg := &PlaceOrderRequest{}

	if msg.GetUseMargin() {
		t.Fatalf("expected default use_margin to be false")
	}

	marshaled, err := protojson.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var roundTrip PlaceOrderRequest
	if err := protojson.Unmarshal(marshaled, &roundTrip); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if roundTrip.GetUseMargin() {
		t.Fatalf("expected use_margin to remain false after round-trip")
	}
}

func TestPlaceOrderRequestUseMarginRoundTripTrue(t *testing.T) {
	msg := &PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          OrderSide_ORDER_SIDE_BUY,
		Type:          OrderType_ORDER_TYPE_LIMIT,
		TimeInForce:   TimeInForce_TIME_IN_FORCE_GTC,
		Quantity:      &decimal.Decimal{Value: "1.23"},
		Price:         &decimal.Decimal{Value: "100.01"},
		ClientOrderId: "abc-123",
		UseMargin:     true,
	}

	marshaled, err := protojson.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var roundTrip PlaceOrderRequest
	if err := protojson.Unmarshal(marshaled, &roundTrip); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !roundTrip.GetUseMargin() {
		t.Fatalf("expected use_margin to remain true after round-trip")
	}
}
