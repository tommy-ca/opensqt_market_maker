# Protocol Audit Specification (Phase 24.1)

## 1. Objective
Ensure that the gRPC protocol definitions (`.proto` files) are consistent, idiomatic, and correctly support both Go and Python implementations with high precision.

## 2. Requirements

### 2.1 Field Casing & Naming (REQ-PROTO-001)
- **Casing**: All field names MUST follow the Protobuf style guide (snake_case).
- **Go Namespacing**: Generated Go code MUST use `PascalCase` via appropriate options if necessary (standard behavior).
- **Consistency**: Fields representing the same data (e.g., Order ID) MUST use the exact same name across all messages (`order_id`).

### 2.2 Numerical Precision (REQ-PROTO-002)
- **Decimal Fields**: All prices, quantities, and balances MUST use `google.type.Decimal` (if available) or `string` to avoid floating-point inaccuracies.
- **Float/Double Prohibition**: The use of `float` or `double` for monetary values is STRICTLY PROHIBITED.

### 2.3 Enumerations (REQ-PROTO-003)
- **Standardized Enums**: `Side` (BUY/SELL), `OrderStatus` (NEW, FILLED, CANCELED, etc.), and `OrderType` (LIMIT, MARKET, POST_ONLY) MUST be centralized.
- **Unspecified Member**: Every enum MUST have an `_UNSPECIFIED = 0` member to handle default values correctly.

### 2.4 Metadata & Headers (REQ-PROTO-004)
- **Request Metadata**: All mutation RPCs (PlaceOrder, CancelOrder) SHOULD support a `client_order_id` for idempotency.
- **Timestamps**: All temporal data MUST use `google.protobuf.Timestamp`.

## 3. Audit Procedure (TDD Flow)

### 3.1 RED Phase
1.  Run `buf lint` on `market_maker/api/proto/`.
2.  Inspect `exchange.proto` and `models.proto` for `double/float` usage.
3.  Verify Go/Python generation doesn't produce conflicting types.

### 3.2 GREEN Phase
1.  Fix naming violations.
2.  Migrate `double` fields to `string` or `google.type.Decimal`.
3.  Centralize enums.

### 3.3 REFACTOR Phase
1.  Clean up comments.
2.  Optimize message nesting.
