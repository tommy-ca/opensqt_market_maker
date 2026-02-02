---
date: 2026-01-29
topic: multi-pair-portfolio-arbitrage
---

# Multi-Pair Portfolio Arbitrage Engine

## What We're Building
A high-performance **Portfolio Management Layer** for the Funding Arbitrage system. This component will evolve the system from managing a single target symbol to orchestrating a dynamic portfolio of multiple arbitrage pairs on a single **Unified Margin (UM)** account. It will leverage the "Quality Score" from the `UniverseSelector` to dynamically allocate capital, ensuring the account always holds the highest-expectation, most stable yield positions while maintaining global health.

## Why This Approach
We chose the **Orchestrator-Led Portfolio** approach (Approach A) because it preserves the **SOLID** principles established in the project. By using the `orchestrator` as a global governor, we can reuse the existing `ArbitrageEngine` logic for execution while centralizing the complex "Global Health" and "Capital Allocation" decisions. This avoids bloating individual engines with awareness of their peers.

## Key Decisions
- **Dynamic Allocation**: Pairs are added or removed based on a "Minimum Quality Threshold" and "Account Headroom" rather than fixed slots.
- **Yield-Weighted Sizing**: Position sizes will be scaled proportionally to their `QualityScore` (e.g., a "Perfect" stability pair gets 2x the allocation of a "Fresh" regime pair).
- **Hysteresis-Based Swapping**: A new pair must have a score >1.5x of an active pair to trigger a "forced swap" (exit old, enter new) to minimize fee churn.
- **Global De-leveraging**: If the shared UM `health_score` drops below 0.7, the Portfolio Manager will trigger a "Ranking-Based Reduction," closing 50% of the **lowest-scoring** pair first.

## Open Questions
- **Cross-Exchange Portfolios**: How do we handle a portfolio that spans multiple UM accounts (e.g., Bybit UM and Binance PM) simultaneously?
- **Execution Priority**: Should we execute multi-pair entries/exits sequentially or in parallel? (Parallel is faster but increases API burst risk).
- **Haircut Aggregation**: How do we efficiently calculate the "Portfolio ECV" when different pairs have overlapping collateral (e.g., BTC as both a trade leg and collateral)?

## Next Steps
→ `/workflows:plan` to design the `PortfolioController` interface and the capital allocation math.
