package portfolio

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/trading/arbitrage"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

// PortfolioController orchestrates the portfolio rebalancing cycle
type PortfolioController struct {
	manager   IEngineManager
	allocator *PortfolioAllocator
	marginSim *MarginSim
	orch      IOrchestrator
	logger    core.ILogger
	interval  time.Duration

	mu            sync.RWMutex
	activeEngines map[string]PortfolioEngine // symbol -> engine
	lastTargets   []TargetPosition
	lastOpps      []arbitrage.Opportunity

	sem chan struct{}

	stopChan chan struct{}
}

func NewPortfolioController(
	manager IEngineManager,
	allocator *PortfolioAllocator,
	marginSim *MarginSim,
	orch IOrchestrator,
	logger core.ILogger,
	interval time.Duration,
) *PortfolioController {

	return &PortfolioController{
		manager:       manager,
		allocator:     allocator,
		marginSim:     marginSim,
		orch:          orch,
		logger:        logger.WithField("component", "portfolio_controller"),
		interval:      interval,
		activeEngines: make(map[string]PortfolioEngine),
		sem:           make(chan struct{}, 5), // Limit to 5 concurrent actions
		stopChan:      make(chan struct{}),
	}
}

func (c *PortfolioController) Start(ctx context.Context) error {
	c.logger.Info("Starting Portfolio Controller", "interval", c.interval)

	// Initial Rebalance
	if err := c.Rebalance(ctx); err != nil {
		c.logger.Error("Initial rebalance failed", "error", err)
	}

	go c.runLoop()
	return nil
}

func (c *PortfolioController) Stop() {
	close(c.stopChan)
}

func (c *PortfolioController) runLoop() {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), c.interval)
			if err := c.Rebalance(ctx); err != nil {
				c.logger.Error("Rebalance failed", "error", err)
			}
			cancel()
		}
	}
}

func (c *PortfolioController) Rebalance(ctx context.Context) error {
	// 1. Scanner: Identify Opportunities
	opps, err := c.manager.Scan(ctx)
	if err != nil {
		return err
	}

	// 2. Allocator: Compute Target Weights
	profile := c.marginSim.GetRiskProfile()

	// For now, assume fixed 3x leverage for the portfolio
	leverage := decimal.NewFromInt(3)
	targets := c.allocator.Allocate(opps, profile.AdjustedEquity, leverage)

	// 3. Reconciler: Match Actual with Target
	c.mu.Lock()

	c.lastOpps = opps
	c.lastTargets = targets

	targetMap := make(map[string]TargetPosition)
	for _, t := range targets {
		targetMap[t.Symbol] = t
	}

	var actions []RebalanceAction

	// a. Check for exits or reductions (Priority 1 & 2)
	for sym, eng := range c.activeEngines {
		if target, ok := targetMap[sym]; ok {
			currentQty := eng.GetOrderQuantity()
			if c.allocator.ShouldRebalance(currentQty, target.Notional, decimal.Zero) {
				diff := target.Notional.Sub(currentQty)
				priority := 3
				if diff.IsNegative() {
					priority = 2 // De-risk (reduction)
				}
				actions = append(actions, RebalanceAction{Symbol: sym, Diff: diff, Priority: priority})
			}
		} else {
			// Complete removal
			actions = append(actions, RebalanceAction{Symbol: sym, Diff: eng.GetOrderQuantity().Neg(), Priority: 1})
		}
	}

	// b. Check for additions (Priority 4)
	for _, t := range targets {
		if _, ok := c.activeEngines[t.Symbol]; !ok {
			actions = append(actions, RebalanceAction{Symbol: t.Symbol, Diff: t.Notional, Priority: 4})
		}
	}

	// Sort actions: Remove/Reduce first to free up margin
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Priority < actions[j].Priority
	})
	c.mu.Unlock()

	// 4. Execution: Apply Actions in parallel batches by priority
	// Group 1: Priority 1 & 2 (Reduction/Removal) - free up margin first
	var g errgroup.Group
	for _, a := range actions {
		if a.Priority <= 2 {
			a := a
			g.Go(func() error {
				select {
				case c.sem <- struct{}{}:
					defer func() { <-c.sem }()
				case <-ctx.Done():
					return ctx.Err()
				}
				return c.executeAction(ctx, a, targetMap[a.Symbol])
			})
		}
	}
	if err := g.Wait(); err != nil {
		c.logger.Error("Some Priority 1/2 rebalance actions failed", "error", err)
	}

	// Group 2: Priority 3 & 4 (Expansion/Addition)
	var g2 errgroup.Group
	for _, a := range actions {
		if a.Priority > 2 {
			a := a
			g2.Go(func() error {
				select {
				case c.sem <- struct{}{}:
					defer func() { <-c.sem }()
				case <-ctx.Done():
					return ctx.Err()
				}
				return c.executeAction(ctx, a, targetMap[a.Symbol])
			})
		}
	}
	return g2.Wait()
}

