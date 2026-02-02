# Workflow Parity Specification (Phase 24.5)

## 1. Objective
Ensure that high-level trading workflows (retries, timeouts, idempotency) behave identically across Go (Native) and Python (Remote) exchange connectors. This prevents strategy divergence when switching between polyglot backends.

## 2. Requirements

### 2.1 Idempotent Order Placement (REQ-WF-001)
- **Client Order ID**: All `PlaceOrder` requests MUST use the `client_order_id` provided by the engine.
- **Duplicate Handling**: If an exchange returns a "Duplicate Order ID" error (mapped in Phase 24.2), the connector MUST treat this as a success if the order details match, or return a standardized `AlreadyExists` error to let the workflow decide.
- **Retry Safety**: Connectors MUST NOT retry `PlaceOrder` automatically if it's uncertain whether the order reached the exchange (to avoid double fills), UNLESS a `client_order_id` is used and the exchange supports idempotent placement.

### 2.2 Standardized Retry Policies (REQ-WF-002)
- **Transient Errors**: Network timeouts, 5xx errors, and "Rate Limit Exceeded" SHOULD be retried with exponential backoff.
- **Deterministic Failures**: "Insufficient Funds", "Invalid Parameters", and "Order Not Found" MUST NOT be retried.
- **Consistency**: Both Go and Python implementations MUST use the same initial backoff (e.g., 100ms) and max retries (e.g., 3).

### 2.3 Timeout Handling (REQ-WF-003)
- **RPC Timeouts**: Every connector call MUST respect the gRPC deadline.
- **Internal Timeouts**: Connectors MUST use internal timeouts for REST calls (e.g., 10s) that are shorter than or equal to the RPC deadline.

## 3. Audit Procedure (TDD Flow)

### 3.1 RED Phase
1.  Create a test case that simulates a network timeout during `PlaceOrder`.
2.  Verify that currently, some connectors might retry while others fail immediately.
3.  Test idempotency by sending the same `client_order_id` twice.

### 3.2 GREEN Phase
1.  Implement a unified retry decorator in Python (`src/connector/errors.py`).
2.  Update Go `RemoteExchange` or native adapters to use a consistent retry policy (e.g., using `failsafe-go`).
3.  Ensure `client_order_id` is always passed through to CCXT/Native APIs.

### 3.3 REFACTOR Phase
1.  Centralize retry configuration in `live_server.yaml` or `config.yaml`.
2.  Ensure telemetry (metrics/logs) records every retry attempt.
