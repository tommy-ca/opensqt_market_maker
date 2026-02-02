package arbitrage

import (
	"context"
	"market_maker/internal/core"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"market_maker/pkg/telemetry"
)

// UniverseManager manages periodic scanning and selection of arbitrage targets
type UniverseManager struct {
	selector *UniverseSelector
	logger   core.ILogger
	interval time.Duration

	mu           sync.RWMutex
	currentOpps  []Opportunity
	activeTarget string
	activeScore  decimal.Decimal

	// Switching Params
	switchingFee     decimal.Decimal // Roundtrip fee (e.g. 0.003 for 0.3%)
	expectedHoldDays decimal.Decimal // How long we expect to hold (e.g. 7 days)
	switchBuffer     decimal.Decimal // Multiple to cover cost (e.g. 1.5)

	stopChan chan struct{}
}

func NewUniverseManager(selector *UniverseSelector, logger core.ILogger, interval time.Duration) *UniverseManager {
	return &UniverseManager{
		selector:         selector,
		logger:           logger.WithField("component", "universe_manager"),
		interval:         interval,
		switchingFee:     decimal.NewFromFloat(0.003), // 0.3% roundtrip
		expectedHoldDays: decimal.NewFromInt(7),       // 1 week
		switchBuffer:     decimal.NewFromFloat(1.5),
		stopChan:         make(chan struct{}),
	}
}

// SetSwitchingParams updates the parameters used for evaluation
func (m *UniverseManager) SetSwitchingParams(fee, holdDays, buffer decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.switchingFee = fee
	m.expectedHoldDays = holdDays
	m.switchBuffer = buffer
}

func (m *UniverseManager) Start(ctx context.Context) error {
	m.logger.Info("Starting Universe Manager", "interval", m.interval)

	// Initial scan
	if err := m.Refresh(ctx); err != nil {
		m.logger.Error("Initial refresh failed", "error", err)
	}

	go m.runLoop()
	return nil
}

func (m *UniverseManager) Stop() error {
	close(m.stopChan)
	return nil
}

func (m *UniverseManager) GetTopOpportunities() []Opportunity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentOpps
}

func (m *UniverseManager) GetActiveTarget() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeTarget
}

// Refresh performs a single scan and updates the internal state
func (m *UniverseManager) Refresh(ctx context.Context) error {
	opps, err := m.selector.Scan(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentOpps = opps

	// Export metrics for all top opportunities
	metrics := telemetry.GetGlobalMetrics()
	for _, o := range opps {
		scoreF, _ := o.QualityScore.Float64()
		metrics.SetQualityScore(o.Symbol, scoreF)
	}

	if len(opps) > 0 {
		best := opps[0]
		if m.activeTarget == "" {
			m.activeTarget = best.Symbol
			m.activeScore = best.QualityScore
			m.logger.Info("Initial active target selected", "symbol", m.activeTarget, "score", m.activeScore)
		} else if best.Symbol != m.activeTarget {
			// Find current target in the list to get its latest score
			var currentOpp *Opportunity
			for _, o := range opps {
				if o.Symbol == m.activeTarget {
					currentOpp = &o
					break
				}
			}

			if currentOpp != nil {
				if m.shouldSwitch(best, *currentOpp) {
					m.logger.Info("Switching target",
						"from", m.activeTarget, "old_score", currentOpp.QualityScore,
						"to", best.Symbol, "new_score", best.QualityScore)
					m.activeTarget = best.Symbol
					m.activeScore = best.QualityScore
				}
			} else {
				// Current target dropped out of top list or liquid universe
				// Force switch to best available
				m.logger.Warn("Active target no longer in liquid universe, switching",
					"from", m.activeTarget, "to", best.Symbol)
				m.activeTarget = best.Symbol
				m.activeScore = best.QualityScore
			}
		} else {
			// Update current score
			m.activeScore = best.QualityScore
		}
	}

	return nil
}

func (m *UniverseManager) runLoop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), m.interval)
			if err := m.Refresh(ctx); err != nil {
				m.logger.Error("Refresh failed", "error", err)
			}
			cancel()
		}
	}
}

func (m *UniverseManager) shouldSwitch(newOpp, oldOpp Opportunity) bool {
	// Calculate Estimated Profit Gain over holding period
	// Gain = (New_APR - Old_APR) * HoldDays / 365
	aprNew := newOpp.Metrics.AverageAnnualAPR
	aprOld := oldOpp.Metrics.AverageAnnualAPR

	if aprNew.LessThanOrEqual(aprOld) {
		return false
	}

	// High precision path for switch logic
	days := decimal.NewFromInt(m.expectedHoldDays.IntPart())
	gain := aprNew.Sub(aprOld).Mul(days).Div(decimal.NewFromInt(365))

	// Requirement: Gain > Cost * Buffer
	threshold := m.switchingFee.Mul(m.switchBuffer)

	m.logger.Debug("Evaluating switch",
		"new", newOpp.Symbol, "old", oldOpp.Symbol,
		"gain", gain.String(), "cost_threshold", threshold.String())

	return gain.GreaterThan(threshold)
}
