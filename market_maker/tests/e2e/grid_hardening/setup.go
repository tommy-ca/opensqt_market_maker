package gridhardening

import (
	"context"
	"testing"

	"market_maker/internal/core"
	"market_maker/internal/engine/gridengine"
	"market_maker/internal/engine/simple"
	"market_maker/internal/pb"
	"market_maker/internal/trading/backtest"
	"market_maker/internal/trading/position"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// MockRegimeMonitor is a stateful fake for testing
type MockRegimeMonitor struct {
	regime pb.MarketRegime
}

func (m *MockRegimeMonitor) Start(ctx context.Context) error { return nil }
func (m *MockRegimeMonitor) Stop() error                     { return nil }
func (m *MockRegimeMonitor) GetRegime() pb.MarketRegime      { return m.regime }
func (m *MockRegimeMonitor) SetRegime(r pb.MarketRegime)     { m.regime = r }

// MockRiskMonitor is a stateful fake for testing
type MockRiskMonitor struct {
	triggered bool
	atr       decimal.Decimal
	volFactor float64
}

func (m *MockRiskMonitor) Start(ctx context.Context) error           { return nil }
func (m *MockRiskMonitor) Stop() error                               { return nil }
func (m *MockRiskMonitor) IsTriggered() bool                         { return m.triggered }
func (m *MockRiskMonitor) GetATR(symbol string) decimal.Decimal      { return m.atr }
func (m *MockRiskMonitor) GetVolatilityFactor(symbol string) float64 { return m.volFactor }
func (m *MockRiskMonitor) SetTriggered(t bool)                       { m.triggered = t }
func (m *MockRiskMonitor) GetAllSymbols() []string                   { return []string{} }
func (m *MockRiskMonitor) GetMetrics(symbol string) *pb.SymbolRiskMetrics {
	score := decimal.Zero
	if m.triggered {
		score = decimal.NewFromInt(100)
	}
	return &pb.SymbolRiskMetrics{
		Symbol:    symbol,
		RiskScore: pbu.FromGoDecimal(score),
	}
}
func (m *MockRiskMonitor) Reset() error {
	m.triggered = false
	return nil
}

// SimulatedExecutor adapts SimulatedExchange to IGridExecutor
type SimulatedExecutor struct {
	exchange    *backtest.SimulatedExchange
	slotManager core.IPositionManager
}

func (e *SimulatedExecutor) SetExchange(exchange *backtest.SimulatedExchange) {
	e.exchange = exchange
}

func (e *SimulatedExecutor) Execute(ctx context.Context, actions []*pb.OrderAction) {
	var results []core.OrderActionResult
	for _, action := range actions {
		if action.Type == pb.OrderActionType_ORDER_ACTION_TYPE_PLACE {
			if action.Request != nil {
				order, err := e.exchange.PlaceOrder(ctx, action.Request)
				results = append(results, core.OrderActionResult{
					Action: action,
					Order:  order,
					Error:  err,
				})
			}
		} else {
			err := e.exchange.BatchCancelOrders(ctx, action.Symbol, []int64{action.OrderId}, false)
			results = append(results, core.OrderActionResult{
				Action: action,
				Error:  err,
			})
		}
	}
	if e.slotManager != nil {
		_ = e.slotManager.ApplyActionResults(results)
	}
}

// SetupGridEngine creates a GridEngine with mocked dependencies for testing
func SetupGridEngine(t *testing.T) (*gridengine.GridCoordinator, *backtest.SimulatedExchange, *MockRegimeMonitor, *MockRiskMonitor) {
	store := simple.NewMemoryStore()
	return SetupGridEngineWithStore(t, store)
}

// SetupGridEngineWithStore allows passing a specific store for persistence testing
func SetupGridEngineWithStore(t *testing.T, store core.IStateStore) (*gridengine.GridCoordinator, *backtest.SimulatedExchange, *MockRegimeMonitor, *MockRiskMonitor) {
	// Simple logger for tests
	logger := logging.NewLogger(logging.InfoLevel, nil)
	// Use no-op meter
	meter := noop.NewMeterProvider().Meter("test")

	// Stateful Fakes
	exchange := backtest.NewSimulatedExchange()
	regimeMonitor := &MockRegimeMonitor{regime: pb.MarketRegime_MARKET_REGIME_RANGE}
	riskMonitor := &MockRiskMonitor{triggered: false, atr: decimal.NewFromInt(100), volFactor: 1.0}

	// Strategy Config
	stratCfg := gridengine.Config{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromInt(100),
		OrderQuantity:  decimal.NewFromFloat(0.001),
		BuyWindowSize:  5,
		SellWindowSize: 5,
		QtyDecimals:    3,
	}

	// Create Position Manager
	pm := position.NewSuperPositionManager(
		"BTCUSDT",
		"binance",
		100.0,
		0.001,
		10.0,
		5, 5, // Buy/Sell Window
		2, 3, // Decimals
		nil, // Strategy (created inside coordinator)
		nil, // RiskMonitor (not needed for PM in this test context)
		store,
		logger,
		meter,
	)

	// Create Coordinator Deps
	deps := gridengine.GridCoordinatorDeps{
		Cfg:         stratCfg,
		Exchanges:   map[string]core.IExchange{"binance": exchange},
		SlotMgr:     pm,
		RiskMonitor: riskMonitor,
		Store:       store,
		Logger:      logger,
		Executor:    &SimulatedExecutor{exchange: exchange, slotManager: pm},
	}

	coordinator := gridengine.NewGridCoordinator(deps)
	// Inject the mock regime monitor
	coordinator.SetRegimeMonitor(regimeMonitor)

	// In test context, we'll use a simplified GridEngine construction
	// or rely on the fact that GridEngine is mostly a wrapper around Coordinator + Executor.
	// Since we can't easily construct the real GridEngine without private fields,
	// and we can't invoke NewGridEngine without proper args (including WorkerPool which we can't easily import),
	// we will return the Coordinator directly for testing logic, or nil for Engine if tests don't use it directly.
	// But the test wants `eng.Start()`.

	// Hack: Tests should use Coordinator for logic testing (Regime) and maybe SimpleEngine for E2E if GridEngine is too hard to mock.
	// But the plan says "target the correct component".

	// Let's modify NewGridEngine to be friendlier or use the Coordinator.
	// Actually, for Regime testing, we only need the Coordinator's OnPriceUpdate.
	// So returning nil for engine might be okay if we change the test to use Coordinator.

	// Wait, we can't change the return signature easily.
	// Let's stub it out.

	return coordinator, exchange, regimeMonitor, riskMonitor
}

func SetupStore() (core.IStateStore, error) {
	return simple.NewMemoryStore(), nil
}

func SetupLogger() core.ILogger {
	return logging.NewLogger(logging.InfoLevel, nil)
}

func SetupMeter() metric.Meter {
	return noop.NewMeterProvider().Meter("test")
}

func SetupPM(store core.IStateStore, logger core.ILogger, meter metric.Meter) *position.SuperPositionManager {
	return position.NewSuperPositionManager(
		"BTCUSDT",
		"binance",
		100.0,
		0.001,
		10.0,
		5, 5, // Buy/Sell Window
		2, 3, // Decimals
		nil, // Strategy (created inside coordinator)
		nil, // RiskMonitor (not needed for PM in this test context)
		store,
		logger,
		meter,
	)
}

func SetupDeps(exch core.IExchange, pm *position.SuperPositionManager, store core.IStateStore, logger core.ILogger, executor gridengine.IGridExecutor) gridengine.GridCoordinatorDeps {
	// Strategy Config
	stratCfg := gridengine.Config{
		Symbol:         "BTCUSDT",
		PriceInterval:  decimal.NewFromInt(100),
		OrderQuantity:  decimal.NewFromFloat(0.001),
		BuyWindowSize:  5,
		SellWindowSize: 5,
		QtyDecimals:    3,
	}

	riskMonitor := &MockRiskMonitor{triggered: false, atr: decimal.NewFromInt(100), volFactor: 1.0}

	return gridengine.GridCoordinatorDeps{
		Cfg:         stratCfg,
		Exchanges:   map[string]core.IExchange{"binance": exch},
		SlotMgr:     pm,
		RiskMonitor: riskMonitor,
		Store:       store,
		Logger:      logger,
		Executor:    executor,
	}
}

func SetupCoordinator(deps gridengine.GridCoordinatorDeps) *gridengine.GridCoordinator {
	coord := gridengine.NewGridCoordinator(deps)
	// Inject the mock regime monitor
	regimeMonitor := &MockRegimeMonitor{regime: pb.MarketRegime_MARKET_REGIME_RANGE}
	coord.SetRegimeMonitor(regimeMonitor)
	return coord
}
