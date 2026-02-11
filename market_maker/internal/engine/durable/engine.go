package durable

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/engine"
	"market_maker/internal/pb"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

// DBOSEngine implements the engine.Engine interface using DBOS
type DBOSEngine struct {
	dbosCtx         dbos.DBOSContext
	workflows       *TradingWorkflows
	positionManager core.IPositionManager
	logger          core.ILogger
}

// NewDBOSEngine creates a new DBOS workflow engine
func NewDBOSEngine(
	dbosCtx dbos.DBOSContext,
	pm core.IPositionManager,
	oe core.IOrderExecutor,
	strategy core.IStrategy,
	logger core.ILogger,
) engine.Engine {
	return &DBOSEngine{
		dbosCtx:         dbosCtx,
		workflows:       NewTradingWorkflows(pm, oe, strategy),
		positionManager: pm,
		logger:          logger.WithField("component", "dbos_engine"),
	}
}

// Start starts the engine and DBOS runtime
func (e *DBOSEngine) Start(ctx context.Context) error {
	e.logger.Info("Starting DBOS engine")
	return e.dbosCtx.Launch()
}

// Stop stops the engine
func (e *DBOSEngine) Stop() error {
	e.logger.Info("Stopping DBOS engine")
	// Shutdown takes a timeout, 30s as default
	e.dbosCtx.Shutdown(30 * 1000 * 1000 * 1000)
	return nil
}

// OnPriceUpdate triggers a durable workflow for price updates
func (e *DBOSEngine) OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error {
	handle, err := e.dbosCtx.RunWorkflow(e.dbosCtx, e.workflows.OnPriceUpdate, price)
	if err != nil {
		return fmt.Errorf("failed to start price update workflow: %w", err)
	}

	_, err = handle.GetResult()
	return err
}

// OnOrderUpdate triggers a durable workflow for order updates
func (e *DBOSEngine) OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error {
	handle, err := e.dbosCtx.RunWorkflow(e.dbosCtx, e.workflows.OnOrderUpdate, update)
	if err != nil {
		return fmt.Errorf("failed to start order update workflow: %w", err)
	}

	_, err = handle.GetResult()
	return err
}

// OnFundingUpdate triggers a durable workflow for funding updates
func (e *DBOSEngine) OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error {
	// DBOSEngine (Grid) currently ignores funding updates
	return nil
}

// OnPositionUpdate triggers a durable workflow for position updates
func (e *DBOSEngine) OnPositionUpdate(ctx context.Context, position *pb.Position) error {
	// DBOSEngine (Grid) currently ignores position updates
	return nil
}

// OnAccountUpdate triggers a durable workflow for account updates
func (e *DBOSEngine) OnAccountUpdate(ctx context.Context, account *pb.Account) error {
	return nil
}
