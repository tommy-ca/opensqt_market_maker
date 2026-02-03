package grid

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/tradingutils"

	"github.com/shopspring/decimal"
)

// GridLevel represents the data required by the strategy logic for a single grid level
type GridLevel struct {
	Price          decimal.Decimal
	PositionStatus pb.PositionStatus
	PositionQty    decimal.Decimal
	SlotStatus     pb.SlotStatus
	OrderSide      pb.OrderSide
	OrderPrice     decimal.Decimal
	OrderID        int64
}

// StrategyConfig holds the parameters for the grid strategy
type StrategyConfig struct {
	Symbol              string
	Exchange            string
	PriceInterval       decimal.Decimal
	OrderQuantity       decimal.Decimal
	MinOrderValue       decimal.Decimal
	BuyWindowSize       int
	SellWindowSize      int
	PriceDecimals       int
	QtyDecimals         int
	IsNeutral           bool
	VolatilityScale     float64
	InventorySkewFactor float64
}

// GridStrategy implements the pure logic for a trailing grid strategy
type GridStrategy struct {
	cfg StrategyConfig
}

func NewGridStrategy(cfg StrategyConfig) *GridStrategy {
	return &GridStrategy{cfg: cfg}
}

// CalculateTargetState computes the desired positions and orders based on current market state
func (s *GridStrategy) CalculateTargetState(
	ctx context.Context,
	currentPrice decimal.Decimal,
	anchorPrice decimal.Decimal,
	atr decimal.Decimal,
	volatilityFactor float64,
	isRiskTriggered bool,
	isCircuitTripped bool,
	state any,
) (*core.TargetState, error) {
	levels, ok := state.([]GridLevel)
	if !ok {
		return nil, fmt.Errorf("invalid state type for GridStrategy: expected []GridLevel")
	}

	target := &core.TargetState{
		Positions: []core.TargetPosition{},
		Orders:    []core.TargetOrder{},
	}

	// 0. Check circuit breaker - if tripped, we want NO active orders
	if isCircuitTripped {
		return target, nil
	}

	// 1. Calculate effective parameters
	interval := s.calculateEffectiveInterval(atr)
	inventory := s.calculateInventory(levels)
	skewedPrice := s.calculateSkewedPrice(currentPrice, inventory)

	gridCenter := tradingutils.FindNearestGridPrice(skewedPrice, anchorPrice, interval)

	buyPrices := s.calculateBuyPrices(gridCenter, interval)
	sellPrices := s.calculateSellPrices(gridCenter, interval)

	activeBuyMap := s.toMap(buyPrices)
	activeSellMap := s.toMap(sellPrices)

	// 2. Identify missing active levels and determine desired orders
	processedPrices := make(map[string]bool)

	// Existing positions must be preserved in TargetState
	for _, level := range levels {
		priceKey := tradingutils.RoundPrice(level.Price, s.cfg.PriceDecimals).String()
		processedPrices[priceKey] = true

		if level.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			target.Positions = append(target.Positions, core.TargetPosition{
				Exchange: s.cfg.Exchange,
				Symbol:   s.cfg.Symbol,
				Size:     level.PositionQty,
			})

			// Closing orders for existing positions
			closingOrder := s.decideClosingOrder(level, currentPrice, interval, volatilityFactor)
			if closingOrder != nil {
				target.Orders = append(target.Orders, *closingOrder)
			}
		}

		// Opening orders for FREE levels
		if level.PositionStatus == pb.PositionStatus_POSITION_STATUS_EMPTY && !isRiskTriggered {
			openingOrder := s.decideOpeningOrder(level, skewedPrice, interval, volatilityFactor, activeBuyMap, activeSellMap)
			if openingOrder != nil {
				target.Orders = append(target.Orders, *openingOrder)
			}
		}
	}

	// 3. Add new opening orders for missing levels
	if !isRiskTriggered {
		// New Buys
		for _, price := range buyPrices {
			key := tradingutils.RoundPrice(price, s.cfg.PriceDecimals).String()
			if !processedPrices[key] {
				safetyBuffer := interval.Mul(decimal.NewFromFloat(0.1))
				if price.LessThan(skewedPrice.Sub(safetyBuffer)) {
					order := s.createTargetOrder(price, "BUY", false, volatilityFactor)
					if order != nil {
						target.Orders = append(target.Orders, *order)
					}
				}
			}
		}

		// New Sells (Neutral mode)
		for _, price := range sellPrices {
			key := tradingutils.RoundPrice(price, s.cfg.PriceDecimals).String()
			if !processedPrices[key] {
				safetyBuffer := interval.Mul(decimal.NewFromFloat(0.1))
				if price.GreaterThan(skewedPrice.Add(safetyBuffer)) {
					order := s.createTargetOrder(price, "SELL", false, volatilityFactor)
					if order != nil {
						target.Orders = append(target.Orders, *order)
					}
				}
			}
		}
	}

	return target, nil
}

func (s *GridStrategy) GetSymbol() string {
	return s.cfg.Symbol
}

