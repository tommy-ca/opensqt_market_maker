# Grid Workflow Audit Specification (Phase 26)

## 1. Objective
Validate the grid trading workflow end-to-end: strategy decisions, slot/state management, idempotent order execution, risk handling, and persistence/recovery.

## 2. Scope
- Code: `market_maker/internal/trading/{strategy,grid,position,order}` and durable workflows (`internal/engine/durable/workflow.go`).
- Persistence: `internal/engine/simple/store_sqlite.go` (WAL + checksum) and DBOS steps for durable runs.
- Interfaces: `api/proto/opensqt/market_maker/v1/{types,resources,events,state,exchange}.proto`.

## 3. Invariants (map to REQ-GRID)
- Slot lifecycle: FREE → LOCKED → (FILLED | CANCELED) → FREE; no orphan LOCKED after recovery.
- Risk trigger: stops opening BUYs and cancels active BUY orders; SELL rules follow strategy mode.
- Idempotency: every placed order carries `client_order_id`; duplicate submissions with same client ID must not create multiple orders.
- Routing: order updates (by order_id or client_order_id) route to the correct slot after crash/restore.
- Precision: price/qty rounding honors configured decimals; no drift across grid levels.

## 4. Test Matrix (TDD-first)
### 4.1 Offline (must-run in CI)
- CrashRecovery: lock slots, crash, reload SQLite, verify locked slots restored and mappings intact.
- RiskProtection: simulate spike → risk trigger → BUYs canceled, no new BUYs on next price update.
- TradingFlow: normal placement/fill cycle validates slot transitions.
- Idempotency: PlaceOrder twice with same `client_order_id` → single open order; GetOrder by client ID resolves the same order.

### 4.2 Integration (env/tag gated)
- FullStackStartup / gRPC loopback / health.
- Connector restart recovery (streams reconnect, no missed updates).
- Arbitrage lifecycle (if enabled) and DBOS workflow atomicity.

## 5. Execution Steps
1) Specs-first: update this spec and `plan.md` before code.
2) RED: add/extend tests per matrix (offline first).
3) GREEN: minimal fixes in strategy/position/order/exchange layers.
4) REFACTOR: cleanup with tests green.
5) Gates: `cd market_maker && make audit && make test` (offline) + tagged integration when env ready.

## 6. Artifacts
- Test commands to document in runbooks:
  - Offline slices: `go test -race ./internal/...`, `go test -race ./pkg/...`, `go test -count=1 -run 'TestE2E_(CrashRecovery|RiskProtection|TradingFlow)' ./tests/e2e`
  - Integration (tag/env): `go test -tags=integration ./tests/e2e` (or env-gated), once services are up.

## 7. Latest Offline Test Run (Jan 28, 2026)
- `go test -race ./internal/...` ✅
- `go test -race ./pkg/...` ✅
- `go test -count=1 -run 'TestE2E_(CrashRecovery|RiskProtection|TradingFlow)' ./tests/e2e` ✅

## 8. TDD Backlog (next RED tests)
- Idempotent placement: same `client_order_id` does not create duplicate orders; GetOrder resolves by client ID.
- Risk trigger enforcement: after trigger, BUYs are canceled and no new BUYs open on next price update; SELL follows mode.
- Restore routing: after crash/restart, order/client maps route updates to correct slots (no orphan LOCKED slots).

## 9. Latest TDD Progress (Jan 28, 2026)
- Added idempotent client_order_id behavior to `MockExchange` (returns existing order on duplicate client ID) and unit test `TestMockExchange_IdempotentClientOrderID` ✅.
- Added risk-trigger cancellation assertion in `grid.Strategy` (`TestStrategy_RiskTriggeredCancelsLockedBuys`) ✅.
- Added restore-routing check for order/client maps in `SuperPositionManager` (`TestSuperPositionManager_RestoreState_RebuildsOrderMaps`) ✅.
- Next: extend idempotency coverage to adapter/exchange GetOrder by client ID (integration-facing) and consider an offline E2E to assert no duplicate placements.
