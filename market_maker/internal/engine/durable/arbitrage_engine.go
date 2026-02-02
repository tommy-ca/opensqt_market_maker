package durable

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/engine/arbengine"
	"market_maker/internal/pb"
	"market_maker/internal/trading/arbitrage"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/shopspring/decimal"
)

// DBOSArbitrageEngine implements the engine.Engine interface using DBOS
type DBOSArbitrageEngine struct {
	dbosCtx        dbos.DBOSContext
	workflows      *ArbitrageWorkflows
	exchanges      map[string]core.IExchange
	monitor        core.IRiskMonitor
	fundingMonitor core.IFundingMonitor
	logger         core.ILogger

	// Building Blocks
	strategy   *arbitrage.Strategy
	legManager *arbitrage.LegManager

	// Config
	symbol        string
	spotExchange  string
	perpExchange  string
	orderQuantity decimal.Decimal
	stalenessTTL  time.Duration

	// State
	mu                  sync.Mutex
	lastNextFundingTime int64
}

func NewDBOSArbitrageEngine(
	dbosCtx dbos.DBOSContext,
	exchanges map[string]core.IExchange,
	monitor core.IRiskMonitor,
	fundingMonitor core.IFundingMonitor,
	logger core.ILogger,
	cfg arbengine.EngineConfig,
) engine.Engine {
	logicCfg := arbitrage.StrategyConfig{
		MinSpreadAPR:         cfg.MinSpreadAPR,
		ExitSpreadAPR:        cfg.ExitSpreadAPR,
		LiquidationThreshold: cfg.LiquidationThreshold,
	}

	return &DBOSArbitrageEngine{
		dbosCtx:        dbosCtx,
		workflows:      NewArbitrageWorkflows(exchanges),
		exchanges:      exchanges,
		monitor:        monitor,
		fundingMonitor: fundingMonitor,
		logger:         logger.WithField("component", "dbos_arbitrage_engine"),
		strategy:       arbitrage.NewStrategy(logicCfg),
		legManager:     arbitrage.NewLegManager(exchanges, logger),
		symbol:         cfg.Symbol,
		spotExchange:   cfg.SpotExchange,
		perpExchange:   cfg.PerpExchange,
		orderQuantity:  cfg.OrderQuantity,
		stalenessTTL:   cfg.FundingStalenessThreshold,
	}
}

func (e *DBOSArbitrageEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting DBOS Arbitrage Engine")

	// Sync State
	if err := e.legManager.SyncState(ctx, e.spotExchange, e.symbol); err != nil {
		return err
	}
	if err := e.legManager.SyncState(ctx, e.perpExchange, e.symbol); err != nil {
		return err
	}

	return e.dbosCtx.Launch()
}

func (e *DBOSArbitrageEngine) Stop() error {
	e.logger.Info("Stopping DBOS Arbitrage Engine")
	e.dbosCtx.Shutdown(30 * 1000 * 1000 * 1000)
	return nil
}

func (e *DBOSArbitrageEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	return nil
}

func (e *DBOSArbitrageEngine) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	return e.legManager.SyncState(ctx, update.Exchange, update.Symbol)
}

