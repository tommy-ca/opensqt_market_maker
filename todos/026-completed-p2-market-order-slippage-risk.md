---
status: completed
priority: p2
issue_id: "026"
tags: [risk, performance, code-review]
dependencies: []
---

# Problem Statement
The use of MARKET orders for emergency or partial exits poses a significant slippage risk, especially in low-liquidity conditions.

# Findings
Current implementation uses MARKET orders for rapid exits. In volatile or thin markets, this can lead to execution at prices far from the current mark, resulting in unexpected losses.

# Proposed Solutions
- Replace MARKET orders with aggressive LIMIT orders (e.g., FOK or IOC with a price offset).
- Implement slippage protection checks before executing emergency exits.

# Recommended Action
Analyze exit strategies and transition from pure MARKET orders to safer execution methods with slippage bounds.

# Acceptance Criteria
- [x] MARKET orders replaced or supplemented with slippage-protected orders.
- [x] Slippage thresholds are configurable or dynamically calculated.
- [x] Emergency exits remain timely but become more predictable in cost.

# Work Log
### 2026-02-01 - Initial Todo Creation
**By:** opencode
**Actions:**
- Created todo for addressing market order slippage risk.
