package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/shopspring/decimal"
)

type RegistryEntry struct {
	Symbol         string          `json:"symbol"`
	Exchange       string          `json:"exchange"`
	Config         json.RawMessage `json:"config"`
	Status         string          `json:"status"`
	TargetNotional decimal.Decimal `json:"target_notional"`
	QualityScore   decimal.Decimal `json:"quality_score"`
	Sector         string          `json:"sector"`
}

type OrchestratorWorkflows struct {
	orch *Orchestrator
	db   *sql.DB
}

func NewOrchestratorWorkflows(orch *Orchestrator, db *sql.DB) *OrchestratorWorkflows {
	return &OrchestratorWorkflows{orch: orch, db: db}
}

// AddTradingPair is a durable workflow to add a new symbol
func (w *OrchestratorWorkflows) AddTradingPair(ctx dbos.DBOSContext, input any) (any, error) {
	entry := input.(RegistryEntry)
	// 1. Transactionally update registry
	_, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		_, err := w.db.Exec(`
				INSERT INTO symbol_registry (symbol, exchange, config, status, target_notional, quality_score, sector)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (symbol) DO UPDATE SET
					exchange = EXCLUDED.exchange,
					config = EXCLUDED.config,
					status = EXCLUDED.status,
					target_notional = EXCLUDED.target_notional,
					quality_score = EXCLUDED.quality_score,
					sector = EXCLUDED.sector`,
			entry.Symbol, entry.Exchange, entry.Config, entry.Status, entry.TargetNotional, entry.QualityScore, entry.Sector)
		return nil, err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update registry: %w", err)
	}

	// 2. Side effect: Start the manager in-memory
	_, err = ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		eng, err := w.orch.factory.CreateEngine(entry.Symbol, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create engine: %w", err)
		}
		w.orch.AddSymbol(entry.Symbol, eng)
		if m := w.orch.managers[entry.Symbol]; m != nil {
			return nil, m.Start()
		}
		return nil, nil
	})

	return nil, err
}

// RemoveTradingPair is a durable workflow to remove a symbol
func (w *OrchestratorWorkflows) RemoveTradingPair(ctx dbos.DBOSContext, input any) (any, error) {
	symbol := input.(string)
	// 1. Transactionally remove from registry
	_, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		_, err := w.db.Exec("DELETE FROM symbol_registry WHERE symbol = $1", symbol)
		return nil, err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to remove from registry: %w", err)
	}

	// 2. Side effect: Stop and remove the manager in-memory
	_, err = ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		w.orch.RemoveSymbol(symbol)
		return nil, nil
	})

	return nil, err
}

// Recover restores the orchestrator state from the database
func (w *OrchestratorWorkflows) Recover(ctx dbos.DBOSContext) (any, error) {
	entries, err := w.GetActiveSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get active symbols: %w", err)
	}

	for _, entry := range entries {
		currentEntry := entry
		_, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
			eng, err := w.orch.factory.CreateEngine(currentEntry.Symbol, currentEntry.Config)
			if err != nil {
				return nil, fmt.Errorf("failed to create engine: %w", err)
			}
			w.orch.AddSymbol(currentEntry.Symbol, eng)
			if m := w.orch.managers[currentEntry.Symbol]; m != nil {
				return nil, m.Start()
			}
			return nil, nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to recover symbol %s: %w", currentEntry.Symbol, err)
		}
	}
	return nil, nil
}

// GetActiveSymbols retrieves all symbols from the registry
func (w *OrchestratorWorkflows) GetActiveSymbols(ctx dbos.DBOSContext) ([]RegistryEntry, error) {
	res, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		rows, err := w.db.Query("SELECT symbol, exchange, config, status, target_notional, quality_score, sector FROM symbol_registry WHERE status = 'ACTIVE'")
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var entries []RegistryEntry
		for rows.Next() {
			var e RegistryEntry
			if err := rows.Scan(&e.Symbol, &e.Exchange, &e.Config, &e.Status, &e.TargetNotional, &e.QualityScore, &e.Sector); err != nil {
				return nil, err
			}
			entries = append(entries, e)
		}
		return entries, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]RegistryEntry), nil
}

func (w *OrchestratorWorkflows) InitializeSchema(ctx dbos.DBOSContext) error {
	_, err := ctx.RunAsStep(ctx, func(ctx context.Context) (any, error) {
		_, err := w.db.Exec(`
				CREATE TABLE IF NOT EXISTS symbol_registry (
					symbol VARCHAR(50) PRIMARY KEY,
					exchange VARCHAR(50) NOT NULL,
					config JSONB NOT NULL,
					status VARCHAR(20) NOT NULL,
					target_notional NUMERIC(32, 16) DEFAULT 0,
					quality_score NUMERIC(32, 16) DEFAULT 0,
					sector VARCHAR(50) DEFAULT ''
				);`)
		return nil, err
	})
	return err
}
