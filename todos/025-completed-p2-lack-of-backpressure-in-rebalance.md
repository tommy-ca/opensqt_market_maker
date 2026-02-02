---
status: completed
priority: p2
issue_id: "025"
tags: [performance, risk, code-review]
dependencies: []
---

# Problem Statement
PortfolioController.Rebalance lacks concurrency limits, potentially leading to rate limiting from exchanges or DB exhaustion under heavy load.

# Findings
The Rebalance function triggers multiple concurrent operations without a mechanism to throttle or limit the number of simultaneous requests. This can overwhelm both internal systems (DB) and external APIs (exchanges).

# Proposed Solutions
- Implement a worker pool or semaphore to limit the number of concurrent rebalance operations.
- Introduce backpressure mechanisms to handle load gracefully.

# Recommended Action
Triage to determine appropriate concurrency limits and implement a semaphore-based control in PortfolioController.

# Acceptance Criteria
- [x] Concurrency limits implemented in PortfolioController.Rebalance.
- [x] System handles high-frequency rebalance triggers without hitting rate limits.
- [x] Database connections are managed efficiently under load.

# Work Log
### 2026-02-01 - Initial Todo Creation
**By:** opencode
**Actions:**
- Created todo for addressing lack of backpressure in rebalance.
