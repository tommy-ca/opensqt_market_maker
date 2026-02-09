package strategy

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/tradingutils"
	"sync"

	"github.com/shopspring/decimal"
)

// GridStrategy implements a slot-based grid trading strategy
type GridStrategy struct {
	symbol         string
	exchangeName   string
	priceInterval  decimal.Decimal
	orderQuantity  decimal.Decimal
	minOrderValue  decimal.Decimal
	buyWindowSize  int
	sellWindowSize int
	priceDecimals  int
	qtyDecimals    int
	isNeutral      bool // If true, quotes both sides around current grid price
	riskMonitor    core.IRiskMonitor
	circuitBreaker core.ICircuitBreaker
	logger         core.ILogger

	// Dynamic Strategy Config
	dynamicInterval     bool
	volatilityScale     float64
	inventorySkewFactor float64
	mu                  sync.RWMutex
}

// NewGridStrategy creates a new grid strategy instance
func NewGridStrategy(
	symbol string,
	exchangeName string,
	priceInterval decimal.Decimal,
	orderQuantity decimal.Decimal,
	minOrderValue decimal.Decimal,
	buyWindowSize int,
	sellWindowSize int,
	priceDecimals int,
	qtyDecimals int,
	isNeutral bool,
	riskMonitor core.IRiskMonitor,
	circuitBreaker core.ICircuitBreaker,
	logger core.ILogger,
) *GridStrategy {
	return &GridStrategy{
		symbol:         symbol,
		exchangeName:   exchangeName,
		priceInterval:  priceInterval,
		orderQuantity:  orderQuantity,
		minOrderValue:  minOrderValue,
		buyWindowSize:  buyWindowSize,
		sellWindowSize: sellWindowSize,
		priceDecimals:  priceDecimals,
		qtyDecimals:    qtyDecimals,
		isNeutral:      isNeutral,
		riskMonitor:    riskMonitor,
		circuitBreaker: circuitBreaker,
		logger:         logger.WithField("component", "grid_strategy").WithField("symbol", symbol),
	}
}

// SetDynamicInterval configures the dynamic interval scaling
func (s *GridStrategy) SetDynamicInterval(enabled bool, scale float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dynamicInterval = enabled
	s.volatilityScale = scale
}

// SetTrendFollowing configures the trend following inventory skew
func (s *GridStrategy) SetTrendFollowing(skewFactor float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inventorySkewFactor = skewFactor
}

