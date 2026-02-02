package risk

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sort"
	"sync"
	"time"
)

// OrderCleaner implements the IOrderCleaner interface
type OrderCleaner struct {
	exchange      core.IExchange
	orderExecutor core.IOrderExecutor
	logger        core.ILogger
	symbol        string

	interval      time.Duration
	maxOpenOrders int
	maxOrderAge   time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewOrderCleaner creates a new order cleaner
func NewOrderCleaner(
	exchange core.IExchange,
	orderExecutor core.IOrderExecutor,
	logger core.ILogger,
	symbol string,
	interval time.Duration,
	maxOpenOrders int,
	maxOrderAge time.Duration,
) *OrderCleaner {
	ctx, cancel := context.WithCancel(context.Background())

	return &OrderCleaner{
		exchange:      exchange,
		orderExecutor: orderExecutor,
		logger:        logger.WithField("component", "order_cleaner"),
		symbol:        symbol,
		interval:      interval,
		maxOpenOrders: maxOpenOrders,
		maxOrderAge:   maxOrderAge,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start begins the cleaner loop
func (oc *OrderCleaner) Start(ctx context.Context) error {
	oc.logger.Info("Starting order cleaner", "interval", oc.interval)
	oc.wg.Add(1)
	go oc.runLoop()
	return nil
}

// Stop stops the cleaner
func (oc *OrderCleaner) Stop() error {
	oc.logger.Info("Stopping order cleaner")
	oc.cancel()
	oc.wg.Wait()
	return nil
}

// Cleanup performs a single cleanup pass
func (oc *OrderCleaner) Cleanup(ctx context.Context) error {
	oc.logger.Info("Starting cleanup pass")

	// Get open orders
	orders, err := oc.exchange.GetOpenOrders(ctx, oc.symbol, false)
	if err != nil {
		return fmt.Errorf("failed to get open orders: %w", err)
	}

	if len(orders) == 0 {
		return nil
	}

	var ordersToCancel []int64
	now := time.Now()

	// Separate by side
	var buyOrders []*pb.Order
	var sellOrders []*pb.Order

	for _, o := range orders {
		// 🔥 Legacy Parity: Skip PARTIALLY_FILLED to avoid stranding funds
		if o.Status == pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED {
			continue
		}

		if o.Side == pb.OrderSide_ORDER_SIDE_BUY {
			buyOrders = append(buyOrders, o)
		} else {
			sellOrders = append(sellOrders, o)
		}
	}

	// Sort orders for cleanup strategy:
	// Buys: Lowest price first (Ascending)
	sort.Slice(buyOrders, func(i, j int) bool {
		p1 := pbu.ToGoDecimal(buyOrders[i].Price)
		p2 := pbu.ToGoDecimal(buyOrders[j].Price)
		return p1.LessThan(p2)
	})

	// Sells: Highest price first (Descending)
	sort.Slice(sellOrders, func(i, j int) bool {
		p1 := pbu.ToGoDecimal(sellOrders[i].Price)
		p2 := pbu.ToGoDecimal(sellOrders[j].Price)
		return p1.GreaterThan(p2)
	})

	// 1. Handle Excess Orders
	totalOrders := len(orders)
	if totalOrders > oc.maxOpenOrders {
		excess := totalOrders - oc.maxOpenOrders
		oc.logger.Warn("Too many open orders",
			"count", totalOrders,
			"max", oc.maxOpenOrders,
			"excess", excess)

		// Balancing Strategy: Cancel from the side with more orders
		for excess > 0 {
			if len(buyOrders) > len(sellOrders) {
				// Cancel Buy
				if len(buyOrders) > 0 {
					ordersToCancel = append(ordersToCancel, buyOrders[0].OrderId)
					buyOrders = buyOrders[1:] // Remove from pool
					excess--
				}
			} else if len(sellOrders) > len(buyOrders) {
				// Cancel Sell
				if len(sellOrders) > 0 {
					ordersToCancel = append(ordersToCancel, sellOrders[0].OrderId)
					sellOrders = sellOrders[1:]
					excess--
				}
			} else {
				// Equal count, cancel both (or one of each to reduce excess by 2)
				if excess >= 2 {
					if len(buyOrders) > 0 {
						ordersToCancel = append(ordersToCancel, buyOrders[0].OrderId)
						buyOrders = buyOrders[1:]
						excess--
					}
					if len(sellOrders) > 0 {
						ordersToCancel = append(ordersToCancel, sellOrders[0].OrderId)
						sellOrders = sellOrders[1:]
						excess--
					}
				} else {
					// Excess is 1, but equal count? pick one (e.g. buy)
					if len(buyOrders) > 0 {
						ordersToCancel = append(ordersToCancel, buyOrders[0].OrderId)
						buyOrders = buyOrders[1:]
						excess--
					}
				}
			}
		}
	}

	// 2. Handle Stale Orders (remaining pool)
	// Re-combine remaining
	remainingOrders := append(buyOrders, sellOrders...)
	for _, order := range remainingOrders {
		age := now.Sub(order.CreatedAt.AsTime())
		if age > oc.maxOrderAge {
			oc.logger.Info("Found stale order",
				"order_id", order.OrderId,
				"age", age)
			ordersToCancel = append(ordersToCancel, order.OrderId)
		}
	}

	// Deduplicate
	ordersToCancel = uniqueInt64(ordersToCancel)

	if len(ordersToCancel) > 0 {
		oc.logger.Info("Canceling orders", "count", len(ordersToCancel))
		err := oc.orderExecutor.BatchCancelOrders(ctx, oc.symbol, ordersToCancel, false)
		if err != nil {
			return fmt.Errorf("failed to cancel orders: %w", err)
		}
	}

	return nil
}

func (oc *OrderCleaner) runLoop() {
	defer oc.wg.Done()

	ticker := time.NewTicker(oc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-oc.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(oc.ctx, 30*time.Second)
			if err := oc.Cleanup(ctx); err != nil {
				oc.logger.Error("Cleanup failed", "error", err.Error())
			}
			cancel()
		}
	}
}

func uniqueInt64(slice []int64) []int64 {
	keys := make(map[int64]bool)
	list := []int64{}
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
