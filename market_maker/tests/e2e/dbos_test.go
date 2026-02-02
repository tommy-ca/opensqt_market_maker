package e2e

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/engine/durable"
	"market_maker/internal/pb"
	"market_maker/internal/trading/backtest"
	"market_maker/internal/trading/order"
	"market_maker/internal/trading/position"
	"market_maker/internal/trading/strategy"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"testing"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/shopspring/decimal"
)

// MockDBOSContext for E2E validation without a real Postgres
type e2eMockDBOSContext struct {
	dbos.DBOSContext
	StepResults []any
	StepErrors  []error
	StepIndex   int
}

func (m *e2eMockDBOSContext) RunAsStep(ctx dbos.DBOSContext, fn dbos.StepFunc, opts ...dbos.StepOption) (any, error) {
	if m.StepIndex >= len(m.StepResults) {
		return nil, fmt.Errorf("unexpected step call at index %d", m.StepIndex)
	}

	res := m.StepResults[m.StepIndex]
	err := m.StepErrors[m.StepIndex]

	// Only execute if this step is NOT supposed to fail
	// to simulate crash BEFORE the step completes successfully.
	if err == nil {
		_, _ = fn(context.Background())
	}

	m.StepIndex++
	return res, err
}

func TestE2E_DBOS_WorkflowAtomicity(t *testing.T) {
	// Setup telemetry to avoid panic
	_, err := telemetry.Setup("test")
	if err != nil {
		t.Fatalf("Failed to setup telemetry: %v", err)
	}

	exch := backtest.NewSimulatedExchange()
	logger, _ := logging.NewZapLogger("DEBUG")

	orderExecutor := order.NewOrderExecutor(exch, logger)

	gridStrategy := strategy.NewGridStrategy(
		symbol, exch.GetName(),
		decimal.NewFromFloat(10.0),
		decimal.NewFromFloat(100.0),
		decimal.NewFromFloat(5.0),
		5, 5, 2, 3, false, nil, nil, logger,
	)

	pm := position.NewSuperPositionManager(
		symbol, exch.GetName(),
		10.0, 100.0, 5.0,
		5, 5, 2, 3, gridStrategy, nil, nil, logger, nil,
	)

	pm.Initialize(decimal.NewFromInt(45000))

	// We use the workflows directly to simulate DBOS execution
	w := durable.NewTradingWorkflows(pm, orderExecutor)

	price := pb.PriceChange{
		Symbol: symbol,
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(45000)),
	}

	// Scenario: Workflow fails AFTER placing order but BEFORE applying results
	actions, _ := pm.CalculateAdjustments(context.Background(), decimal.NewFromInt(45000))

	// First run fails at the final step
	mockCtx1 := &e2eMockDBOSContext{
		StepResults: []any{
			actions, // Step 1: Success
			core.OrderActionResult{Action: actions[0], Order: &pb.Order{OrderId: 999}}, // Step 2: Success
			core.OrderActionResult{Action: actions[1], Order: &pb.Order{OrderId: 1000}},
			core.OrderActionResult{Action: actions[2], Order: &pb.Order{OrderId: 1001}},
			core.OrderActionResult{Action: actions[3], Order: &pb.Order{OrderId: 1002}},
			core.OrderActionResult{Action: actions[4], Order: &pb.Order{OrderId: 1003}},
			nil, // Step 3: Apply Results - FAIL
		},
		StepErrors: []error{nil, nil, nil, nil, nil, nil, fmt.Errorf("simulated crash")},
	}

	_, err = w.OnPriceUpdate(mockCtx1, &price)
	if err == nil {
		t.Fatal("Expected workflow to fail")
	}

	// Verify that state was NOT updated in PM yet (ApplyResults failed)
	slots := pm.GetSlots()
	for _, s := range slots {
		if s.OrderId != 0 {
			t.Errorf("Slot at %s should not have OrderId yet", pbu.ToGoDecimal(s.Price))
		}
	}

	// Second run: Resumption
	// DBOS would provide the same workflow ID and resume
	// In our mock, Step 1 and 2 return cached results, Step 3 succeeds
	mockCtx2 := &e2eMockDBOSContext{
		StepResults: []any{
			actions,
			core.OrderActionResult{Action: actions[0], Order: &pb.Order{OrderId: 999}},
			core.OrderActionResult{Action: actions[1], Order: &pb.Order{OrderId: 1000}},
			core.OrderActionResult{Action: actions[2], Order: &pb.Order{OrderId: 1001}},
			core.OrderActionResult{Action: actions[3], Order: &pb.Order{OrderId: 1002}},
			core.OrderActionResult{Action: actions[4], Order: &pb.Order{OrderId: 1003}},
			nil, // Step 3: Success
		},
		StepErrors: []error{nil, nil, nil, nil, nil, nil, nil},
	}

	_, err = w.OnPriceUpdate(mockCtx2, &price)
	if err != nil {
		t.Fatalf("Resumed workflow failed: %v", err)
	}

	// Verify state IS now updated
	found := 0
	for _, s := range pm.GetSlots() {
		if s.OrderId != 0 {
			found++
		}
	}
	if found == 0 {
		t.Error("Expected slots to be updated after successful resumption")
	}
}