// CalculateActions calculates the required order actions based on current price and slots
func (s *GridStrategy) CalculateActions(
	ctx context.Context,
	slots map[string]*core.InventorySlot,
	anchorPrice decimal.Decimal,
	currentPrice decimal.Decimal,
) ([]*pb.OrderAction, error) {
	// 0. Check circuit breaker
	if s.circuitBreaker != nil && s.circuitBreaker.IsTripped() {
		s.logger.Warn("Circuit breaker is TRIPPED - skipping order calculation")
		return nil, nil
	}

	// 1. Calculate dynamic grid window
	s.mu.RLock()
	dynamicEnabled := s.dynamicInterval
	volScale := s.volatilityScale
	skewFactor := s.inventorySkewFactor
	s.mu.RUnlock()

	effectiveInterval := s.priceInterval
	if dynamicEnabled && s.riskMonitor != nil {
		atr := s.riskMonitor.GetATR(s.symbol)
		if !atr.IsZero() && volScale > 0 {
			// Interval = Max(BaseInterval, ATR * Scale)
			dynamicInterval := atr.Mul(decimal.NewFromFloat(volScale))
			if dynamicInterval.GreaterThan(effectiveInterval) {
				effectiveInterval = dynamicInterval
			}
		}
	}

	// Apply Inventory Skew to Current Price
	// TargetInventory = 0 (Neutral assumption for relative skew)
	// If current inventory > 0, we want to shift price DOWN to offload.
	// CalculateSkewedPrice: Price * (1 - (Inv - Target) * Factor)
	skewedCurrentPrice := currentPrice
	if skewFactor > 0 {
		currentInventory := s.calculateCurrentInventory(slots)
		skewedCurrentPrice = tradingutils.CalculateSkewedPrice(currentPrice, currentInventory, decimal.Zero, decimal.NewFromFloat(skewFactor))
		s.logger.Info("Applied trend following skew",
			"original_price", currentPrice,
			"skewed_price", skewedCurrentPrice,
			"inventory", currentInventory,
			"factor", skewFactor)
	}

	currentGridPrice := tradingutils.FindNearestGridPrice(skewedCurrentPrice, anchorPrice, effectiveInterval)

	var buyPrices []decimal.Decimal
	var sellPrices []decimal.Decimal

	if s.isNeutral {
		// Neutral: Buy below grid, Sell above grid
		buyPrices = tradingutils.CalculatePriceLevels(currentGridPrice, effectiveInterval.Neg(), s.buyWindowSize)
		sellPrices = tradingutils.CalculatePriceLevels(currentGridPrice, effectiveInterval, s.sellWindowSize)
	} else {
		// Directional (Long): Buy below grid
		buyPrices = tradingutils.CalculatePriceLevels(currentGridPrice, effectiveInterval.Neg(), s.buyWindowSize)
	}

	activeBuySlots := make(map[string]bool)
	for _, p := range buyPrices {
		activeBuySlots[tradingutils.RoundPrice(p, s.priceDecimals).String()] = true
	}

	activeSellSlots := make(map[string]bool)
	for _, p := range sellPrices {
		activeSellSlots[tradingutils.RoundPrice(p, s.priceDecimals).String()] = true
	}

	var actions []*pb.OrderAction

	// Iterate all existing slots
	for _, slot := range slots {
		slot.Mu.RLock()
		slotPriceStr := pbu.ToGoDecimal(slot.Price).String()
		isBuyCandidate := activeBuySlots[slotPriceStr]
		isSellCandidate := activeSellSlots[slotPriceStr]

		// Use skewedCurrentPrice for order logic to ensure existing orders align with the skew
		action := s.calculateSlotAdjustment(ctx, slot, skewedCurrentPrice, isBuyCandidate, isSellCandidate, effectiveInterval)
		if action != nil {
			actions = append(actions, action)
		}
		slot.Mu.RUnlock()
	}

	// Also check if any BUY slots are missing from the map
	for _, p := range buyPrices {
		rounded := tradingutils.RoundPrice(p, s.priceDecimals)
		if _, exists := slots[rounded.String()]; !exists {
			// Use skewedCurrentPrice for safety check too
			safetyBuffer := effectiveInterval.Mul(decimal.NewFromFloat(0.1))
			if rounded.LessThan(skewedCurrentPrice.Sub(safetyBuffer)) {
				action := s.requestBuyOrderForPrice(rounded)
				if action != nil {
					actions = append(actions, action)
				}
			}
		}
	}

	// Also check if any SELL slots are missing (only for Neutral)
	if s.isNeutral {
		for _, p := range sellPrices {
			rounded := tradingutils.RoundPrice(p, s.priceDecimals)
			if _, exists := slots[rounded.String()]; !exists {
				safetyBuffer := effectiveInterval.Mul(decimal.NewFromFloat(0.1))
				if rounded.GreaterThan(skewedCurrentPrice.Add(safetyBuffer)) {
					action := s.requestSellOrderForPrice(rounded)
					if action != nil {
						actions = append(actions, action)
					}
				}
			}
		}
	}

	return actions, nil
}

