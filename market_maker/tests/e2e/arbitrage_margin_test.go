package e2e

import (
	"context"
	"testing"
	"time"

	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/engine/arbengine"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/internal/trading/monitor"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
)

type recordingExchange struct {
	*mock.MockExchange
	useMargin []bool
}

func (r *recordingExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	r.useMargin = append(r.useMargin, req.UseMargin)
	return r.MockExchange.PlaceOrder(ctx, req)
}

func TestE2E_ArbitrageNegativeFundingSetsSpotMarginFlag(t *testing.T) {
	spot := &recordingExchange{MockExchange: mock.NewMockExchange("binance_spot")}
	perp := mock.NewMockExchange("binance_perp")

	exchanges := map[string]core.IExchange{
		"binance_spot": spot,
		"binance_perp": perp,
	}

	logger := &mockLogger{}
	cfg := config.DefaultConfig()
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.ArbitrageSpotExchange = "binance_spot"
	cfg.Trading.ArbitragePerpExchange = "binance_perp"
	cfg.Trading.MinSpreadAPR = 0.01

	fundingMonitor := monitor.NewFundingMonitor(exchanges, logger, cfg.Trading.Symbol)
	fundingMonitor.Start(context.Background())

	eng := arbengine.NewArbitrageEngine(exchanges, nil, fundingMonitor, logger, arbengine.EngineConfig{
		Symbol:                    cfg.Trading.Symbol,
		SpotExchange:              cfg.Trading.ArbitrageSpotExchange,
		PerpExchange:              cfg.Trading.ArbitragePerpExchange,
		MinSpreadAPR:              decimal.NewFromFloat(cfg.Trading.MinSpreadAPR),
		ExitSpreadAPR:             decimal.NewFromFloat(cfg.Trading.ExitSpreadAPR),
		LiquidationThreshold:      decimal.NewFromFloat(cfg.Trading.LiquidationThreshold),
		OrderQuantity:             decimal.NewFromFloat(cfg.Trading.OrderQuantity),
		FundingStalenessThreshold: time.Minute,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	spot.SetFundingRate("BTCUSDT", decimal.Zero)
	perp.SetFundingRate("BTCUSDT", decimal.NewFromFloat(-0.0005))
	fundingMonitor.Start(ctx)

	update := &pb.FundingUpdate{
		Exchange:        "binance_perp",
		Symbol:          "BTCUSDT",
		Rate:            pbu.FromGoDecimal(decimal.NewFromFloat(-0.0005)),
		NextFundingTime: time.Now().Add(1 * time.Hour).UnixMilli(),
		Timestamp:       time.Now().UnixMilli(),
	}

	if err := eng.OnFundingUpdate(ctx, update); err != nil {
		t.Fatalf("funding update failed: %v", err)
	}

	if len(spot.useMargin) == 0 {
		t.Fatalf("expected spot orders to be placed for negative funding scenario")
	}
	if !spot.useMargin[0] {
		t.Fatalf("expected spot leg to set use_margin when shorting")
	}
}
