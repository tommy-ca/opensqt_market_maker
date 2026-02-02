---
module: Strategy & Execution
date: 2026-02-01
problem_type: performance_issue
component: tooling
symptoms:
  - "Inefficient historical funding analysis scans (>60s)"
  - "Hedge slippage during sequential cross-exchange exits"
  - "Delta exposure due to unscaled second leg on partial fills"
  - "Python Protobuf import errors after connector refactor"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags: [arbitrage, worker-pool, atomic-neutrality, parallel-execution, protobuf]
---

# Troubleshooting: Arbitrage Strategy & Execution Optimization

## Problem
The Funding Arbitrage system suffered from execution inefficiencies and logical gaps that led to capital risk. Specifically, sequential scanning of symbols was slow, sequential closing of cross-exchange legs increased hedge slippage, and fixed-quantity order placement created delta exposure when the first leg only partially filled. Additionally, refactoring to a hybrid workspace introduced Protobuf registry collisions in Python.

## Environment
- Module: Strategy & Execution (Arbitrage Engine)
- Affected Component: `UniverseSelector`, `ArbitrageEngine`, `ParallelExecutor`, `python-connector`
- Date: 2026-02-01

## Symptoms
- **Slow Scans**: Historical funding analysis for 100+ symbols took over 60 seconds, risking rate limits and stale data.
- **Hedge Slippage**: Sequential execution of exit legs meant the second leg was delayed, often resulting in unfavorable price moves.
- **Atomic Neutrality Violation**: On partial fills of the Spot leg, the Perp leg was still placed for the full requested quantity, resulting in an unhedged delta position.
- **Python Import Errors**: `TypeError: Couldn't build proto file into descriptor pool: Depends on file 'opensqt/market_maker/v1/types.proto', but it has not been loaded`.

## What Didn't Work

**Attempted Solution 1:** Using `errgroup` for per-scan parallelism.
- **Why it failed:** Spawning hundreds of goroutines per scan interval (every 5-15 mins) introduced unnecessary overhead and didn't solve I/O bottlenecks efficiently compared to a persistent worker pool.

**Attempted Solution 2:** Updating `MockExchange` to auto-fill market orders.
- **Why it failed:** While it helped testing, it didn't address the underlying production logic requiring dynamic scaling of the second leg based on the first leg's fill.

## Solution

The implementation addressed performance and safety through several architectural changes:

1.  **Persistent Worker Pool**: Refactored `UniverseSelector` to maintain a pool of workers, reducing goroutine churn and enabling efficient parallel historical scans.
2.  **ParallelExecutor**: Created a new execution primitive for risk-sensitive operations (exits) to place multiple orders concurrently across exchanges.
3.  **Atomic Neutrality Scaling**: Re-engineered `ArbitrageEngine.executeEntry` to place the first leg, capture the actual `ExecutedQty`, and dynamically scale the second leg to match.
4.  **Protobuf Stabilization**: Standardized `make proto` across the workspace and fixed Python import paths to ensure consistent descriptor pool registration.

**Code changes** (Atomic Neutrality):
```go
// Before: Sequential/Fixed
steps := []execution.Step{
    {Exchange: e.spotExchange, Request: spotReq},
    {Exchange: e.perpExchange, Request: perpReq},
}
return e.executor.Execute(ctx, steps)

// After: Dynamic Scaling
spotOrder, err := spotEx.PlaceOrder(ctx, spotReq)
if err != nil { return err }

execQty := pbu.ToGoDecimal(spotOrder.ExecutedQty)
if execQty.IsZero() { return fmt.Errorf("zero fill") }

// Scale second leg to actual fill of first leg
perpReq.Quantity = pbu.FromGoDecimal(execQty)
_, err = perpEx.PlaceOrder(ctx, perpReq)
```

**Commands run**:
```bash
# Steps taken to fix:
cd market_maker && make proto
cd python-connector && uv run ruff check . --fix
```

## Why This Works

1.  **ROOT CAUSE**: The root cause was a combination of **logical design gaps** (sequential execution by default) and **infrastructure overhead** (per-request goroutines). The Python issue was a **configuration error** in how generated files were imported.
2.  **Logic**: Atomic Neutrality scaling ensures that we never over-hedge or under-hedge on the second leg, which is critical for delta-neutral arbitrage.
3.  **Parallelism**: Concurrent execution for exits minimizes the time window where one leg is closed and the other is open, reducing exposure to market volatility.

## Prevention

- **Worker Pool Pattern**: Use persistent worker pools for high-frequency or bulk I/O tasks like scanning.
- **Parallel-by-Default for Risk**: Use `ParallelExecutor` for emergency exits or risk-reduction maneuvers.
- **Leg Scaling**: Always scale subsequent legs of an atomic transaction to the actual fill of preceding legs.
- **CI/CD Proto Validation**: Ensure `make proto` is run in CI to catch registry collisions early.

## Related Issues

- See also: [multi-pair-portfolio-arbitrage-unified-margin-support.md](../architecture-patterns/multi-pair-portfolio-arbitrage-unified-margin-support.md)
- Similar to: [hardening-python-connector-20260130.md](../security-issues/hardening-python-connector-20260130.md)