func (s *GridStrategy) calculateSlotAdjustment(
	ctx context.Context,
	slot *core.InventorySlot,
	currentPrice decimal.Decimal,
	isBuyCandidate bool,
	isSellCandidate bool,
	interval decimal.Decimal,
) *pb.OrderAction {
	slotPrice := pbu.ToGoDecimal(slot.Price)
	orderPrice := pbu.ToGoDecimal(slot.OrderPrice)

	switch slot.SlotStatus {
	case pb.SlotStatus_SLOT_STATUS_FREE:
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_EMPTY {
			// Try to Buy
			safetyBuffer := interval.Mul(decimal.NewFromFloat(0.1))
			if isBuyCandidate && slotPrice.LessThan(currentPrice.Sub(safetyBuffer)) {
				return s.requestBuyOrder(slot)
			}
			// Try to Sell (Neutral mode)
			if isSellCandidate && slotPrice.GreaterThan(currentPrice.Add(safetyBuffer)) {
				return s.requestSellOrderOpening(slot)
			}
		} else if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			// Directional Long:
			if !s.isNeutral {
				sellWindowMax := currentPrice.Add(decimal.NewFromInt(int64(s.sellWindowSize)).Mul(interval))
				if slotPrice.LessThanOrEqual(sellWindowMax) {
					return s.requestSellOrderClosing(slot, interval)
				}
			} else {
				// Neutral: close based on position side
				if slot.PositionQty.Value != "" && !pbu.ToGoDecimal(slot.PositionQty).IsZero() {
					return s.requestClosingOrder(slot, interval)
				}
			}
		}

	case pb.SlotStatus_SLOT_STATUS_LOCKED:
		// Trailing/Cleanup
		if slot.OrderSide == pb.OrderSide_ORDER_SIDE_BUY {
			minPrice := currentPrice.Sub(interval.Mul(decimal.NewFromInt(int64(s.buyWindowSize))))
			if orderPrice.LessThan(minPrice) || orderPrice.GreaterThan(currentPrice) {
				return &pb.OrderAction{Type: pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL, OrderId: slot.OrderId, Symbol: s.symbol, Price: slot.Price}
			}
		} else {
			// Sell Order
			maxPrice := currentPrice.Add(interval.Mul(decimal.NewFromInt(int64(s.sellWindowSize))))
			// Don't cancel if orderPrice < currentPrice (profitable behind market)
			// Only cancel if it's too far ABOVE.
			if orderPrice.GreaterThan(maxPrice) {
				return &pb.OrderAction{Type: pb.OrderActionType_ORDER_ACTION_TYPE_CANCEL, OrderId: slot.OrderId, Symbol: s.symbol, Price: slot.Price}
			}
		}
	}
	return nil
}

func (s *GridStrategy) requestBuyOrder(slot *core.InventorySlot) *pb.OrderAction {
	qty := tradingutils.RoundQuantity(s.getDynamicQuantity(), s.qtyDecimals)
	slotPrice := pbu.ToGoDecimal(slot.Price)
	if slotPrice.Mul(qty).LessThan(s.minOrderValue) {
		return nil
	}
	clientOID := s.generateClientOrderID(slotPrice, pb.OrderSide_ORDER_SIDE_BUY)

	return &pb.OrderAction{
		Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
		Price: slot.Price,
		Request: &pb.PlaceOrderRequest{
			Symbol: s.symbol, Side: pb.OrderSide_ORDER_SIDE_BUY, Type: pb.OrderType_ORDER_TYPE_LIMIT,
			TimeInForce: pb.TimeInForce_TIME_IN_FORCE_GTC, Quantity: pbu.FromGoDecimal(qty), Price: slot.Price,
			PostOnly: true, ClientOrderId: clientOID,
		},
	}
}

func (s *GridStrategy) requestBuyOrderForPrice(price decimal.Decimal) *pb.OrderAction {
	qty := tradingutils.RoundQuantity(s.getDynamicQuantity(), s.qtyDecimals)
	if price.Mul(qty).LessThan(s.minOrderValue) {
		return nil
	}
	clientOID := s.generateClientOrderID(price, pb.OrderSide_ORDER_SIDE_BUY)

	return &pb.OrderAction{
		Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
		Price: pbu.FromGoDecimal(price),
		Request: &pb.PlaceOrderRequest{
			Symbol: s.symbol, Side: pb.OrderSide_ORDER_SIDE_BUY, Type: pb.OrderType_ORDER_TYPE_LIMIT,
			TimeInForce: pb.TimeInForce_TIME_IN_FORCE_GTC, Quantity: pbu.FromGoDecimal(qty), Price: pbu.FromGoDecimal(price),
			PostOnly: true, ClientOrderId: clientOID,
		},
	}
}

func (s *GridStrategy) requestSellOrderOpening(slot *core.InventorySlot) *pb.OrderAction {
	qty := tradingutils.RoundQuantity(s.getDynamicQuantity(), s.qtyDecimals)
	slotPrice := pbu.ToGoDecimal(slot.Price)
	if slotPrice.Mul(qty).LessThan(s.minOrderValue) {
		return nil
	}
	clientOID := s.generateClientOrderID(slotPrice, pb.OrderSide_ORDER_SIDE_SELL)

	return &pb.OrderAction{
		Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
		Price: slot.Price,
		Request: &pb.PlaceOrderRequest{
			Symbol: s.symbol, Side: pb.OrderSide_ORDER_SIDE_SELL, Type: pb.OrderType_ORDER_TYPE_LIMIT,
			TimeInForce: pb.TimeInForce_TIME_IN_FORCE_GTC, Quantity: pbu.FromGoDecimal(qty), Price: slot.Price,
			PostOnly: true, ClientOrderId: clientOID,
		},
	}
}

