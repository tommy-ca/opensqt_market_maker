package grid

import (
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/tradingutils"

	"github.com/shopspring/decimal"
)

// Slot represents the data required by the strategy logic for a single grid level
type Slot struct {
	Price          decimal.Decimal
	PositionStatus pb.PositionStatus
	PositionQty    decimal.Decimal
	SlotStatus     pb.SlotStatus
	OrderSide      pb.OrderSide
	OrderPrice     decimal.Decimal
}

// StrategyConfig holds the parameters for the grid strategy
type StrategyConfig struct {
	Symbol              string
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

// Strategy implements the pure logic for a trailing grid strategy
type Strategy struct {
	cfg StrategyConfig
}

func NewStrategy(cfg StrategyConfig) *Strategy {
	return &Strategy{cfg: cfg}
}

// CalculateEffectiveInterval calculates the interval adjusted for volatility if needed
func (s *Strategy) CalculateEffectiveInterval(atr decimal.Decimal) decimal.Decimal {
	if atr.IsZero() || s.cfg.VolatilityScale <= 0 {
		return s.cfg.PriceInterval
	}
	dynamicInterval := atr.Mul(decimal.NewFromFloat(s.cfg.VolatilityScale))
	if dynamicInterval.GreaterThan(s.cfg.PriceInterval) {
		return dynamicInterval
	}
	return s.cfg.PriceInterval
}

// CalculateSkewedPrice calculates the reference price adjusted for inventory skew
func (s *Strategy) CalculateSkewedPrice(currentPrice decimal.Decimal, inventory decimal.Decimal) decimal.Decimal {
	if s.cfg.InventorySkewFactor <= 0 {
		return currentPrice
	}
	return tradingutils.CalculateSkewedPrice(currentPrice, inventory, decimal.Zero, decimal.NewFromFloat(s.cfg.InventorySkewFactor))
}

// CalculateActions decides which orders to place or cancel
func (s *Strategy) CalculateActions(
	currentPrice decimal.Decimal,
	anchorPrice decimal.Decimal,
	atr decimal.Decimal,
	volatilityFactor float64,
	isRiskTriggered bool,
	slots []Slot,
) []*pb.OrderAction {
	interval := s.CalculateEffectiveInterval(atr)
	inventory := s.calculateInventory(slots)
	skewedPrice := s.CalculateSkewedPrice(currentPrice, inventory)

	gridCenter := tradingutils.FindNearestGridPrice(skewedPrice, anchorPrice, interval)

	buyPrices := s.calculateBuyPrices(gridCenter, interval)
	sellPrices := s.calculateSellPrices(gridCenter, interval)

	activeBuyMap := s.toMap(buyPrices)
	activeSellMap := s.toMap(sellPrices)

	var actions []*pb.OrderAction

	// 1. Process existing slots
	processedPrices := make(map[string]bool)
	for _, slot := range slots {
		priceKey := tradingutils.RoundPrice(slot.Price, s.cfg.PriceDecimals).String()
		processedPrices[priceKey] = true

		action := s.decideActionForSlot(slot, skewedPrice, interval, volatilityFactor, isRiskTriggered, activeBuyMap, activeSellMap)
		if action != nil {
			actions = append(actions, action)
		}
	}

	// 2. Identify missing active slots (opening logic)
	// Iterate through calculated grid levels and check if they are missing from existing slots

	// Open Buys
	for _, price := range buyPrices {
		key := tradingutils.RoundPrice(price, s.cfg.PriceDecimals).String()
		if !processedPrices[key] && !isRiskTriggered {
			safetyBuffer := interval.Mul(decimal.NewFromFloat(0.1))
			if price.LessThan(skewedPrice.Sub(safetyBuffer)) {
				// Treat as FREE/EMPTY
				action := s.createPlaceAction(price, pb.OrderSide_ORDER_SIDE_BUY, false, volatilityFactor)
				if action != nil {
					actions = append(actions, action)
				}
			}
		}
	}

	// Open Sells
	for _, price := range sellPrices {
		key := tradingutils.RoundPrice(price, s.cfg.PriceDecimals).String()
		if !processedPrices[key] {
			safetyBuffer := interval.Mul(decimal.NewFromFloat(0.1))
			if price.GreaterThan(skewedPrice.Add(safetyBuffer)) {
				// Treat as FREE/EMPTY
				action := s.createPlaceAction(price, pb.OrderSide_ORDER_SIDE_SELL, false, volatilityFactor)
				if action != nil {
					actions = append(actions, action)
				}
			}
		}
	}

	return actions
}

func (s *Strategy) calculateInventory(slots []Slot) decimal.Decimal {
	total := decimal.Zero
	for _, slot := range slots {
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			total = total.Add(slot.PositionQty)
		}
	}
	return total
}

func (s *Strategy) calculateBuyPrices(center decimal.Decimal, interval decimal.Decimal) []decimal.Decimal {
	return tradingutils.CalculatePriceLevels(center, interval.Neg(), s.cfg.BuyWindowSize)
}

func (s *Strategy) calculateSellPrices(center decimal.Decimal, interval decimal.Decimal) []decimal.Decimal {
	if !s.cfg.IsNeutral {
		return nil
	}
	return tradingutils.CalculatePriceLevels(center, interval, s.cfg.SellWindowSize)
}

