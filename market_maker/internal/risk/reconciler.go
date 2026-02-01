package risk

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Reconciler implements the IReconciler interface
type Reconciler struct {
	exchange        core.IExchange
	positionManager core.IPositionManager
	riskMonitor     core.IRiskMonitor    // Optional
	circuitBreaker  core.ICircuitBreaker // Optional
	logger          core.ILogger
	symbol          string
	interval        time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex

	// Status tracking
	lastResult *pb.GetReconciliationStatusResponse
	statusMu   sync.RWMutex
}

// NewReconciler creates a new reconciler
func NewReconciler(
	exchange core.IExchange,
	positionManager core.IPositionManager,
	riskMonitor core.IRiskMonitor,
	logger core.ILogger,
	symbol string,
	interval time.Duration,
) *Reconciler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Reconciler{
		exchange:        exchange,
		positionManager: positionManager,
		riskMonitor:     riskMonitor,
		logger:          logger.WithField("component", "reconciler"),
		symbol:          symbol,
		interval:        interval,
		ctx:             ctx,
		cancel:          cancel,
		lastResult: &pb.GetReconciliationStatusResponse{
			Status: "never_run",
		},
	}
}

// Start begins the reconciliation loop
func (r *Reconciler) Start(ctx context.Context) error {
	r.logger.Info("Starting reconciler", "interval", r.interval)

	r.wg.Add(1)
	go r.runLoop()

	return nil
}

// Stop stops the reconciler
func (r *Reconciler) Stop() error {
	r.logger.Info("Stopping reconciler")
	r.cancel()
	r.wg.Wait()
	return nil
}

// Reconcile performs a single reconciliation pass
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	recID := fmt.Sprintf("rec_%d", time.Now().UnixNano())
	startTime := time.Now()

	r.statusMu.Lock()
	r.lastResult.ReconciliationId = recID
	r.lastResult.Status = "running"
	r.lastResult.StartedAt = startTime.Unix()
	r.lastResult.Results = nil
	r.statusMu.Unlock()

	// Skip reconciliation if risk monitor is triggered (save API calls)
	if r.riskMonitor != nil && r.riskMonitor.IsTriggered() {
		r.statusMu.Lock()
		r.lastResult.Status = "skipped_risk_triggered"
		r.lastResult.CompletedAt = time.Now().Unix()
		r.statusMu.Unlock()
		return nil
	}

	r.logger.Info("Starting reconciliation pass", "id", recID)

	// 1. Get Exchange State
	// Get all open orders
	openOrders, err := r.exchange.GetOpenOrders(ctx, r.symbol, false)
	if err != nil {
		r.updateStatusFailed(err)
		return fmt.Errorf("failed to get open orders: %w", err)
	}

	// Get current position
	positions, err := r.exchange.GetPositions(ctx, r.symbol)
	if err != nil {
		r.updateStatusFailed(err)
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// Find our symbol's position
	var currentPos *pb.Position
	for _, p := range positions {
		if p.Symbol == r.symbol {
			currentPos = p
			break
		}
	}

	if currentPos == nil {
		// No position exists on exchange, use empty default
		currentPos = &pb.Position{Symbol: r.symbol, Size: pbu.FromGoDecimal(decimal.Zero)}
	}

	// 2. Get Local State Snapshot (Safe Copy)
	slots := r.positionManager.CreateReconciliationSnapshot()

	// 3. Reconcile Orders
	r.reconcileOrders(ctx, slots, openOrders)

	// 4. Reconcile Positions
	match, localSize, exchangeSize, corrected := r.reconcilePositions(ctx, slots, currentPos)

	r.statusMu.Lock()
	r.lastResult.Status = "completed"
	r.lastResult.CompletedAt = time.Now().Unix()
	r.lastResult.Results = []*pb.ReconciliationResult{
		{
			Symbol:           r.symbol,
			PositionMatch:    match,
			LocalPosition:    pbu.FromGoDecimal(localSize),
			ExchangePosition: pbu.FromGoDecimal(exchangeSize),
			Divergence:       pbu.FromGoDecimal(exchangeSize.Sub(localSize)),
			Corrected:        corrected,
		},
	}
	r.statusMu.Unlock()

	r.logger.Info("Reconciliation pass completed", "id", recID)
	return nil
}

func (r *Reconciler) updateStatusFailed(err error) {
	r.statusMu.Lock()
	r.lastResult.Status = "failed"
	r.lastResult.CompletedAt = time.Now().Unix()
	r.statusMu.Unlock()
}

func (r *Reconciler) GetStatus() *pb.GetReconciliationStatusResponse {
	r.statusMu.RLock()
	defer r.statusMu.RUnlock()
	return r.lastResult
}

func (r *Reconciler) runLoop() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
			if err := r.Reconcile(ctx); err != nil {
				r.logger.Error("Reconciliation failed", "error", err.Error())
			}
			cancel()
		}
	}
}

