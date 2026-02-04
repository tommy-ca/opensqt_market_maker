---
id: 2026-02-03-declarative-target-state-reconciliation
title: Declarative Target-State Reconciliation
category: architecture-patterns
status: solved
components:
  - GridEngine
  - ArbEngine
  - PortfolioController
symptoms:
  - Delta drift on partial fills
  - Fragmented strategy logic
  - Transient rebalance loops
tags:
  - reconciliation-pattern
  - declarative-execution
  - durable-workflows
  - grid-trading
  - funding-arbitrage
related_issues:
  - "market_maker/docs/specs/arbitrage_bot_design.md"
  - "market_maker/docs/specs/grid_strategy_design.md"
  - "docs/solutions/performance-issues/arbitrage-strategy-execution-optimization-20260201.md"
---

# Declarative Target-State Reconciliation

## Problem Statement

The system was originally built on an **imperative action-based model**. In this model, strategies would analyze the market and issue discrete commands like `PlaceOrder` or `CancelOrder`. This approach suffered from several critical weaknesses:

1.  **Missing Middle State**: If a process crashed between placing an order and updating the local database, the system became inconsistent (e.g., an order existed on the exchange but was "forgotten" locally).
2.  **Delta Neutrality Drift**: In funding arbitrage, if a Spot leg only partially filled, the system often failed to precisely scale the hedging Perp leg, leading to unhedged directional exposure.
3.  **Logic Fragmentation**: Grid trading logic was duplicated across multiple packages, leading to maintenance debt and divergent behavior between "simple" and "durable" engines.

## Investigation & Findings

### Root Cause Analysis
The root cause was the **Event-Action Binding**. The system treated trading signals as transient events rather than persistent desires.

*   **Imperative Logic**: "If price > threshold, THEN cancel order X." If the cancellation failed due to a network timeout, the logic didn't automatically retry on the next tick because the "event" had already passed.
*   **Sequential Blindness**: In multi-leg trades (Arb), the second leg was often sized based on the *requested* quantity of the first leg, rather than its *actual execution results*.

## Solution: The Declarative Reconciler

The system transitioned to a **Declarative Target-State Reconciliation** pattern, similar to the architecture used by Kubernetes or React.

### 1. Primal State Responsibility
Strategies were refactored to be **Pure Functions**. Instead of issuing actions, they now return an immutable **`TargetState`** struct. This struct describes the holistic ideal state of the world:
-   Exactly which positions should be held.
-   Exactly which orders should be active.

### 2. The Engine as a Controller
The **Trading Engine** (Grid or Arb) now acts as a controller loop. On every update:
1.  It fetches the **Current State** (Exchange reality + local cache).
2.  It calls the strategy to get the **Target State**.
3.  It computes the **Delta** between Current and Target.
4.  It executes the minimum set of actions (Place/Cancel/Adjust) to converge reality to the target.

### 3. Workflow Hardening
-   **IOC (Immediate-or-Cancel)**: Arbitrage entry legs now use IOC orders. This forces the exchange to return immediate execution results, allowing the reconciler to precisely hedge the delta without complex "waiting" state machines.
-   **Parallel Durable Exits**: Critical risk operations (Emergency Exits) are wrapped in **DBOS Durable Workflows** that execute both legs concurrently. This minimizes directional risk while ensuring atomicity and crash recovery.
-   **Idempotency via ClientOrderID**: The `ClientOrderID` serves as the natural idempotency key. The reconciler ensures that a workflow for a specific OID is never duplicated, and DBOS ensures that if a workflow is interrupted, it resumes from the last successful step rather than double-placing orders.

## Code Examples

### Before: Imperative Commands
```go
// Brittle: Fails to reconcile drift on next tick
func (s *GridStrategy) CalculateActions(price decimal.Decimal, slots map[string]*Slot) []*pb.OrderAction {
    if price < s.anchor && !s.hasOrderAtLevel(price, slots) {
        return []*pb.OrderAction{{
            Type: pb.Action_PLACE,
            Side: pb.Side_BUY,
            Price: price,
            Qty: s.orderQty,
        }}
    }
    return nil
}
```

