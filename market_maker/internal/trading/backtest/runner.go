package backtest

import (
	"context"
	"github.com/shopspring/decimal"
	"market_maker/internal/engine/durable"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
)

type BacktestRunner struct {
	engine   durable.Engine
	exchange *SimulatedExchange
}

func NewBacktestRunner(engine durable.Engine, exch *SimulatedExchange) *BacktestRunner {
	return &BacktestRunner{
		engine:   engine,
		exchange: exch,
	}
}

func (r *BacktestRunner) Run(ctx context.Context, symbol string, prices []decimal.Decimal) error {
	for _, p := range prices {
		// 1. Update exchange (simulates market move and fills)
		r.exchange.UpdatePrice(symbol, p)

		// 2. Notify engine of price move
		err := r.engine.OnPriceUpdate(ctx, &pb.PriceChange{
			Symbol: symbol,
			Price:  pbu.FromGoDecimal(p),
		})
		if err != nil {
			return err
		}

		// r.exchange.WaitForNotifications() // Removed if not implemented
	}
	return nil
}
