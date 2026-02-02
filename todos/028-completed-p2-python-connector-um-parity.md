---
status: completed
priority: p2
issue_id: "028"
tags: [parity, code-review]
dependencies: []
---

# Problem Statement
The Python connector lacks support for Unified Margin (UM) features, such as health_score, which are essential for risk management in UM accounts.

# Findings
The current Python connector implementation does not expose or handle Unified Margin specific metrics. This creates a feature gap compared to other connectors or the core engine capabilities.

# Proposed Solutions
- Extend the Python connector to support Unified Margin API endpoints.
- Add fields for health_score and other UM metrics to the Python data models.

# Recommended Action
Implement UM parity in the Python connector by adding support for health_score and related risk metrics.

# Acceptance Criteria
- [x] Python connector exposes health_score for UM accounts.
- [x] UM-specific data models are implemented and tested.
- [x] Parity achieved with core engine UM support.

# Work Log
### 2026-02-01 - Initial Todo Creation
**By:** opencode
**Actions:**
- Created todo for addressing Python connector UM parity.
