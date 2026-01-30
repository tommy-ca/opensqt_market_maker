---
status: completed
priority: p1
issue_id: "001"
tags: [go, risk, margin]
dependencies: []
---

# Problem Statement
The current implementation of `SimulateImpact` in `market_maker/internal/trading/portfolio/marginsim.go` is a stub that merely returns the current health score. It does not perform actual margin requirement simulations for proposed position changes.

# Findings
- `SimulateImpact` in `market_maker/internal/trading/portfolio/marginsim.go` (line 92) is a placeholder.
- Real margin calculations require taking into account the Estimated Collateral Value (ECV) and Maintenance Margin Requirement (MMR).
- The system already has `prices` and `haircuts` maps, but they are not used for impact simulation.

# Proposed Solutions
1. **Full Margin Simulation**: Implement a logic that clones the current account state, applies the proposed position changes, and recalculates Adjusted Equity and Total Maintenance Margin using exchange-specific or unified formulas (haircuts, price-tiers).
2. **Delta-based Approximation**: Calculate the change in MMR and ECV based only on the deltas of the proposed positions. Faster but potentially less accurate if nonlinear tiers are involved.

# Recommended Action
TBD during triage. (Likely Solution 1 for maximum safety).

# Acceptance Criteria
- [x] `SimulateImpact` correctly calculates the projected Health Score after proposed changes.
- [x] Calculations use asset haircuts for ECV.
- [x] Calculations use maintenance margin rates/tiers for MMR.
- [x] Unit tests verify simulation accuracy against known scenarios.

# Work Log
### 2026-01-29 - Todo Created
**By:** Antigravity
**Actions:** Initialized todo based on critical finding.

### 2026-01-30 - Implementation Completed
**By:** Antigravity (pr-comment-resolver)
**Actions:**
- Added `mmrs` map and `SetMMR` to `MarginSim`.
- Implemented `SimulateImpact` using delta-based projection of TMM and AdjEq.
- Incorporated 15% safety buffer in health score calculation.
- Added comprehensive unit tests in `marginsim_test.go`.
- Fixed broken `controller_test.go`.

