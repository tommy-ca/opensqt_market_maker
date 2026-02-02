---
id: 2026-01-29-multi-pair-portfolio-arbitrage
title: Multi-Pair Portfolio Arbitrage and Unified Margin Support
category: architecture-patterns
status: solved
date: 2026-01-29
components:
  - PortfolioController
  - MarginSim (VME)
  - BinanceAdapter
  - OKXAdapter
  - PythonConnector
symptoms:
  - Capital inefficiency
  - Liquidation risk
  - Code duplication
  - Numerical precision issues
tags:
  - portfolio-management
  - unified-margin
  - reconciliation-pattern
  - risk-engine
---

# Multi-Pair Portfolio Arbitrage and Unified Margin Support

## Problem Statement

The system originally operated as a single-pair Funding Arbitrage bot with several critical limitations:
1. **Capital Inefficiency**: Capital was locked into one pair, leaving yield on the table and creating margin "idle time" during sequential swaps.
2. **Liquidation Risk**: Segregated accounts created binary risk; a price spike could liquidate the hedge leg even if the spot leg was in profit.
3. **High Maintenance Debt**: All 5 exchange adapters shared ~4000+ lines of duplicated code, and numerical precision issues existed due to floating-point usage in the Python connector.

## Root Cause Analysis

*   **MVP Scope Constraints**: The initial design focused on a single-pair "proof of concept," missing the orchestration layer needed for dynamic capital allocation.
*   **Decentralized Adapter Logic**: Lack of a robust `BaseAdapter` led to copy-pasted HTTP/WebSocket management across venues.
*   **Float drift**: Using `float` in Python for monetary values introduced rounding errors that caused order rejections and "dust" positions.

## Working Solution

Implemented a sophisticated **Portfolio Orchestrator** and **Unified Margin (UM)** risk model.

### 1. Active Reconciler Pattern
Implemented `PortfolioController`, which manages the lifecycle of multiple `ArbitrageEngine` instances. It uses a target-state reconciliation loop: `Ideal Positions - Current Positions = Adjustment Actions`.

### 2. Precision Virtual Margin Engine (VME)
Developed `MarginSim` to calculate **Effective Collateral Value (ECV)** using exchange-specific haircuts and high-frequency WebSocket price updates.

```go
// Health Score Estimation with 15% Shadow Margin Buffer
// Health = 1 - (TMM * (1 + SafetyBuffer) / AdjustedEquity)
safeTMM := projectedTMM.Mul(decimal.NewFromInt(1).Add(s.safetyBuffer))
health := decimal.NewFromInt(1).Sub(safeTMM.Div(projectedAdjEq))
```

### 3. Native Batch Execution
Upgraded adapters to use native exchange batch endpoints (e.g., Binance `papi/v1/batchOrders`) for atomic-like multi-leg execution.

```python
# ccxt.create_orders implementation in BinanceConnector
orders = await self.exchange.create_orders(ccxt_orders)
```

### 4. Full-Stack Decimal Refactor
Migrated the entire hybrid Go/Python stack to use high-precision decimal types (`shopspring/decimal` in Go, `decimal.Decimal` in Python), ensuring exact rounding at the exchange lot/tick boundaries.

## Prevention Strategies

### Basis Stop (Toxic Funding Guard)
Monitors Spot-Perp basis premium. If it flips negative (beyond -5 bps) for 3 consecutive intervals, the system triggers an emergency exit to avoid "toxic funding" regimes.

### Atomic Delta Neutrality
Ensures partial fills on the primary leg (Spot) are immediately matched by the hedge leg (Perp) by dynamically scaling the second leg's quantity to the first leg's `ExecutedQty`.

### Proportional Entry Protocol
Eliminates capital idle time by allowing new entries to scale dynamically as exit legs fill, rather than waiting for binary completion.

## Related Documentation

*   **Roadmap**: `docs/plans/2026-01-29-feat-multi-pair-portfolio-arbitrage-plan.md`
*   **Unified Margin Spec**: `docs/plans/2026-01-29-feat-unified-margin-arbitrage-plan.md`
*   **Infrastructure**: `docs/specs/phase18_orchestrator_tech_spec.md`
*   **Best Practices**: `docs/best_practices/arbitrage_margin_prevention.md`

## Use Cases

1. **High-Yield Portfolio**: Managing 10+ symbols on a single Bybit UTA account with 3x leverage.
2. **Crash-Resilient Swapping**: Automating the transition from a declining yield pair (e.g., BTC) to a fresh regime (e.g., SOL) without losing margin headroom.
3. **Agent-Assisted Trading**: Exposing `SimulateMargin` tools to external AI agents for collaborative risk analysis.
