---
status: completed
priority: p1
issue_id: "002"
tags: [go, binance, okx, trade]
dependencies: []
---

# Problem Statement
The system lacks batch order execution in Go, which prevents atomic de-leveraging and reduces efficiency when managing multiple positions. Currently, orders are likely processed sequentially or in parallel but not as a single exchange-level batch where supported.

# Findings
- Binance supports `/papi/v1/batchOrders`.
- OKX supports `/api/v5/trade/batch-orders`.
- Atomic batch operations are critical for de-leveraging during high-risk events to ensure all components of a hedge or exit are executed together.

# Proposed Solutions
1. **Implement Batch Execution in Go Connectors**: Add support for the respective batch endpoints in the Go-based exchange connectors.
2. **Update Service Interface**: Ensure the `ExchangeService` or equivalent in Go supports `BatchPlaceOrders` and `BatchCancelOrders`.

# Recommended Action
Resolved in implementation.

# Acceptance Criteria
- [x] Binance connector implements `/papi/v1/batchOrders`.
- [x] OKX connector implements `/api/v5/trade/batch-orders`.
- [x] Unit/Integration tests verify batch order success and partial failure handling.

# Work Log
### 2026-01-29 - Todo Created
**By:** Antigravity
**Actions:** Initialized todo based on critical finding.

### 2026-01-30 - Implementation Verified and Refined
**By:** Antigravity (pr-comment-resolver)
**Actions:**
- Verified `BatchPlaceOrders` and `BatchCancelOrders` in Binance and OKX adapters.
- Refined OKX `BatchCancelOrders` with chunking (limit 20).
- Fixed Binance PAPI `BatchCancelOrders` parameter name (`orderIds`).
- Implemented `BatchPlaceOrders` fallback and `BatchCancelOrders` natively for Bybit.
- Verified all tests pass.
