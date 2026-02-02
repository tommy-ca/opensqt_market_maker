---
status: completed
priority: p1
issue_id: "004"
tags: [sql, persistence, portfolio]
dependencies: []
---

# Problem Statement
Numerical precision in the database is currently insufficient for financial data, and there is a gap in persistence for portfolio intents. Specifically, `target_notional` and `quality_score` in the `symbol_registry` table need to be `NUMERIC` to avoid rounding issues. Additionally, `PortfolioController` must persist its intents.

# Findings
- `symbol_registry` used `DOUBLE PRECISION` for `target_notional` and `quality_score`.
- `PortfolioController` calculated intents but they were not being persisted via `OrchestratorWorkflows`.

# Proposed Solutions
1. **Schema Migration**: Changed the column types to `NUMERIC(32, 16)` in the database schema.
2. **Persistence Implementation**: Updated `PortfolioController` to call `AddTradingPair` and `RemoveTradingPair` on the orchestrator to save intended position states.

# Recommended Action
Completed.

# Acceptance Criteria
- [x] Database columns `target_notional` and `quality_score` are changed to `NUMERIC`.
- [x] `PortfolioController` successfully persists intents to the database.
- [x] Data integrity verified after migration.

# Work Log
### 2026-01-29 - Todo Created
**By:** Antigravity
**Actions:** Initialized todo based on critical finding.

### 2026-01-30 - Completed
**By:** Antigravity
**Actions:** 
- Migrated `symbol_registry` schema to `NUMERIC(32, 16)` for precision.
- Updated `RegistryEntry` to use `decimal.Decimal`.
- Integrated `OrchestratorWorkflows` into `PortfolioController` via `IOrchestrator` interface.
- Verified persistence with unit tests.
