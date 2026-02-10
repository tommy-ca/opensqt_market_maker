---
status: pending
priority: p2
issue_id: "032"
tags: [performance, quality, exchange]
dependencies: ["031"]
---

# PR #15 Quality & Performance Enhancements

## Problem Statement
Various performance bottlenecks and quality gaps identified in the Grid Trading workflow hardening that impact scalability and maintainability.

## Findings
1.  **🟡 Inefficient Hot Path**: `OnPriceUpdate` performs O(N) sequential slot locking and conversion on every tick.
2.  **🟡 Binance Futures Sync Gap**: `GetOpenOrders` is a stub for Binance Futures, which will break reconciliation on boot for those accounts.
3.  **🟡 Swallowed Errors**: Critical errors from `json.Unmarshal` and stream `Send` calls are ignored with `_ =`.
4.  **🟡 Persistence Throttling Bypass**: State is saved on every action, potentially causing I/O saturation in volatile markets.
5.  **🔵 Secret Redaction Gap**: `Secret` type lacks `GoString()`, risking raw leaks in `%#v` logs.
6.  **🔵 Test Flakiness**: `regime_test.go` uses `time.Sleep` instead of `Eventually`.

## Proposed Solutions
1.  **Optimize Hot Path**: Cache decimal values in slots; use pre-allocated slices for strategy input.
2.  **Implement Binance Sync**: Implement the actual `GetOpenOrders` API call for Binance Futures.
3.  **Audit Swallowed Errors**: Replace `_ =` with explicit logging or error propagation.
4.  **Harden Persistence**: Implement a cooldown for activity-based saves (e.g., save at most every 500ms).
5.  **Redaction**: Implement `func (s Secret) GoString() string { return "[REDACTED]" }`.
6.  **Fix Tests**: Use `assert.Eventually` in `regime_test.go`.

## Recommended Action
Implement P2/P3 improvements after P1 bugs are resolved.

## Acceptance Criteria
- [ ] No `_ =` on critical networking or unmarshaling calls.
- [ ] Binance Futures boot sync verified with logs.
- [ ] O(1) order lookups confirmed in `OnOrderUpdate`.

## Work Log
- 2026-02-09: Findings consolidated.
