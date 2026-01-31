# Comprehensive Connector & Model Audit Checklist

This checklist tracks the systematic review of exchange connectors, Protobuf definitions, and trading workflows across Go and Python implementations.

## 1. Protobuf & Data Models (`api/proto/`)

- [ ] **Field Naming**: Protobuf fields use `snake_case` (e.g., `order_id`, `client_order_id`).
- [ ] **Generated Code**: Go codegen will typically expose `OrderId` / `ClientOrderId`; do not rename generated fields.
- [ ] **Hand-Written Go**: Use `orderID` / `clientOrderID` for locals and parameters (staticcheck ST1003).
- [ ] **Type Consistency**:
    - [ ] All price/quantity fields use `google.type.Decimal` or standardized string representation.
    - [ ] Timestamps use `google.protobuf.Timestamp`.
    - [ ] Enums (Side, Status) are centralized and used consistently.
- [ ] **Documentation**: Every message and RPC has a comment explaining its purpose and constraints.
- [ ] **Buf Linting**: Run `buf lint` and ensure 0 violations.
- [ ] **Breaking Changes**: Run `buf breaking --against .git#branch=main` to ensure backward compatibility.

## 2. Go Connectors (`market_maker/internal/exchange/`)

- [ ] **Implementation Parity**: All exchanges (Binance, Bitget, Gate, OKX, Bybit) implement the full `IExchange` interface.
- [ ] **Data Mapping**:
    - [ ] Verify `Shopspring/Decimal` conversion from raw exchange JSON.
    - [ ] Verify error code mapping to system-standard `apperrors`.
- [ ] **Streaming Robustness**:
    - [ ] WebSocket reconnection logic uses exponential backoff.
    - [ ] Multiplexing logic for multi-symbol streams is thread-safe.
- [ ] **Health Checks**: `CheckHealth()` correctly validates API credentials and connectivity.

## 2.1 Static Audit (`market_maker/`)

- [ ] `cd market_maker && make audit` passes (`go fmt`, `go vet`, `staticcheck`).
- [ ] `govulncheck` runs; if blocked locally (e.g., HTTP 403), record it and ensure CI runs it with network access.

## 3. Python Connectors (`python-connector/src/connector/`)

- [ ] **Logic Alignment**: Python logic for `PlaceOrder`, `CancelOrder`, etc., matches the Go implementation exactly.
- [ ] **CCXT Integration**:
    - [ ] Review how CCXT errors are caught and translated to Protobuf errors.
    - [ ] Verify CCXT unified API usage vs. exchange-specific methods.
- [ ] **Async Performance**: Ensure `asyncio` is used correctly without blocking calls.
- [ ] **Type Hints**: All connector methods have complete Python type hints for static analysis.

## 4. Workflows & Durability

- [ ] **DBOS Compatibility**: Ensure Go connectors return deterministic errors that allow DBOS workflows to decide on retry vs. fail.
- [ ] **Side-Effect Atomicity**: Verify that partial fills or multi-step orders don't leave the system in an inconsistent state if a crash occurs between steps.
- [ ] **Recovery Testing**: Manually trigger process crashes during a connector call and verify DBOS resumes correctly.

## 5. Parity Verification

- [ ] **Cross-Language Tests**: Run the same integration test suite against both a Go connector and the Python connector (via gRPC) to ensure identical results.
- [ ] **Decimal Precision**: Verify that `0.1 + 0.2` results in exactly `0.3` across the entire gRPC/Model chain.
