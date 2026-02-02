# Funding & Arbitrage Audit Specification (Phase 27)

## 1. Objective
Lock down funding data semantics, restore end-to-end funding streams/RPCs, and make arbitrage workflows idempotent, safe on retry, and observable.

## 2. Scope
- Protos: `api/proto/opensqt/market_maker/v1/{resources,events,exchange}.proto`
- Connectors: funding rate fetch/stream for Binance/Bybit/OKX/Bitget/Gate and spot.
- Server/Client: `ExchangeServer` funding RPCs/streams; `RemoteExchange` callers.
- Monitor: `internal/trading/monitor/funding_monitor.go` (symbols, staleness, subscribers).
- Arbitrage: `arbitrage/selector.go`, `arbitrage/strategy.go`, `arbengine` (in-memory + DBOS), `arbitrage_workflow.go`.

## 3. Invariants (map to REQ-FUND / REQ-ARB / REQ-METRICS)
- Funding timestamps are ms (UTC); `0` means not applicable (spot) or unknown.
- Spot funding: `rate=0`, `next_funding_time=0`, non-zero `timestamp` (connector receipt).
- Predicted rate: optional; unset if unavailable; same units as `rate`.
- Signal uses spread (short – long) and real interval (from `next_funding_time` delta or configured fallback) to compute APR.
- Idempotency: deterministic `client_order_id` per workflow step; duplicate triggers/workflow replays do not create duplicate legs.
- Single in-flight entry/exit per symbol; overlapping workflows are rejected/deduped.
- Partial fills drive opposite leg sizing and compensations from executed quantities.
- Metrics: staleness/lag for funding; spread/APR gauges; retries/slippage; exposure/margin/liquidation distance.
- Circuit-breakers: stale feeds, high retries, unsafe margin/liquidation distance pause entry/force exit.

## 4. Test Matrix (TDD-first)
### 4.1 Offline (must-run in CI)
- Proto/unit: ms timestamp contract; spot sentinel (`rate=0`, `next_funding_time=0`); predicted_rate optional.
- Connector unit: Binance futures REST/WS mapping preserves exchange ms timestamps; spot returns 0; stubs replaced for Bybit/OKX/Bitget/Gate (happy-path unit per venue).
- FundingMonitor unit: configured symbols, no hardcoded BTCUSDT; staleness TTL returns error; Subscribe(exchange,symbol) filters updates.
- Spread calc unit: sign matrix (long pays/short receives), APR from interval (8h or derived from next_funding_time delta).
- Workflow/DBOS unit: deterministic client_order_id; replay does not duplicate orders; partial fill drives second leg/unwind sizing.

### 4.2 Integration (env/tag gated)
- ExchangeServer funding RPCs/streams work end-to-end with RemoteExchange.
- Arbitrage lifecycle (entry/exit), connector restart recovery, DBOS replay safety (no double-entry/exit).
- Cross-venue spread ingestion (both legs streaming) and decision based on spread APR.

## 5. Execution Steps
1) Specs-first: update protos/comments and docs (plan/requirements/design) before code.
2) RED: add unit tests for funding mapping, spread calculator, monitor staleness/subscribe, workflow idempotency/partial-fill sizing.
3) GREEN: implement funding RPCs/streams, fix connectors, add idempotent client_order_id + in-flight guard, fix partial-fill sizing and compensations.
4) REFACTOR: clean up duplication and hardcoded symbols; ensure metrics labels remain low-cardinality.
5) Gates: `cd market_maker && make audit` and offline test suite; integration suite behind env/tag.

## 8. Pending Actions (to unblock GREEN)
- Regenerate protos after FundingRate/FundingUpdate comment updates.
- Add RED tests:
  - Funding mapping per venue (Binance futures/spot, Bybit/OKX/Bitget/Gate once implemented), asserting ms timestamps and spot sentinel.
  - Spread/APR calculator (both legs, interval-aware) and “missing leg blocks decision.”
  - FundingMonitor staleness TTL + Subscribe(exchange,symbol) filtering; no hardcoded symbols.
  - Workflow idempotency: deterministic client_order_id prevents duplicate orders on replay; single in-flight entry/exit per symbol.
  - Partial-fill sizing drives opposite leg/unwind quantities; compensation logic tested with partial fills.

## 6. Artifacts / Commands
- Offline: `go test -race ./internal/...`, `go test -race ./pkg/...`, targeted units for funding/arb once added.
- Integration (tag/env): `go test -tags=integration ./tests/e2e` (arbitrage/funding lifecycle, connector restart, DBOS replay).

## 7. Metrics & Risk (MVP)
- Metrics: feed staleness/lag per stream; funding rate/spread/APR gauges; exposure (spot/perp/net); margin ratio; liquidation distance; retries/failures (small reason enum); hedge slippage histograms with bounded labels.
- Circuit-breakers: stale feeds, high retries, unsafe margin/liquidation distance; actions = pause entry/quotes or force exit depending on strategy (documented in requirements/design).
