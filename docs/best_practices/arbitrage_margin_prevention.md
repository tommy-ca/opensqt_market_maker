# Multi-Pair Portfolio Arbitrage & Unified Margin: Prevention Strategies

This document outlines the prevention strategies and best practices for the Multi-Pair Portfolio Arbitrage and Unified Margin (UM) systems, focusing on safety, precision, and robustness.

## 1. Avoiding "Toxic Funding" (Basis Stop)

"Toxic Funding" occurs when the Spot-Perp basis flips negative (Perp trades at a significant discount to Spot) for a sustained period, making the arbitrage position unprofitable despite receiving funding (or causing heavy losses if paying funding).

### Prevention Strategies
- **Basis at Risk (BaR) Metric**: Implement a rolling average of the `(Perp Mid - Spot Mid) / Spot Mid` basis. If this value stays below a "Toxic Threshold" (e.g., -0.05% for 3+ funding intervals), trigger an automatic exit.
- **Entry Gating**: Block new entries if the current basis is narrower than the historical 1st percentile or if the `predicted_funding` is trending towards zero while the basis is already negative.
- **Regime Detection**: Monitor the "Quality Score" from the `UniverseSelector`. A sharp drop in the Quality Score often precedes a toxic regime change.

### Best Practices
- **Rolling Windows**: Use a median or volume-weighted basis over a 5-minute window to filter out transient flash-crash spikes.
- **Hysteresis**: Use different thresholds for "Exit" and "Re-entry" to prevent "ping-ponging" the position during volatile basis regimes.

## 2. Maintaining Delta Neutrality during Partial Fills

Partial fills on one leg create unhedged delta exposure, exposing the portfolio to market direction risk.

### Prevention Strategies
- **Passive-First Execution**: Place the "restricted" or "illiquid" leg first (usually the Spot leg if using margin, or the leg with higher maker fees). Wait for the fill before placing the second leg.
- **Atomic Scaling**: As implemented in the `ArbitrageWorkflows`, always align the second leg's quantity to the first leg's `ExecutedQty`.
- **Immediate Compensation**: If the second leg fails to place (e.g., due to "Insufficient Margin" or "Risk Limit"), the system must immediately trigger an unwind of the first leg's executed portion.

### Best Practices
- **Deterministic IDs**: Use `client_order_id` patterns like `entry_{symbol}_{timestamp}_{attempt}` and `unwind_{original_id}` to ensure idempotency and traceability during partial fills.
- **Slippage Buffers**: Use aggressive `PriceSlippage` limits on the second leg (the "hedge" leg) to ensure it fills immediately even if the market moves slightly after the first leg.

## 3. Ensuring Numerical Precision (Hybrid Go/Python)

Floating-point errors can lead to "dust" positions, margin breaches, or rejected orders due to incorrect rounding.

### Prevention Strategies
- **Decimal-Only Core**: Use `shopspring/decimal` (Go) and `decimal.Decimal` (Python) for all price, quantity, and balance logic.
- **String-Based Transit**: Use `google.type.Decimal` (which stores value as a string) for all gRPC and Protocol Buffer communication between Go and Python.
- **Zero-Tolerance Linting**: Implement cross-language tests that verify `0.1 + 0.2` results in exactly `0.3` across the gRPC bridge.

### Best Practices
- **Late Rounding**: Only round numbers at the "Adapter" layer immediately before sending to the exchange, using the exchange-provided `lot_size` or `price_filter`.
- **Dust Management**: Treat quantities below the exchange's `min_qty` as zero to avoid "stuck" partial fills that cannot be hedged.

## 4. Monitoring & Alerting

### Key Alerts
- **Delta Leakage**: Trigger a CRITICAL alert if `|Spot_Position + Perp_Position| > Threshold` for more than 30 seconds.
- **Health Score Drop**: Trigger a WARNING if the UM `health_score` drops below 0.8, and a CRITICAL (Automatic De-leveraging) if it drops below 0.6.
- **Basis Inversion**: Alert if the basis flips negative for more than 1 hour.
- **Stale Funding**: Alert if the `next_funding_time` has passed but no new funding rate has been received for 60 seconds.

### Test Cases
- **Simulated Partial Fill**: Unit test the `ArbitrageWorkflow` by mocking a 50% fill on the Spot leg and verifying the Perp leg scales correctly.
- **Precision Round-Trip**: Verify that a Python-generated decimal (e.g., `1.00000001`) reaches the Go engine and is stored in the DB without losing the last digit.
- **Circuit Breaker Trip**: Manually inject a `health_score = 0.5` into the `MarginSim` and verify that all pending entry workflows are aborted.
