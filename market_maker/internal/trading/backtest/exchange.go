package backtest

import (
	"context"
	"github.com/shopspring/decimal"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
)

// SimulatedExchange extends MockExchange with backtest-specific logic
type SimulatedExchange struct {
	mock.MockExchange
}

func NewSimulatedExchange() *SimulatedExchange {
	return &SimulatedExchange{
		MockExchange: *mock.NewMockExchange("backtest"),
	}
}

// UpdatePrice simulates market move and triggers fills

func (s *SimulatedExchange) UpdatePrice(symbol string, price decimal.Decimal) {
	// 2. Check open orders for fills
	openOrders, _ := s.GetOpenOrders(context.Background(), symbol, false)
	for _, o := range openOrders {
		shouldFill := false

		orderPrice := pbu.ToGoDecimal(o.Price)
		if o.Side == pb.OrderSide_ORDER_SIDE_BUY && price.LessThanOrEqual(orderPrice) {
			shouldFill = true
		} else if o.Side == pb.OrderSide_ORDER_SIDE_SELL && price.GreaterThanOrEqual(orderPrice) {
			shouldFill = true
		}

		if shouldFill {
			s.SimulateOrderFill(o.OrderId, pbu.ToGoDecimal(o.Quantity), orderPrice)
		}
	}
}

// StartOrderStream override to ensure callback is captured in SimulatedExchange
func (s *SimulatedExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	// Just use parent implementation, but logging might be helpful
	return s.MockExchange.StartOrderStream(ctx, callback)
}