func (r *Reconciler) reconcileOrders(ctx context.Context, slots map[string]*core.InventorySlot, exchangeOrders []*pb.Order) {
	// Map local orders by ID for fast lookup
	localOrderMap := make(map[int64]*core.InventorySlot)
	for _, slot := range slots {
		if slot.OrderId != 0 {
			localOrderMap[slot.OrderId] = slot
		}
	}

	// Map exchange orders by ID for fast lookup
	exchangeOrderMap := make(map[int64]*pb.Order)
	for _, order := range exchangeOrders {
		exchangeOrderMap[order.OrderId] = order
	}

	// 1. Check for missing orders (Local has it, Exchange doesn't)
	for _, slot := range slots {
		if slot.SlotStatus != pb.SlotStatus_SLOT_STATUS_LOCKED || slot.OrderId == 0 {
			continue
		}

		if _, exists := exchangeOrderMap[slot.OrderId]; !exists {
			r.logger.Warn("Order missing on exchange (ghost local order)",
				"order_id", slot.OrderId,
				"slot_price", pbu.ToGoDecimal(slot.Price))

			update := pb.OrderUpdate{
				OrderId:    slot.OrderId,
				Symbol:     r.symbol,
				Status:     pb.OrderStatus_ORDER_STATUS_CANCELED,
				UpdateTime: time.Now().UnixMilli(),
			}
			r.positionManager.OnOrderUpdate(ctx, &update)
		}
	}

	// 2. Check for unknown orders (Exchange has it, Local doesn't)
	// These are "ghost" orders that might have been placed just before a crash.
	for _, exchangeOrder := range exchangeOrders {
		if _, exists := localOrderMap[exchangeOrder.OrderId]; !exists {
			r.logger.Warn("Unknown order found on exchange (ghost exchange order), canceling...",
				"order_id", exchangeOrder.OrderId,
				"side", exchangeOrder.Side,
				"price", exchangeOrder.Price)

			err := r.exchange.CancelOrder(ctx, r.symbol, exchangeOrder.OrderId, false)
			if err != nil {
				r.logger.Error("Failed to cancel ghost exchange order",
					"order_id", exchangeOrder.OrderId,
					"error", err)
			}
		}
	}
}

func (r *Reconciler) reconcilePositions(ctx context.Context, slots map[string]*core.InventorySlot, exchangePos *pb.Position) (bool, decimal.Decimal, decimal.Decimal, bool) {
	// Calculate total local position size
	localSize := decimal.Zero
	for _, slot := range slots {
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			localSize = localSize.Add(pbu.ToGoDecimal(slot.PositionQty))
		}
	}

	exchangeSize := pbu.ToGoDecimal(exchangePos.Size)
	if !localSize.Equal(exchangeSize) {
		r.logger.Warn("Position mismatch detected",
			"local_size", localSize,
			"exchange_size", exchangeSize)

		divergence := exchangeSize.Sub(localSize)
		// Calculate percentage relative to exchange size (add epsilon to avoid div by zero)
		denominator := exchangeSize.Abs()
		if denominator.IsZero() {
			denominator = decimal.NewFromFloat(0.0001)
		}
		divergencePct := divergence.Div(denominator).Mul(decimal.NewFromInt(100)).Abs()

		if divergencePct.LessThan(decimal.NewFromInt(5)) {
			// Small divergence (<5%): Auto-correct
			r.logger.Info("Auto-correcting small position divergence", "divergence_pct", divergencePct)
			if err := r.positionManager.ForceSync(r.ctx, r.symbol, exchangeSize); err != nil {
				r.logger.Error("Failed to force sync position", "error", err)
				return false, localSize, exchangeSize, false
			}
			return false, localSize, exchangeSize, true
		} else {
			// Large divergence (>=5%): Halt trading
			r.logger.Error("CRITICAL: Large position divergence detected - halting trading",
				"divergence_pct", divergencePct)

			if r.circuitBreaker != nil {
				if err := r.circuitBreaker.Open(r.symbol, "large_position_divergence"); err != nil {
					r.logger.Error("Failed to open circuit breaker", "error", err)
				}
			} else {
				r.logger.Error("Circuit breaker not configured, cannot halt trading")
			}
			return false, localSize, exchangeSize, false
		}
	}
	return true, localSize, exchangeSize, false
}

// GetPositionManager returns the position manager used by the reconciler
func (r *Reconciler) GetPositionManager() core.IPositionManager {
	return r.positionManager
}

// SetCircuitBreaker sets the circuit breaker for the reconciler
func (r *Reconciler) SetCircuitBreaker(cb core.ICircuitBreaker) {
	r.circuitBreaker = cb
}

// TriggerManual triggers a manual reconciliation immediately
func (r *Reconciler) TriggerManual(ctx context.Context) error {
	r.logger.Info("Manual reconciliation triggered")
	return r.Reconcile(ctx)
}
