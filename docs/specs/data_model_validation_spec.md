# Data Model Validation Specification (Phase 24.4)

## 1. Objective
Ensure that all exchange connectors (Go and Python) correctly and consistently map exchange-specific data structures to the standardized Protobuf models defined in `api/proto/`.

## 2. Requirements

### 2.1 Precision & Types (REQ-MOD-001)
- **Decimal Consistency**: All prices, quantities, and balances MUST be parsed as high-precision decimals (using `shopspring/decimal` in Go and `decimal.Decimal` in Python) BEFORE being converted to the Protobuf `google.type.Decimal` (string-based) format.
- **Timestamp Normalization**: All exchange timestamps (milliseconds or seconds) MUST be normalized to `google.protobuf.Timestamp`.

### 2.2 Field Coverage (REQ-MOD-002)
- **Required Fields**: Every mapped object (Order, Position, Balance) MUST populate all mandatory fields as defined in the Protobuf schema.
- **Empty vs Null**: String fields SHOULD be empty strings rather than null/undefined if possible.

### 2.3 Enumeration Mapping (REQ-MOD-003)
- **Side/Status/Type**: Every connector MUST implement a robust mapping from exchange-specific strings (e.g., "FILLED", "PARTIALLY_FILLED", "CANCELED") to the centralized Protobuf Enums.
- **Unmapped Values**: Any unknown or unmapped value MUST result in an error or be mapped to the `_UNSPECIFIED` enum member and logged.

## 3. Audit Procedure (TDD Flow)

### 3.1 RED Phase
1.  Identify a connector and a specific data model (e.g., Binance Go `Position` mapping).
2.  Write a unit test with a sample raw JSON response from the exchange.
3.  Assert that the mapped Protobuf object matches the expected values exactly.
4.  Test will fail if mapping logic is missing, imprecise, or incorrect.

### 3.2 GREEN Phase
1.  Implement or fix the mapping logic in the connector.
2.  Ensure use of `pbu` helpers (Go) or equivalent (Python).
3.  Verify the test passes.

### 3.3 REFACTOR Phase
1.  Deduplicate mapping code.
2.  Centralize common mapping logic into shared packages (e.g., `pkg/pbu` for Go).