### After: Declarative Target State

```go
// Robust: Describes the final ideal state; Engine converges reality
func (s *GridStrategy) CalculateTargetState(ctx context.Context, price, anchor, atr decimal.Decimal, ...) (*core.TargetState, error) {
    // Quantities are ALWAYS handled as decimal.Decimal
    qty := s.orderQty.Round(s.qtyDecimals)

    target := &core.TargetState{
        Positions: []core.TargetPosition{
            {Symbol: s.Symbol, Size: s.desiredNetPosition},
        },
        Orders: []core.TargetOrder{
            {Price: buyLevel, Quantity: qty, Side: "BUY", ClientOrderID: "grid_b_1"},
            {Price: sellLevel, Quantity: qty, Side: "SELL", ClientOrderID: "grid_s_1"},
        },
    }
    return target, nil
}
```

### Python Implementation Pattern

In Python, we achieve the same rigor using `dataclasses` and the standard `decimal` library.

```python
from dataclasses import dataclass
from decimal import Decimal
from typing import List

@dataclass(frozen=True)
class TargetOrder:
    price: Decimal
    quantity: Decimal
    side: str
    client_order_id: str

@dataclass(frozen=True)
class TargetState:
    symbol: str
    desired_position: Decimal
    orders: List[TargetOrder]

def calculate_target_state(price: Decimal, inventory: Decimal) -> TargetState:
    # Pure function: easy to test, no side effects
    return TargetState(
        symbol="BTC/USDT",
        desired_position=Decimal("0.5"),
        orders=[...]
    )
```

## Prevention & Best Practices

To prevent state drift and "missing middle" bugs in future modules:

### 1. "Target State" First Principle
-   **Always return the TargetState**: Never write a strategy that emits "Actions." The strategy should only answer: "What should the world look like right now?"
-   **Immutability**: The `TargetState` must be immutable. This makes testing trivial—verify convergence by comparing `Reality - Target`.

### 2. Rebalancing Safety
-   **Margin Health Gates**: When rebalancing a multi-pair portfolio (e.g., in `PortfolioController`), implement "Validation Gates" between legs. Use DBOS to ensure that if a process crashes while moving margin, it resumes and verifies account health before proceeding.
-   **Atomic Scaling**: Ensure that any hedging leg is calculated based on the `ExecutedQty` of the primary leg.

### 3. Numerical Precision
-   **No Floats**: Never use `float64` or `float` for prices, quantities, or notionals. Always use `decimal.Decimal` (Go) or `decimal.Decimal` (Python).
-   **Normalization**: When converting from Protobuf or JSON, immediately convert to the native Decimal type. Use `:f` formatting in Python or `.String()` in Go to avoid scientific notation when sending back to exchanges.
-   **Rounding**: Strategies must explicitly round their `TargetState` values to the exchange's `tick_size` and `step_size` BEFORE returning the state.

### 4. Testing Patterns
-   **Drift Simulation**: In E2E tests, manually inject a "ghost order" onto the exchange and verify that the reconciler identifies and cancels it automatically on the next cycle.
-   **Partial Fill Testing**: Simulate an IOC order that only fills 42% and verify that the second leg scales exactly to 42% without manual intervention.

### 5. Architectural Unification
- **Centralized Diffing**: Future strategy implementations should utilize a unified `core.Diff(current, target) []Action` utility to ensure consistent reconciliation behavior across all engines (Grid, Arb, etc.), further reducing boilerplate in the engine layer.

## References
-   **Specification 026**: `market_maker/docs/specs/arbitrage_bot_design.md`
-   **Grid Design**: `market_maker/docs/specs/grid_strategy_design.md`
-   **Workflow Orchestrator**: `market_maker/docs/specs/orchestrator_workflow.md`
-   **Durable Engine implementation**: `market_maker/internal/engine/gridengine/durable.go`
