package observability

import (
	"context"
	"net"
	"testing"
	"time"

	"market_maker/internal/pb"
	"market_maker/internal/risk"
	"market_maker/internal/trading/backtest"
	"market_maker/internal/trading/grid"
	"market_maker/internal/trading/order"
	"market_maker/internal/trading/position"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"

	"market_maker/internal/core"
	"market_maker/internal/engine/gridengine"
	"market_maker/internal/engine/simple"
	"market_maker/internal/infrastructure/grpc/client"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
)

func setupTelemetry() {
	meter := otel.GetMeterProvider().Meter("e2e")
	_ = telemetry.GetGlobalMetrics().InitMetrics(meter)
}

func TestE2E_ObservabilityFlow(t *testing.T) {
	setupTelemetry()
	logger, _ := logging.NewZapLogger("INFO")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Setup Simulated Exchange
	exch := backtest.NewSimulatedExchange()
	symbol := "BTCUSDT"

	// 2. Setup Market Maker (Hub)
	// Using the new SlotManager instead of legacy PositionManager
	riskPool := concurrency.NewWorkerPool(concurrency.PoolConfig{Name: "RiskPool", MaxWorkers: 2, MaxCapacity: 10}, logger)
	defer riskPool.Stop()

	// New SlotManager
	pm := grid.NewSlotManager(symbol, 2, logger)
	pm.Initialize(decimal.NewFromInt(50000))

	rm := risk.NewRiskMonitor(exch, logger, []string{symbol}, "1m", 2.0, 5, 1, "Any", riskPool)
	cb := risk.NewCircuitBreaker(risk.CircuitConfig{})

	oe := order.NewOrderExecutor(exch, logger)
	store := simple.NewMemoryStore()

	// Use GridEngine instead of SimpleEngine
	cfg := gridengine.Config{

		Symbol:         symbol,
		PriceInterval:  decimal.NewFromInt(10),
		OrderQuantity:  decimal.NewFromInt(1),
		MinOrderValue:  decimal.NewFromInt(5),
		BuyWindowSize:  2,
		SellWindowSize: 2,
		PriceDecimals:  2,
		QtyDecimals:    3,
		IsNeutral:      true,
	}
	eng := gridengine.NewGridEngine(map[string]core.IExchange{"mock": exch}, oe, rm, store, logger, nil, pm, cfg)

	// Start MM gRPC Server
	lis, err := net.Listen("tcp", ":0")
	assert.NoError(t, err)
	grpcServer := grpc.NewServer()
	riskSvc := risk.NewRiskServiceServer(rm, nil, cb)
	posSvc := position.NewPositionServiceServer(pm, "mock")
	pb.RegisterRiskServiceServer(grpcServer, riskSvc)
	pb.RegisterPositionServiceServer(grpcServer, posSvc)

	go grpcServer.Serve(lis)
	defer grpcServer.GracefulStop()

	// 3. Setup gRPC Client
	mmAddr := lis.Addr().String()
	mmClient, err := client.NewMarketMakerClient(mmAddr, logger)
	assert.NoError(t, err)
	defer mmClient.Close()

	// 4. Verification Flow
	updateReceived := make(chan *pb.PositionUpdate, 10)
	mmClient.SubscribePositions(ctx, []string{symbol}, func(upd *pb.PositionUpdate) {
		t.Logf("Received update: %v, symbol: %v", upd.UpdateType, upd.Position.Symbol)
		updateReceived <- upd
	})

	// Initial snapshot?
	// The mmClient just subscribes to the stream.
	// We need to trigger something that causes an update in PM.

	// Give subscription time to register
	time.Sleep(100 * time.Millisecond)

	// Inject a price change to trigger strategy -> order placement
	eng.OnPriceUpdate(ctx, &pb.PriceChange{
		Symbol: symbol,
		Price:  pbu.FromGoDecimal(decimal.NewFromInt(50005)),
	})

	// Wait for orders to be placed
	time.Sleep(200 * time.Millisecond)

	orders, _ := exch.GetOpenOrders(ctx, symbol, false)
	t.Logf("Open orders: %d", len(orders))
	for _, o := range orders {
		t.Logf("Order: ID=%d Price=%s Side=%s", o.OrderId, pbu.ToGoDecimal(o.Price), o.Side)
	}

	// Manually trigger the OnOrderUpdate in engine because backtest doesn't auto-wire callbacks in this raw setup
	exch.StartOrderStream(ctx, func(upd *pb.OrderUpdate) {
		// t.Logf("Forwarding order update: %v %v", upd.OrderId, upd.Status)
		if err := eng.OnOrderUpdate(ctx, upd); err != nil {
			t.Logf("Engine OnOrderUpdate failed: %v", err)
		}
	})

	// Trigger a fill on the exchange
	// We expect a buy order at 50000 (anchor) - 10 = 49990
	exch.UpdatePrice(symbol, decimal.NewFromInt(49980))

	select {
	case upd := <-updateReceived:
		assert.Equal(t, symbol, upd.Position.Symbol)
		assert.Equal(t, "filled", upd.UpdateType)
		t.Logf("Successfully received gRPC update from engine: %v", upd.UpdateType)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for gRPC position update")
	}
}
