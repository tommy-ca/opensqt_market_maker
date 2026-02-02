---
status: resolved
priority: p1
issue_id: 021
tags: [code-review, data-integrity, critical, position-reconciliation]
dependencies: []
---

# Position Reconciliation Detects But Never Corrects Divergence

## Problem Statement

**Location**: `internal/risk/reconciler.go:194-209`

Position reconciliation detects mismatches but **takes no corrective action**:

```go
if !localSize.Equal(exchangeSize) {
    r.logger.Warn("Position mismatch detected",
        "local_size", localSize,
        "exchange_size", exchangeSize)
    // ⚠️ NO CORRECTIVE ACTION - divergence continues indefinitely
}
```

**Impact**:
- **CRITICAL**: Position tracking desynchronizes from exchange reality
- **Financial risk**: System thinks it has different position than actual
- **Risk limit breach**: May exceed position limits unknowingly
- **Trading errors**: Orders based on incorrect position assumptions
- **Audit failure**: Regulatory requirement to maintain accurate position tracking

## Evidence

From Data Integrity Guardian review:
> "The reconciler logs position mismatches but does not correct them. The local position manager state remains diverged from the exchange's reality, which can lead to risk limit violations or incorrect trading decisions."

## Failure Scenario

**Timeline of divergence**:
1. **T=0**: Local position = 100 BTC, Exchange position = 100 BTC ✓
2. **T=1**: Network disruption, WebSocket message lost
3. **T=2**: Order fills on exchange, position = 150 BTC
4. **T=3**: Local position still = 100 BTC (missed update)
5. **T=5**: Reconciler runs, detects mismatch (100 vs 150)
6. **T=5**: Logs warning, **NO CORRECTION**
7. **T=10**: System places order assuming 100 BTC position
8. **T=11**: Actual position = 200 BTC (150 + 50)
9. **T=12**: Risk limit exceeded (limit = 175 BTC)
10. **T=13**: Exchange liquidates position → **Financial loss**

## Root Cause Analysis

**Design flaw**: Reconciler is "read-only" - detects but doesn't correct.

**Missing logic**:
- No position adjustment mechanism
- No notification to position manager
- No circuit breaker on detected divergence
- No escalation path

## Proposed Solutions

### Option 1: Automatic Correction (Recommended for Small Divergence)

**Effort**: 8-12 hours

```go
func (r *Reconciler) reconcileOrders(ctx context.Context, slots map[string]*core.InventorySlot, exchangeOrders []*pb.Order) {
    // ... existing comparison logic

    if !localSize.Equal(exchangeSize) {
        divergence := exchangeSize.Sub(localSize)
        divergencePct := divergence.Div(exchangeSize).Mul(decimal.NewFromInt(100))

        r.logger.Warn("Position mismatch detected",
            "symbol", symbol,
            "local_size", localSize,
            "exchange_size", exchangeSize,
            "divergence", divergence,
            "divergence_pct", divergencePct)

        // Emit metrics
        r.metrics.PositionDivergence.WithLabelValues(symbol).Set(divergence.InexactFloat64())

        // CORRECTIVE ACTION based on divergence size
        if divergencePct.Abs().LessThan(decimal.NewFromFloat(5.0)) {
            // Small divergence (<5%) - Auto-correct
            r.logger.Info("Auto-correcting small position divergence")
            if err := r.positionManager.ForceSync(ctx, symbol, exchangeSize); err != nil {
                r.logger.Error("Failed to sync position", "error", err)
                return
            }
            r.metrics.PositionCorrections.WithLabelValues(symbol, "auto").Inc()
        } else {
            // Large divergence (≥5%) - Halt trading and alert
            r.logger.Error("CRITICAL: Large position divergence detected - halting trading")

            // Circuit breaker - stop all trading for this symbol
            if err := r.circuitBreaker.Open(symbol, "position_divergence"); err != nil {
                r.logger.Error("Failed to open circuit breaker", "error", err)
            }

            // Alert operations team
            r.alertManager.Send(Alert{
                Severity: "CRITICAL",
                Title:    "Position Divergence Detected",
                Message:  fmt.Sprintf("Symbol %s: local=%s, exchange=%s, divergence=%s%%",
                    symbol, localSize, exchangeSize, divergencePct),
                Action:   "Manual investigation required",
            })

            r.metrics.PositionCorrections.WithLabelValues(symbol, "manual_required").Inc()
        }
    }
}
```

