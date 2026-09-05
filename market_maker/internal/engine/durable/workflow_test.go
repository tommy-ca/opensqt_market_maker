package durable

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/shopspring/decimal"
)

// Manual Mock for DBOSContext
type MockDBOSContext struct {
	dbos.DBOSContext
	StepResults []any
	StepErrors  []error
	StepIndex   int
}

func (m *MockDBOSContext) RunAsStep(ctx dbos.DBOSContext, fn dbos.StepFunc, opts ...dbos.StepOption) (any, error) {
	if m.StepIndex >= len(m.StepResults) {
		return nil, fmt.Errorf("unexpected step call at index %d", m.StepIndex)
	}

	// Actually execute the function to trigger side effects in mocks
	_, _ = fn(context.Background())

	res := m.StepResults[m.StepIndex]
	err := m.StepErrors[m.StepIndex]
	m.StepIndex++
	return res, err
}

type mockStrategy struct {
	actions []*pb.OrderAction
}

func (m *mockStrategy) CalculateActions(
	currentPrice decimal.Decimal,
	anchorPrice decimal.Decimal,
	atr decimal.Decimal,
	volatilityFactor float64,
	isRiskTriggered bool,
	regime pb.MarketRegime,
	slots []core.StrategySlot,
) []*pb.OrderAction {
	return m.actions
}

type mockPM struct {
	core.IPositionManager
	applyResults []core.OrderActionResult
}

func (m *mockPM) ApplyActionResults(results []core.OrderActionResult) error {
	m.applyResults = results
	return nil
}

func (m *mockPM) GetSnapshot() *pb.PositionManagerSnapshot {
	return &pb.PositionManagerSnapshot{}
}

func (m *mockPM) GetStrategySlots(target []core.StrategySlot) []core.StrategySlot {
	return nil
}

func (m *mockPM) GetAnchorPrice() decimal.Decimal {
	return decimal.NewFromInt(45000)
}

func (m *mockPM) MarkSlotsPending(actions []*pb.OrderAction) {
}

type mockOE struct {
	core.IOrderExecutor
	placedRequests []*pb.PlaceOrderRequest
}

func (m *mockOE) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	m.placedRequests = append(m.placedRequests, req)
	return &pb.Order{OrderId: 12345}, nil
}

func TestTradingWorkflows_OnPriceUpdate(t *testing.T) {
	pm := &mockPM{}
	actions := []*pb.OrderAction{
		{
			Type:    pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
			Request: &pb.PlaceOrderRequest{Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY},
		},
	}
	strategy := &mockStrategy{actions: actions}
	oe := &mockOE{}
	w := NewTradingWorkflows(pm, oe, strategy)

	price := pb.PriceChange{
		Symbol: "BTCUSDT",
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(45000)),
	}

	mockCtx := &MockDBOSContext{
		StepResults: []any{
			actions, // Step 1: Calculate Actions (via Strategy)
			core.OrderActionResult{ // Step 2: Place Order
				Action: actions[0],
				Order:  &pb.Order{OrderId: 12345},
			},
			nil, // Step 3: Apply Results
		},
		StepErrors: []error{nil, nil, nil},
	}

	_, err := w.OnPriceUpdate(mockCtx, &price)
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// We don't verify calculatePrice on pm anymore as it's not passed to pm
	// We could verify it on strategy if we stored it in mockStrategy

	if len(oe.placedRequests) != 1 {
		t.Errorf("Expected 1 order placed, got %d", len(oe.placedRequests))
	}

	if len(pm.applyResults) != 1 {
		t.Errorf("Expected 1 result applied, got %d", len(pm.applyResults))
	} else if pm.applyResults[0].Order.OrderId != 12345 {
		t.Errorf("Expected order ID 12345, got %d", pm.applyResults[0].Order.OrderId)
	}
}

func TestTradingWorkflows_OnPriceUpdate_Resumption(t *testing.T) {
	pm := &mockPM{}
	actions := []*pb.OrderAction{
		{
			Type:    pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
			Request: &pb.PlaceOrderRequest{Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY},
		},
	}
	strategy := &mockStrategy{actions: actions}
	oe := &mockOE{}
	w := NewTradingWorkflows(pm, oe, strategy)

	price := pb.PriceChange{
		Symbol: "BTCUSDT",
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(45000)),
	}

	// First execution fails at Step 3 (Apply Results)
	mockCtx1 := &MockDBOSContext{
		StepResults: []any{
			actions, // Step 1: Success
			core.OrderActionResult{ // Step 2: Success
				Action: actions[0],
				Order:  &pb.Order{OrderId: 12345},
			},
			nil, // Step 3: Failure
		},
		StepErrors: []error{nil, nil, fmt.Errorf("DB failure")},
	}

	_, err := w.OnPriceUpdate(mockCtx1, &price)
	if err == nil {
		t.Fatal("Expected workflow to fail")
	}

	// Simulate resumption: DBOS would re-run the workflow.
	// Step 1 and 2 should be skipped (returning cached results)
	// Step 3 should be retried.
	// In our mock, we just verify that if Step 1 and 2 return their cached values,
	// the workflow continues to Step 3.

	// Reset mocks for second run
	oe.placedRequests = nil
	pm.applyResults = nil

	mockCtx2 := &MockDBOSContext{
		StepResults: []any{
			actions, // Step 1: Cached
			core.OrderActionResult{ // Step 2: Cached
				Action: actions[0],
				Order:  &pb.Order{OrderId: 12345},
			},
			nil, // Step 3: Success this time
		},
		StepErrors: []error{nil, nil, nil},
	}

	_, err = w.OnPriceUpdate(mockCtx2, &price)
	if err != nil {
		t.Fatalf("Resumed workflow failed: %v", err)
	}

	if len(pm.applyResults) != 1 {
		t.Errorf("Expected 1 result applied on resumption, got %d", len(pm.applyResults))
	}
}
