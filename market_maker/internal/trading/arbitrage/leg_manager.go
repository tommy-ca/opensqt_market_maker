package arbitrage

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"

	"github.com/shopspring/decimal"
)

// Leg represents one side of an arbitrage position
type Leg struct {
	Exchange string
	Symbol   string
	Side     pb.OrderSide
	Size     decimal.Decimal
}

// LegManager tracks the state of multi-leg positions
type LegManager struct {
	exchanges map[string]core.IExchange
	logger    core.ILogger

	legs map[string]*Leg // key: exchange:symbol
	mu   sync.RWMutex
}

func NewLegManager(exchanges map[string]core.IExchange, logger core.ILogger) *LegManager {
	return &LegManager{
		exchanges: exchanges,
		logger:    logger.WithField("component", "leg_manager"),
		legs:      make(map[string]*Leg),
	}
}

// SyncState fetches current positions from exchanges
func (m *LegManager) SyncState(ctx context.Context, exchange, symbol string) error {
	ex, ok := m.exchanges[exchange]
	if !ok {
		return fmt.Errorf("exchange not found: %s", exchange)
	}

	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear old state for this exchange/symbol to ensure accurate sync
	delete(m.legs, fmt.Sprintf("%s:%s", exchange, symbol))

	for _, p := range positions {
		if p.Symbol != symbol && symbol != "" {
			continue
		}

		size := pbu.ToGoDecimal(p.Size)
		if size.IsZero() {
			continue
		}

		side := pb.OrderSide_ORDER_SIDE_BUY
		if size.IsNegative() {
			side = pb.OrderSide_ORDER_SIDE_SELL
			size = size.Abs()
		}

		m.legs[fmt.Sprintf("%s:%s", exchange, p.Symbol)] = &Leg{
			Exchange: exchange,
			Symbol:   p.Symbol,
			Side:     side,
			Size:     size,
		}
	}

	return nil
}

// UpdateFromOrder incrementally updates the state from an order update
func (m *LegManager) UpdateFromOrder(update *pb.OrderUpdate) {
	// For now, we prefer SyncState for accuracy because order updates
	// don't always provide the full resulting position size.
	// But we could use this to trigger a background sync.
}

// IsDeltaNeutral checks if the sum of sizes (signed) is near zero
func (m *LegManager) IsDeltaNeutral(symbol string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalDelta := decimal.Zero
	count := 0
	for _, leg := range m.legs {
		if leg.Symbol == symbol {
			delta := leg.Size
			if leg.Side == pb.OrderSide_ORDER_SIDE_SELL {
				delta = delta.Neg()
			}
			totalDelta = totalDelta.Add(delta)
			count++
		}
	}

	return count >= 2 && totalDelta.Abs().LessThan(decimal.NewFromFloat(0.0001))
}

// GetSide returns the side of the position for the exchange/symbol
func (m *LegManager) GetSide(exchange, symbol string) pb.OrderSide {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if leg, ok := m.legs[fmt.Sprintf("%s:%s", exchange, symbol)]; ok {
		return leg.Side
	}
	return pb.OrderSide_ORDER_SIDE_UNSPECIFIED
}

// GetSize returns the size of the position for the exchange/symbol
func (m *LegManager) GetSize(exchange, symbol string) decimal.Decimal {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if leg, ok := m.legs[fmt.Sprintf("%s:%s", exchange, symbol)]; ok {
		return leg.Size
	}
	return decimal.Zero
}

// HasOpenPosition returns true if any leg has a non-zero size for the symbol
func (m *LegManager) HasOpenPosition(symbol string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, leg := range m.legs {
		if leg.Symbol == symbol && !leg.Size.IsZero() {
			return true
		}
	}
	return false
}