func (s *GridStrategy) requestSellOrderForPrice(price decimal.Decimal) *pb.OrderAction {
	qty := tradingutils.RoundQuantity(s.getDynamicQuantity(), s.qtyDecimals)
	if price.Mul(qty).LessThan(s.minOrderValue) {
		return nil
	}
	clientOID := s.generateClientOrderID(price, pb.OrderSide_ORDER_SIDE_SELL)

	return &pb.OrderAction{
		Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
		Price: pbu.FromGoDecimal(price),
		Request: &pb.PlaceOrderRequest{
			Symbol: s.symbol, Side: pb.OrderSide_ORDER_SIDE_SELL, Type: pb.OrderType_ORDER_TYPE_LIMIT,
			TimeInForce: pb.TimeInForce_TIME_IN_FORCE_GTC, Quantity: pbu.FromGoDecimal(qty), Price: pbu.FromGoDecimal(price),
			PostOnly: true, ClientOrderId: clientOID,
		},
	}
}

func (s *GridStrategy) requestSellOrderClosing(slot *core.InventorySlot, interval decimal.Decimal) *pb.OrderAction {
	slotPrice := pbu.ToGoDecimal(slot.Price)
	clientOID := s.generateClientOrderID(slotPrice, pb.OrderSide_ORDER_SIDE_SELL)

	return &pb.OrderAction{
		Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
		Price: slot.Price,
		Request: &pb.PlaceOrderRequest{
			Symbol: s.symbol, Side: pb.OrderSide_ORDER_SIDE_SELL, Type: pb.OrderType_ORDER_TYPE_LIMIT,
			TimeInForce: pb.TimeInForce_TIME_IN_FORCE_GTC, Quantity: slot.PositionQty,
			Price:      pbu.FromGoDecimal(slotPrice.Add(interval)),
			ReduceOnly: true, ClientOrderId: clientOID,
		},
	}
}

func (s *GridStrategy) requestClosingOrder(slot *core.InventorySlot, interval decimal.Decimal) *pb.OrderAction {
	slotPrice := pbu.ToGoDecimal(slot.Price)
	posQty := pbu.ToGoDecimal(slot.PositionQty)

	var side pb.OrderSide
	var targetPrice decimal.Decimal

	if posQty.GreaterThan(decimal.Zero) {
		side = pb.OrderSide_ORDER_SIDE_SELL
		targetPrice = slotPrice.Add(interval)
	} else {
		side = pb.OrderSide_ORDER_SIDE_BUY
		targetPrice = slotPrice.Sub(interval)
	}

	clientOID := s.generateClientOrderID(slotPrice, side)
	return &pb.OrderAction{
		Type:  pb.OrderActionType_ORDER_ACTION_TYPE_PLACE,
		Price: slot.Price,
		Request: &pb.PlaceOrderRequest{
			Symbol: s.symbol, Side: side, Type: pb.OrderType_ORDER_TYPE_LIMIT,
			TimeInForce: pb.TimeInForce_TIME_IN_FORCE_GTC, Quantity: pbu.FromGoDecimal(posQty.Abs()),
			Price:      pbu.FromGoDecimal(targetPrice),
			ReduceOnly: true, ClientOrderId: clientOID,
		},
	}
}

func (s *GridStrategy) getDynamicQuantity() decimal.Decimal {
	if s.riskMonitor == nil {
		return s.orderQuantity
	}
	vol := s.riskMonitor.GetVolatilityFactor(s.symbol)
	if vol <= 0 {
		return s.orderQuantity
	}
	multiplier := 1.0 - (50.0 * vol)
	if multiplier < 0.1 {
		multiplier = 0.1
	}
	return s.orderQuantity.Mul(decimal.NewFromFloat(multiplier))
}

func (s *GridStrategy) calculateCurrentInventory(slots map[string]*core.InventorySlot) decimal.Decimal {
	total := decimal.Zero
	for _, slot := range slots {
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			total = total.Add(pbu.ToGoDecimal(slot.PositionQty))
		}
	}
	return total
}

func (s *GridStrategy) generateClientOrderID(price decimal.Decimal, side pb.OrderSide) string {
	oid := pbu.GenerateCompactOrderID(price, side.String(), s.priceDecimals)
	return pbu.AddBrokerPrefix(s.exchangeName, oid)
}
