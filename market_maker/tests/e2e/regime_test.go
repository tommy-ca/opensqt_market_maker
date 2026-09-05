package e2e

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/engine/gridengine"
	"market_maker/internal/engine/simple"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/internal/trading/grid"
	"market_maker/internal/trading/order"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_RegimeFiltering(t *testing.T) {
	// 1. Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger, _ := logging.NewZapLogger("DEBUG")
	exch := mock.NewMockExchange("mock")
	exch.SetTicker(&pb.Ticker{
		Symbol:    "BTCUSDT",
		LastPrice: pbu.FromGoDecimal(decimal.NewFromFloat(100.0)),
	})

	sm := grid.NewSlotManager("BTCUSDT", 2, logger)
	cfg := gridengine.Config{
		StrategyID:     "scenario1",
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromFloat(1.0),
		OrderQuantity:  decimal.NewFromFloat(1.0),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		IsNeutral:      true,
	}

	oe := order.NewOrderExecutor(exch, logger)
	store := simple.NewMemoryStore()

	eng := gridengine.NewGridEngine(
		map[string]core.IExchange{"mock": exch},
		oe,
		nil, // risk monitor
		store,
		logger,
		nil, // pool
		sm,
		cfg,
	)

	// Start engine
	err := eng.Start(ctx)
	require.NoError(t, err)

	// SCENARIO 1: RANGE (Normal)
	err = eng.OnPriceUpdate(ctx, &pb.PriceChange{
		Symbol: "BTCUSDT", Price: pbu.FromGoDecimal(decimal.NewFromInt(100)),
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		orders := exch.GetOrders()
		hasBuy := false
		hasSell := false
		for _, o := range orders {
			if o.Status == pb.OrderStatus_ORDER_STATUS_NEW {
				if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
					hasBuy = true
				}
				if o.Side == pb.OrderSide_ORDER_SIDE_SELL {
					hasSell = true
				}
			}
		}
		return hasBuy && hasSell
	}, 2*time.Second, 10*time.Millisecond, "Should have buy and sell orders in Range")

	// SCENARIO 2: BULL TREND (Sells disabled for opening)
	_ = exch.CancelAllOrders(ctx, "BTCUSDT", false)
	assert.Eventually(t, func() bool {
		orders, _ := exch.GetOpenOrders(ctx, "BTCUSDT", false)
		return len(orders) == 0
	}, 1*time.Second, 10*time.Millisecond, "Orders should be cancelled before scenario 2")

	openOrders, _ := exch.GetOpenOrders(ctx, "BTCUSDT", false)
	sm.SyncOrders(openOrders, decimal.Zero)

	eng.GetCoordinator().SetStrategyID("scenario2")
	eng.GetCoordinator().GetRegimeMonitor().UpdateFromIndicators(80.0, 1.0)

	err = eng.OnPriceUpdate(ctx, &pb.PriceChange{
		Symbol: "BTCUSDT", Price: pbu.FromGoDecimal(decimal.NewFromInt(105)),
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		orders := exch.GetOrders()
		hasBuy := false
		hasSell := false
		for _, o := range orders {
			if o.Status == pb.OrderStatus_ORDER_STATUS_NEW {
				if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
					hasBuy = true
				}
				if o.Side == pb.OrderSide_ORDER_SIDE_SELL {
					hasSell = true
				}
			}
		}
		return hasBuy && !hasSell
	}, 2*time.Second, 10*time.Millisecond, "Should have buy orders and NOT have sell orders in Bull Trend")

	// SCENARIO 3: BEAR TREND (Buys disabled for opening)
	_ = exch.CancelAllOrders(ctx, "BTCUSDT", false)
	assert.Eventually(t, func() bool {
		orders, _ := exch.GetOpenOrders(ctx, "BTCUSDT", false)
		return len(orders) == 0
	}, 1*time.Second, 10*time.Millisecond, "Orders should be cancelled before scenario 3")

	openOrders, _ = exch.GetOpenOrders(ctx, "BTCUSDT", false)
	sm.SyncOrders(openOrders, decimal.Zero)

	eng.GetCoordinator().SetStrategyID("scenario3")
	eng.GetCoordinator().GetRegimeMonitor().UpdateFromIndicators(20.0, -1.0)

	err = eng.OnPriceUpdate(ctx, &pb.PriceChange{
		Symbol: "BTCUSDT", Price: pbu.FromGoDecimal(decimal.NewFromInt(95)),
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		orders := exch.GetOrders()
		hasBuy := false
		hasSell := false
		for _, o := range orders {
			if o.Status == pb.OrderStatus_ORDER_STATUS_NEW {
				if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
					hasBuy = true
				}
				if o.Side == pb.OrderSide_ORDER_SIDE_SELL {
					hasSell = true
				}
			}
		}
		return !hasBuy && hasSell
	}, 2*time.Second, 10*time.Millisecond, "Should NOT have buy orders and have sell orders in Bear Trend")
}
