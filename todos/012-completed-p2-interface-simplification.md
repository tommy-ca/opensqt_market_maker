---
status: completed
priority: p2
issue_id: "012"
tags: [refactoring, architecture, interfaces]
dependencies: []
---

# Problem Statement
The `IScanner` and `EngineFactory` interfaces overlap in some areas or are too granular, leading to high cognitive load for developers.

# Findings
- `IScanner` and `EngineFactory` are separate but tightly coupled.
- Developers find it difficult to understand the boundaries between these interfaces.

# Proposed Solutions
- Consolidate common functionality into a more unified interface.
- Simplify method signatures and remove redundant methods.

# Recommended Action
Consolidate `IScanner` and `EngineFactory` interfaces to reduce cognitive load.

# Acceptance Criteria
- [x] Simplified interface structure.
- [x] Reduced number of distinct interface methods.
- [x] Code using these interfaces is updated and functional.

# Work Log
### 2026-01-29 - Todo Created
**By:** Antigravity
**Actions:**
- Created initial todo for interface simplification.

### 2026-01-30 - Interfaces Simplified
**By:** Antigravity
**Actions:**
- Created `engine.EngineFactory` in `internal/engine/interfaces.go`.
- Created `portfolio.IEngineManager` in `internal/trading/portfolio/types.go` consolidating `Scan` and `CreateEngine`.
- Updated `PortfolioController` to use `IEngineManager`.
- Updated `Orchestrator` to use `engine.EngineFactory`.
- Removed redundant interface definitions.
- Verified with build and tests.
