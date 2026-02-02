---
title: "feat: Implement Unified Margin support for Funding Arbitrage"
type: feat
date: 2026-01-29
---

# feat: Implement Unified Margin support for Funding Arbitrage

## Overview

This feature implements support for **Unified Margin (UM)** accounts across Bybit (UTA), Binance (Portfolio Margin), OKX (Unified Account), and Bitget. It enables the system to treat Spot and Perpetual positions as a single pool of risk, allowing unrealized profits in one leg to serve as collateral for the other. This significantly increases capital efficiency (40-60% reduction in margin) and reduces liquidation risk during market-neutral arbitrage.

## Problem Statement / Motivation

1. **Capital Inefficiency**: Segregated accounts require 2x collateral (margin for spot and margin for perp).
2. **Phantom Liquidations**: In segregated accounts, a price spike can liquidate the perp leg even if the spot leg is in offsetting profit.
3. **Stale Safety Metrics**: Relying on 5s polling for account health leads to delayed reactions during volatility.

## Proposed Solution

1. **Protocol Standardization**: Update `pb.Account` with `health_score` (normalized 0.0-1.0) and `MarginMode`.
2. **Adapter-Based Normalization**: Each exchange adapter (Bybit, Binance, OKX) implements its own mapping of proprietary metrics (MMR, uniMMR, mgnRatio) to the unified `health_score`.
3. **Same-Exchange Atomic Entry**: Use `BatchPlaceOrders` for same-exchange arbitrage to minimize execution skew.
4. **Virtual Margin Engine (VME)**: A real-time safety layer that estimates account health between API snapshots using WebSocket price ticks.
5. **Two-Tiered Risk Guard**:
   - **Warning (0.7 Health)**: Proactively reduce position size by 50% to restore margin.
   - **Emergency (0.5 Health)**: Full atomic exit of the arbitrage pair.

## Technical Approach

### 1. Model & Protocol (`resources.proto`)
- `is_unified`: Boolean flag.
- `health_score`: Normalized safety metric (1.0 = Safe, 0.0 = Liquidation).
- `MarginMode`: Enum (`REGULAR`, `UNIFIED`, `PORTFOLIO`).
- `adjusted_equity`: Effective collateral value after haircuts (for informational UI).

### 2. Exchange Adapters (Implementation)
- **Bybit UTA**: Map `accountMMRate` → `1.0 - MMR`.
- **Binance PM**: Map `uniMMR` → `1.0 - (1.0 / uniMMR)`.
- **OKX Unified**: Map `mgnRatio` → normalized ratio.
- **Normalization Safeguard**: All health math must be clamped to `[0, 1]` and handle division-by-zero or glitchy `0` values from APIs.

### 3. Risk Management
- **ADL Monitoring**: Explicitly watch execution reports for `ADL` (Auto-Deleveraging) flags to immediately close the corresponding spot leg.
- **Collateral Efficiency**: Rank positions by `FundingRate / (1 - Haircut)` to determine which assets to reduce first during a margin warning.
- **Shadow Ledger**: Maintain a local estimate of collateral value updated by WebSocket prices.

## Acceptance Criteria

- [x] `pb.Account` updated and regenerated for Go and Python.
- [x] `IExchange` interface supports `IsUnifiedMargin()` detection.
- [x] Binance PAPI adapter implements `GetAccount` mapping `uniMMR` and `actualEquity`.
- [x] Bybit UTA adapter implements `wallet` WebSocket stream for real-time MMR updates.
- [x] `ArbitrageEngine` supports `SameExchangeExecutor` using `BatchPlaceOrders`.
- [x] `RiskMonitor` triggers 50% reduction at 0.7 health and full exit at 0.5.
- [x] Unit tests for VME (Virtual Margin Engine) price-based health estimation.
- [x] Documentation warning users to use dedicated sub-accounts even with UM enabled.


## Success Metrics

- **Latency**: Account health updates < 500ms from price move (via VME).
- **Efficiency**: 50% average reduction in required collateral for Spot-Perp pairs.
- **Reliability**: Zero "orphan legs" during UM target switches due to batch execution.

## Dependencies & Risks

- **Haircut Volatility**: Exchanges changing collateral weights can cause health to drop without price movement.
- **API Payload Bloat**: UM snapshots are larger; adapters should use field filtering where available.
- **Basis Disconnect**: Mark Price (Perp) diverging from Index Price (Spot) can cause margin squeeze even if Delta is neutral.

## Enhancement Summary (Deepened)

**Deepened on**: Jan 29, 2026
**Agents used**: performance-oracle, architecture-strategist, security-sentinel, spec-flow-analyzer, code-simplicity-reviewer, best-practices-researcher, framework-docs-researcher.

### Key Improvements
1. **Virtual Margin Engine**: Transitioned from polling snapshots to real-time price-adjusted health estimation to eliminate the "5s blind spot."
2. **Simplified Risk Tiers**: Consolidated 4 complex tiers into 2 actionable states (Warning/Emergency) for improved reliability.
3. **ADL Detection**: Added specific monitoring for Auto-Deleveraging events, a critical risk in UM environments.
4. **Batch Execution**: Mandated the use of `BatchPlaceOrders` for same-exchange legs to reduce hedge slippage.
5. **Safety Clamping**: Implemented numerical safeguards for normalization math to prevent panics on API glitches.
