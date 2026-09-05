package gridhardening

import (
	"context"
	"testing"
	"time"

	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGridHardening_RiskSafety(t *testing.T) {
	t.Parallel()

	// 1. Setup
	eng, exch, _, riskMock := SetupGridEngine(t)
	ctx := context.Background()
	require.NoError(t, eng.Start(ctx))
	defer func() { _ = eng.Stop() }()

	// 2. Normal State: Place Orders
	price := &pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(decimal.NewFromInt(50000)),
		Timestamp: nil,
	}
	require.NoError(t, eng.OnPriceUpdate(ctx, price))

	// Verify orders placed
	assert.Eventually(t, func() bool {
		orders, _ := exch.GetOpenOrders(ctx, "BTCUSDT", false)
		return len(orders) > 0
	}, 1*time.Second, 10*time.Millisecond, "Expected orders placed")

	// 3. Trigger Risk Event (ATR Spike)
	// Expectation: Cancel all open BUY orders immediately
	riskMock.SetTriggered(true)

	// Trigger update to process risk state
	require.NoError(t, eng.OnPriceUpdate(ctx, price))

	// Verify cancellation of BUYS
	assert.Eventually(t, func() bool {
		orders, _ := exch.GetOpenOrders(ctx, "BTCUSDT", false)
		for _, o := range orders {
			if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
				return false // Should have cancelled all buys
			}
		}
		return true // No buys left
	}, 1*time.Second, 10*time.Millisecond, "Expected ALL Buy orders cancelled")

	// 4. Verify Prevention of New Orders
	// Even with price movement that would trigger buys, risk should prevent it.
	priceDrop := &pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(decimal.NewFromInt(45000)), // Huge drop, should trigger buys normally
		Timestamp: nil,
	}
	require.NoError(t, eng.OnPriceUpdate(ctx, priceDrop))

	// Verify still no buys
	time.Sleep(50 * time.Millisecond)
	orders, _ := exch.GetOpenOrders(ctx, "BTCUSDT", false)
	for _, o := range orders {
		if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
			t.Fatal("Risk monitor failed to prevent new BUY order")
		}
	}
}