func (c *PortfolioController) GetLastTargets() []TargetPosition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastTargets
}

func (c *PortfolioController) GetLastOpps() []arbitrage.Opportunity {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastOpps
}

func (c *PortfolioController) executeAction(ctx context.Context, action RebalanceAction, target TargetPosition) error {
	c.logger.Info("Rebalance action", "symbol", action.Symbol, "priority", action.Priority, "diff", action.Diff.String())

	switch action.Priority {
	case 1: // Removal
		if err := c.orch.RemoveTradingPair(ctx, action.Symbol); err != nil {
			c.logger.Error("Failed to remove trading pair from persistence", "symbol", action.Symbol, "error", err)
			return err
		}
		c.orch.RemoveSymbol(action.Symbol)

		c.mu.Lock()
		delete(c.activeEngines, action.Symbol)
		c.mu.Unlock()
	case 2, 3: // Resize
		c.mu.RLock()
		eng, ok := c.activeEngines[action.Symbol]
		c.mu.RUnlock()

		if ok {
			eng.SetOrderQuantity(target.Notional)

			// Get config for persistence
			configJSON, err := c.manager.CreateConfig(action.Symbol, target.Notional)
			if err != nil {
				c.logger.Error("Failed to create config for resize intent", "symbol", action.Symbol, "error", err)
				return err
			}

			if err := c.orch.AddTradingPair(ctx, action.Symbol, target.Exchange, configJSON, target.Notional, target.QualityScore, ""); err != nil {
				c.logger.Error("Failed to persist resize intent", "symbol", action.Symbol, "error", err)
				return err
			}
		}
	case 4: // Addition
		configJSON, err := c.manager.CreateConfig(action.Symbol, target.Notional)
		if err != nil {
			c.logger.Error("Failed to create config for addition intent", "symbol", action.Symbol, "error", err)
			return err
		}

		// Persist intent before creating engine
		if err := c.orch.AddTradingPair(ctx, action.Symbol, target.Exchange, configJSON, target.Notional, target.QualityScore, ""); err != nil {
			c.logger.Error("Failed to persist addition intent", "symbol", action.Symbol, "error", err)
			return err
		}

		eng, err := c.manager.CreateEngine(action.Symbol, configJSON)
		if err != nil {
			c.logger.Error("Failed to create engine", "symbol", action.Symbol, "error", err)
			return err
		}

		if pEng, ok := eng.(PortfolioEngine); ok {
			c.orch.AddSymbol(action.Symbol, pEng)
			if err := c.orch.StartSymbol(action.Symbol); err != nil {
				c.logger.Error("Failed to start symbol", "symbol", action.Symbol, "error", err)
				return err
			}

			c.mu.Lock()
			c.activeEngines[action.Symbol] = pEng
			c.mu.Unlock()
		} else {
			err := fmt.Errorf("engine for %s does not implement PortfolioEngine", action.Symbol)
			c.logger.Error(err.Error())
			return err
		}
	}
	return nil
}