func (s *GridStrategy) calculateEffectiveInterval(atr decimal.Decimal) decimal.Decimal {
	if atr.IsZero() || s.cfg.VolatilityScale <= 0 {
		return s.cfg.PriceInterval
	}
	dynamicInterval := atr.Mul(decimal.NewFromFloat(s.cfg.VolatilityScale))
	if dynamicInterval.GreaterThan(s.cfg.PriceInterval) {
		return dynamicInterval
	}
	return s.cfg.PriceInterval
}

func (s *GridStrategy) calculateSkewedPrice(currentPrice decimal.Decimal, inventory decimal.Decimal) decimal.Decimal {
	if s.cfg.InventorySkewFactor <= 0 {
		return currentPrice
	}
	return tradingutils.CalculateSkewedPrice(currentPrice, inventory, decimal.Zero, decimal.NewFromFloat(s.cfg.InventorySkewFactor))
}

func (s *GridStrategy) calculateInventory(levels []GridLevel) decimal.Decimal {
	total := decimal.Zero
	for _, level := range levels {
		if level.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			total = total.Add(level.PositionQty)
		}
	}
	return total
}

func (s *GridStrategy) calculateBuyPrices(center decimal.Decimal, interval decimal.Decimal) []decimal.Decimal {
	return tradingutils.CalculatePriceLevels(center, interval.Neg(), s.cfg.BuyWindowSize)
}

func (s *GridStrategy) calculateSellPrices(center decimal.Decimal, interval decimal.Decimal) []decimal.Decimal {
	if !s.cfg.IsNeutral {
		return nil
	}
	return tradingutils.CalculatePriceLevels(center, interval, s.cfg.SellWindowSize)
}

func (s *GridStrategy) toMap(prices []decimal.Decimal) map[string]bool {
	m := make(map[string]bool)
	for _, p := range prices {
		m[tradingutils.RoundPrice(p, s.cfg.PriceDecimals).String()] = true
	}
	return m
}

func (s *GridStrategy) decideOpeningOrder(
	level GridLevel,
	skewedPrice decimal.Decimal,
	interval decimal.Decimal,
	volatilityFactor float64,
	activeBuys map[string]bool,
	activeSells map[string]bool,
) *core.TargetOrder {
	priceStr := tradingutils.RoundPrice(level.Price, s.cfg.PriceDecimals).String()
	isBuyCandidate := activeBuys[priceStr]
	isSellCandidate := activeSells[priceStr]

	safetyBuffer := interval.Mul(decimal.NewFromFloat(0.1))

	if isBuyCandidate && level.Price.LessThan(skewedPrice.Sub(safetyBuffer)) {
		return s.createTargetOrder(level.Price, "BUY", false, volatilityFactor)
	}
	if isSellCandidate && level.Price.GreaterThan(skewedPrice.Add(safetyBuffer)) {
		return s.createTargetOrder(level.Price, "SELL", false, volatilityFactor)
	}

	return nil
}

func (s *GridStrategy) decideClosingOrder(
	level GridLevel,
	currentPrice decimal.Decimal,
	interval decimal.Decimal,
	volatilityFactor float64,
) *core.TargetOrder {
	if !s.cfg.IsNeutral {
		// Directional Long: close at level price + interval
		sellWindowMax := currentPrice.Add(decimal.NewFromInt(int64(s.cfg.SellWindowSize)).Mul(interval))
		if level.Price.LessThanOrEqual(sellWindowMax) {
			return s.createTargetOrder(level.Price.Add(interval), "SELL", true, volatilityFactor)
		}
	} else {
		// Neutral: close based on side
		if level.PositionQty.IsPositive() {
			return s.createTargetOrder(level.Price.Add(interval), "SELL", true, volatilityFactor)
		} else if level.PositionQty.IsNegative() {
			return s.createTargetOrder(level.Price.Sub(interval), "BUY", true, volatilityFactor)
		}
	}
	return nil
}

func (s *GridStrategy) createTargetOrder(price decimal.Decimal, side string, reduceOnly bool, volatilityFactor float64) *core.TargetOrder {
	qty := s.calculateDynamicQuantity(volatilityFactor)
	if price.Mul(qty).LessThan(s.cfg.MinOrderValue) {
		return nil
	}

	return &core.TargetOrder{
		Exchange:      s.cfg.Exchange,
		Symbol:        s.cfg.Symbol,
		Price:         tradingutils.RoundPrice(price, s.cfg.PriceDecimals),
		Quantity:      tradingutils.RoundQuantity(qty, s.cfg.QtyDecimals),
		Side:          side,
		Type:          "LIMIT",
		ReduceOnly:    reduceOnly,
		PostOnly:      !reduceOnly,
		ClientOrderID: s.generateClientOrderID(price, side),
	}
}

func (s *GridStrategy) generateClientOrderID(price decimal.Decimal, side string) string {
	return pbu.GenerateCompactOrderID(price, side, s.cfg.PriceDecimals)
}

func (s *GridStrategy) calculateDynamicQuantity(vol float64) decimal.Decimal {
	if vol <= 0 {
		return s.cfg.OrderQuantity
	}
	multiplier := 1.0 - (50.0 * vol)
	if multiplier < 0.1 {
		multiplier = 0.1
	}
	return s.cfg.OrderQuantity.Mul(decimal.NewFromFloat(multiplier))
}