func (e *DBOSArbitrageEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
	// Only care about our exchanges
	if (update.Exchange != e.perpExchange && update.Exchange != e.spotExchange) || update.Symbol != e.symbol {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Deduplicate by NextFundingTime
	if update.NextFundingTime <= e.lastNextFundingTime && update.NextFundingTime != 0 {
		return nil
	}

	// Check staleness for both legs
	if e.fundingMonitor.IsStale(e.spotExchange, e.symbol, e.stalenessTTL) {
		e.logger.Warn("Spot funding stale, skipping decision", "exchange", e.spotExchange)
		return nil
	}
	if e.fundingMonitor.IsStale(e.perpExchange, e.symbol, e.stalenessTTL) {
		e.logger.Warn("Perp funding stale, skipping decision", "exchange", e.perpExchange)
		return nil
	}

	// Get rates
	spotRate, err := e.fundingMonitor.GetRate(e.spotExchange, e.symbol)
	if err != nil {
		return nil
	}
	perpRate, err := e.fundingMonitor.GetRate(e.perpExchange, e.symbol)
	if err != nil {
		return nil
	}

	// Compute spread and APR
	spread := arbitrage.ComputeSpread(spotRate, perpRate)
	// For now assume 8h interval
	apr := arbitrage.AnnualizeSpread(spread, decimal.NewFromInt(8))

	isPositionOpen := e.legManager.HasOpenPosition(e.symbol)

	// Risk Check
	isRiskTriggered := false
	if e.monitor != nil {
		isRiskTriggered = e.monitor.IsTriggered()
	}

	action := e.strategy.CalculateAction(apr, isPositionOpen)

	switch action {
	case arbitrage.ActionEntryPositive:
		if isRiskTriggered {
			e.logger.Warn("Risk triggered, skipping entry")
			return nil
		}
		e.logger.Info("Strategy: ENTRY (Positive) signaled", "apr", apr.String())
		if err := e.triggerWorkflow(ctx, "entry_positive", update.NextFundingTime); err != nil {
			return err
		}
		e.lastNextFundingTime = update.NextFundingTime
		return nil
	case arbitrage.ActionEntryNegative:
		if isRiskTriggered {
			e.logger.Warn("Risk triggered, skipping entry")
			return nil
		}
		e.logger.Info("Strategy: ENTRY (Negative) signaled", "apr", apr.String())
		if err := e.triggerWorkflow(ctx, "entry_negative", update.NextFundingTime); err != nil {
			return err
		}
		e.lastNextFundingTime = update.NextFundingTime
		return nil
	case arbitrage.ActionExit:
		e.logger.Info("Strategy: EXIT signaled", "apr", apr.String())
		if err := e.triggerWorkflow(ctx, "exit", update.NextFundingTime); err != nil {
			return err
		}
		e.lastNextFundingTime = update.NextFundingTime
		return nil
	}

	return nil
}

func (e *DBOSArbitrageEngine) OnPositionUpdate(ctx context.Context, pos *pb.Position) error {
	if pos.Symbol != e.symbol {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.strategy.ShouldEmergencyExit(pos) {
		e.logger.Warn("Strategy: EMERGENCY EXIT signaled (Liquidation Guard)")
		return e.triggerWorkflow(ctx, "exit", 0)
	}

	return nil
}

func (e *DBOSArbitrageEngine) OnAccountUpdate(ctx context.Context, account *pb.Account) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	action := e.strategy.EvaluateUMAccountHealth(account)
	switch action {
	case arbitrage.ActionExit:
		e.logger.Warn("UM Account Health Critical, initiating emergency exit")
		return e.triggerWorkflow(ctx, "exit", 0)
	case arbitrage.ActionReduceExposure:
		e.logger.Warn("UM Account Health Warning, reducing exposure by 50%")
		// TODO: Implement durable partial reduction workflow
		// For now just exit
		return e.triggerWorkflow(ctx, "exit", 0)
	}

	return nil
}

func (e *DBOSArbitrageEngine) triggerWorkflow(ctx context.Context, workflowType string, nextFundingTime int64) error {
	if workflowType == "entry_positive" || workflowType == "entry_negative" {
		spotSide := pb.OrderSide_ORDER_SIDE_BUY
		perpSide := pb.OrderSide_ORDER_SIDE_SELL
		if workflowType == "entry_negative" {
			spotSide = pb.OrderSide_ORDER_SIDE_SELL
			perpSide = pb.OrderSide_ORDER_SIDE_BUY
		}

		req := &pb.ArbitrageEntryRequest{
			SpotExchange: e.spotExchange,
			PerpExchange: e.perpExchange,
			SpotOrder: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          spotSide,
				Type:          pb.OrderType_ORDER_TYPE_MARKET,
				Quantity:      pbu.FromGoDecimal(e.orderQuantity),
				ClientOrderId: fmt.Sprintf("dbos_spot_%s_%d", e.symbol, nextFundingTime),
				UseMargin:     workflowType == "entry_negative",
			},
			PerpOrder: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          perpSide,
				Type:          pb.OrderType_ORDER_TYPE_MARKET,
				Quantity:      pbu.FromGoDecimal(e.orderQuantity),
				ClientOrderId: fmt.Sprintf("dbos_perp_%s_%d", e.symbol, nextFundingTime),
			},
		}
		_, err := e.dbosCtx.RunWorkflow(e.dbosCtx, e.workflows.ExecuteSpotPerpEntry, req)
		return err
	} else if workflowType == "exit" {
		// Determine sides from LegManager
		spotSide := pb.OrderSide_ORDER_SIDE_SELL // Default close for Long
		perpSide := pb.OrderSide_ORDER_SIDE_BUY  // Default close for Short

		currentSpotSide := e.legManager.GetSide(e.spotExchange, e.symbol)
		if currentSpotSide == pb.OrderSide_ORDER_SIDE_SELL {
			spotSide = pb.OrderSide_ORDER_SIDE_BUY
		}

		currentPerpSide := e.legManager.GetSide(e.perpExchange, e.symbol)
		if currentPerpSide == pb.OrderSide_ORDER_SIDE_BUY {
			perpSide = pb.OrderSide_ORDER_SIDE_SELL
		}

		req := &pb.ArbitrageExitRequest{
			SpotExchange: e.spotExchange,
			PerpExchange: e.perpExchange,
			SpotOrder: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          spotSide,
				Type:          pb.OrderType_ORDER_TYPE_MARKET,
				Quantity:      pbu.FromGoDecimal(e.orderQuantity),
				ClientOrderId: fmt.Sprintf("dbos_exit_spot_%s_%d", e.symbol, nextFundingTime),
				UseMargin:     currentSpotSide == pb.OrderSide_ORDER_SIDE_SELL, // Close margin if we were short
			},
			PerpOrder: &pb.PlaceOrderRequest{
				Symbol:        e.symbol,
				Side:          perpSide,
				Type:          pb.OrderType_ORDER_TYPE_MARKET,
				Quantity:      pbu.FromGoDecimal(e.orderQuantity),
				ClientOrderId: fmt.Sprintf("dbos_exit_perp_%s_%d", e.symbol, nextFundingTime),
			},
		}
		_, err := e.dbosCtx.RunWorkflow(e.dbosCtx, e.workflows.ExecuteSpotPerpExit, req)
		return err
	}
	return nil
}