func (s *Strategy) toMap(prices []decimal.Decimal) map[string]bool {
	m := make(map[string]bool)
	for _, p := range prices {
		m[tradingutils.RoundPrice(p, s.cfg.PriceDecimals).String()] = true
	}
	return m
}

func (s *Strategy) decideActionForSlot(
	slot Slot,
	currentPrice decimal.Decimal,
	interval decimal.Decimal,
	volatilityFactor float64,
	isRiskTriggered bool,
	activeBuys map[string]bool,
	activeSells map[string]bool,
) *pb.OrderAction {
	priceStr := tradingutils.RoundPrice(slot.Price, s.cfg.PriceDecimals).String()
	isBuyCandidate := activeBuys[priceStr]
	isSellCandidate := activeSells[priceStr]

	safetyBuffer := interval.Mul(decimal.NewFromFloat(0.1))

	switch slot.SlotStatus {
	case pb.SlotStatus_SLOT_STATUS_FREE:
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_EMPTY {
			// Opening logic: Disable buys if risk triggered
			if !isRiskTriggered && isBuyCandidate && slot.Price.LessThan(currentPrice.Sub(safetyBuffer)) {
				return s.createPlaceAction(slot.Price, pb.OrderSide_ORDER_SIDE_BUY, false, volatilityFactor)
			}
			if isSellCandidate && slot.Price.GreaterThan(currentPrice.Add(safetyBuffer)) {
				return s.createPlaceAction(slot.Price, pb.OrderSide_ORDER_SIDE_SELL, false, volatilityFactor)
			}
		} else if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			// Closing logic
			if !s.cfg.IsNeutral {
				// Directional Long: close at price + interval
				sellWindowMax := currentPrice.Add(decimal.NewFromInt(int64(s.cfg.SellWindowSize)).Mul(interval))
				if slot.Price.LessThanOrEqual(sellWindowMax) {
					return s.createPlaceAction(slot.Price.Add(interval), pb.OrderSide_ORDER_SIDE_SELL, true, volatilityFactor)
				}
			} else {
				// Neutral: close based on side
				if slot.PositionQty.IsPositive() {
					return s.createPlaceAction(slot.Price.Add(interval), pb.OrderSide_ORDER_SIDE_SELL, true, volatilityFactor)
				} else if slot.PositionQty.IsNegative() {
					return s.createPlaceAction(slot.Price.Sub(interval), pb.OrderSide_ORDER_SIDE_BUY, true, volatilityFactor)
				}
			}
		}

	case pb.SlotStatus_SLOT_STATUS_LOCKED:
		// Trailing/Cleanup/Risk logic
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
			// Force cancel all buys if risk triggered
			if isRiskTriggered {
				return &pb.OrderAction{Type: pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL, Symbol: s.cfg.Symbol, Price: pbu.FromGoDecimal(slot.Price), OrderId: 0}
			}

			minPrice := currentPrice.Sub(interval.Mul(decimal.NewFromInt(int64(s.cfg.BuyWindowSize))))
			if slot.OrderPrice.LessThan(minPrice) || slot.OrderPrice.GreaterThan(currentPrice) {
				return &pb.OrderAction{Type: pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL, Symbol: s.cfg.Symbol, Price: pbu.FromGoDecimal(slot.Price), OrderId: 0}
			}
		} else {
			maxPrice := currentPrice.Add(interval.Mul(decimal.NewFromInt(int64(s.cfg.SellWindowSize))))
			if slot.OrderPrice.GreaterThan(maxPrice) || slot.OrderPrice.LessThan(currentPrice) {
				return &pb.OrderAction{Type: pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL, Symbol: s.cfg.Symbol, Price: pbu.FromGoDecimal(slot.Price), OrderId: 0}
			}
		}
	}

	return nil
}

func (s *Strategy) createPlaceAction(price decimal.Decimal, side pb.OrderSide, reduceOnly bool, volatilityFactor float64) *pb.OrderAction {
	qty := s.calculateDynamicQuantity(volatilityFactor)
	if price.Mul(qty).LessThan(s.cfg.MinOrderValue) {
		return nil
	}

	return &pb.OrderAction{
		Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
		Price: pbu.FromGoDecimal(price),
		Request: &pb.PlaceOrderRequest{
			Symbol:        s.cfg.Symbol,
			Side:          side,
			Type:          pb.OrderType_ORDER_TYPE_LIMIT,
			Quantity:      pbu.FromGoDecimal(tradingutils.RoundQuantity(qty, s.cfg.QtyDecimals)),
			Price:         pbu.FromGoDecimal(tradingutils.RoundPrice(price, s.cfg.PriceDecimals)),
			ReduceOnly:    reduceOnly,
			PostOnly:      !reduceOnly,
			ClientOrderId: s.generateClientOrderID(price, side),
		},
	}
}

func (s *Strategy) generateClientOrderID(price decimal.Decimal, side pb.OrderSide) string {
	return pbu.GenerateCompactOrderID(price, side.String(), s.cfg.PriceDecimals)
}

func (s *Strategy) calculateDynamicQuantity(vol float64) decimal.Decimal {
	if vol <= 0 {
		return s.cfg.OrderQuantity
	}
	multiplier := 1.0 - (50.0 * vol)
	if multiplier < 0.1 {
		multiplier = 0.1
	}
	return s.cfg.OrderQuantity.Mul(decimal.NewFromFloat(multiplier))
}
