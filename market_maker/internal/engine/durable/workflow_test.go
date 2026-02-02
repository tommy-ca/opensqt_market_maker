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

type mockPM struct {
	core.IPositionManager
	calculatePrice decimal.Decimal
	actions        []*pb.OrderAction
	applyResults   []core.OrderActionResult
}

func (m *mockPM) CalculateAdjustments(ctx context.Context, price decimal.Decimal) ([]*pb.OrderAction, error) {
	m.calculatePrice = price
	return m.actions, nil
}

func (m *mockPM) ApplyActionResults(results []core.OrderActionResult) error {
	m.applyResults = results
	return nil
}

func (m *mockPM) GetSnapshot() *pb.PositionManagerSnapshot {
	return &pb.PositionManagerSnapshot{}
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
	pm := &mockPM{
		actions: []*pb.OrderAction{
			{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
				Request: &pb.PlaceOrderRequest{Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY},
			},
		},
	}
	oe := &mockOE{}
	w := NewTradingWorkflows(pm, oe)

	price := pb.PriceChange{
		Symbol: "BTCUSDT",
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(45000)),
	}

	mockCtx := &MockDBOSContext{
		StepResults: []any{
			pm.actions, // Step 1: Calculate Adjustments
			core.OrderActionResult{ // Step 2: Place Order
				Action: pm.actions[0],
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

	if !pm.calculatePrice.Equal(decimal.NewFromInt(45000)) {
		t.Errorf("Expected price 45000, got %s", pm.calculatePrice)
	}

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
	pm := &mockPM{
		actions: []*pb.OrderAction{
			{
				Type:    pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
				Request: &pb.PlaceOrderRequest{Symbol: "BTCUSDT", Side: pb.OrderSide_ORDER_SIDE_BUY},
			},
		},
	}
	oe := &mockOE{}
	w := NewTradingWorkflows(pm, oe)

	price := pb.PriceChange{
		Symbol: "BTCUSDT",
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(45000)),
	}

	// First execution fails at Step 3 (Apply Results)
	mockCtx1 := &MockDBOSContext{
		StepResults: []any{
			pm.actions, // Step 1: Success
			core.OrderActionResult{ // Step 2: Success
				Action: pm.actions[0],
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
			pm.actions, // Step 1: Cached
			core.OrderActionResult{ // Step 2: Cached
				Action: pm.actions[0],
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
