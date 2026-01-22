# Proto Review and Audit

## Status
- **Date**: 2026-01-20
- **Scope**: `market_maker/api/proto/v1/exchange.proto`, `market_maker/api/proto/v1/models.proto`
- **Result**: Approved with notes.

## Findings

### 1. Consistency
- The gRPC service definition in `exchange.proto` correctly maps to the Go interface `core.IExchange`.
- Method signatures match the legacy parity requirements (e.g., `BatchPlaceOrders`, `CancelAllOrders`).

### 2. Data Types
- **Decimals**: All monetary values (Price, Quantity, Balance) use `google.type.Decimal`, which maps to `shopspring/decimal` in Go. This ensures precision for financial calculations.
- **Timestamps**: Uses `google.protobuf.Timestamp` for standard time representation.
- **Order IDs**: Uses `int64`.
    - **Note**: Some exchanges (e.g., Bybit) use string/UUIDs for Order IDs. The current implementation parses these into `int64`. If parsing fails, it falls back to 0 or relies on `ClientOrderID`.
    - **Mitigation**: The system relies heavily on `ClientOrderID` (string) for tracking, which is consistent and robust.

### 3. Streaming
- Streaming RPCs (`SubscribePrice`, `SubscribeOrders`) are correctly defined with `stream` keywords.
- Response types (`PriceChange`, `OrderUpdate`) contain necessary fields for the trading engine.

### 4. Parity Verification
- `GetSymbolInfo`: Implemented in proto, replacing legacy discrete getters.
- `BatchPlaceOrders`: Returns `all_success` boolean, matching legacy behavior.

## Recommendations
- No immediate changes required for Phase 1/2.
- Future consideration: Change `order_id` to `string` in a future major version if exchange support requires non-numeric IDs that cannot be mapped.