### Option 2: Manual Approval Flow

**Effort**: 12-16 hours

```go
type ReconciliationAction struct {
    Symbol        string
    LocalPosition decimal.Decimal
    ExchangePosition decimal.Decimal
    DetectedAt    time.Time
    Status        string  // pending, approved, rejected
    ApprovedBy    string
}

func (r *Reconciler) reconcileOrders(ctx context.Context, slots map[string]*core.InventorySlot, exchangeOrders []*pb.Order) {
    if !localSize.Equal(exchangeSize) {
        // Create pending action
        action := &ReconciliationAction{
            Symbol:           symbol,
            LocalPosition:    localSize,
            ExchangePosition: exchangeSize,
            DetectedAt:       time.Now(),
            Status:           "pending",
        }

        // Store for manual review
        if err := r.actionStore.Save(ctx, action); err != nil {
            r.logger.Error("Failed to save reconciliation action", "error", err)
        }

        // Halt trading until approved
        r.circuitBreaker.Open(symbol, "pending_reconciliation")

        // Notify operations
        r.notifyOps(action)
    }
}

// Separate approval endpoint
func (r *Reconciler) ApproveReconciliation(ctx context.Context, actionID string, approver string) error {
    action, err := r.actionStore.Get(ctx, actionID)
    if err != nil {
        return err
    }

    // Apply correction
    if err := r.positionManager.ForceSync(ctx, action.Symbol, action.ExchangePosition); err != nil {
        return err
    }

    // Update action
    action.Status = "approved"
    action.ApprovedBy = approver
    r.actionStore.Update(ctx, action)

    // Resume trading
    r.circuitBreaker.Close(action.Symbol)

    return nil
}
```

## Recommended Action

**Implement Hybrid Approach**:

1. **Automatic correction** for small divergence (<5%)
   - Fast recovery from transient issues
   - Minimal operational overhead

2. **Circuit breaker + manual approval** for large divergence (≥5%)
   - Prevents trading on bad data
   - Human review for significant issues
   - Audit trail for compliance

3. **Add PositionManager.ForceSync() method**:
```go
func (m *PositionManager) ForceSync(ctx context.Context, symbol string, exchangePosition decimal.Decimal) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    key := PositionKey{Symbol: symbol}
    currentPosition := m.positions[key]

    // Create synthetic adjustment update
    adjustment := exchangePosition.Sub(currentPosition.Quantity)

    m.logger.Warn("Force syncing position from reconciliation",
        "symbol", symbol,
        "old_quantity", currentPosition.Quantity,
        "new_quantity", exchangePosition,
        "adjustment", adjustment)

    // Update position
    currentPosition.Quantity = exchangePosition
    currentPosition.UpdatedAt = time.Now()

    // Persist to database
    if err := m.store.SavePosition(ctx, currentPosition); err != nil {
        return fmt.Errorf("failed to persist position sync: %w", err)
    }

    return nil
}
```

## Monitoring

**Metrics to add**:
```go
prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "position_divergence_detected_total",
    Help: "Total number of position divergences detected",
}, []string{"symbol", "severity"}) // severity: small, large

prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "position_corrections_total",
    Help: "Total number of position corrections applied",
}, []string{"symbol", "type"}) // type: auto, manual

prometheus.NewGaugeVec(prometheus.GaugeOpts{
    Name: "position_divergence_amount",
    Help: "Current position divergence amount",
}, []string{"symbol"})
```

**Alerts**:
- Alert on any large divergence (≥5%)
- Alert on frequent small divergences (>10/hour)
- Alert on failed auto-corrections

## Acceptance Criteria

- [ ] Small divergence (<5%) auto-corrects within 1 reconciliation cycle
- [ ] Large divergence (≥5%) opens circuit breaker and halts trading
- [ ] PositionManager.ForceSync() method implemented
- [ ] Metrics exported for divergence detection and corrections
- [ ] Alerts configured for critical divergences
- [ ] Manual approval flow implemented (optional but recommended)
- [ ] Integration test: Inject position divergence, verify correction
- [ ] All tests pass

## Resources

- Data Integrity Guardian Report: Critical finding #1
- File: `internal/risk/reconciler.go`
- Related: Issue #009 (idempotency - already resolved)
- Related: Issue #010 (stream reconnection - already resolved)
