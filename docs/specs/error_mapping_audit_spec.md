# Error Mapping Audit Specification (Phase 24.2)

## 1. Objective
Standardize error handling in the Python gRPC connector to ensure parity with the Go `apperrors` mapping. This allows the trading engine to handle exchange errors deterministically regardless of which connector language is used.

## 2. Requirements

### 2.1 Standardized Error Codes (REQ-ERR-001)
The Python connector MUST map CCXT exceptions to the following gRPC status codes:

| System Error | CCXT Exception | gRPC Status Code |
|--------------|----------------|------------------|
| Insufficient Funds | `InsufficientFunds` | `RESOURCE_EXHAUSTED` |
| Order Not Found | `OrderNotFound` | `NOT_FOUND` |
| Invalid Parameter | `InvalidOrder`, `BadRequest` | `INVALID_ARGUMENT` |
| Authentication Failed | `AuthenticationError` | `UNAUTHENTICATED` |
| Rate Limit Exceeded | `RateLimitExceeded` | `RESOURCE_EXHAUSTED` |
| Network / Unavailable | `NetworkError`, `ExchangeNotAvailable` | `UNAVAILABLE` |
| Order Rejected | `ExchangeError` (generic) | `FAILED_PRECONDITION` |
| Duplicate Order | `DuplicateOrderId` | `ALREADY_EXISTS` |

### 2.2 Error Metadata (REQ-ERR-002)
- Error messages MUST preserve the original exchange error message for debugging.
- The `details` field of the gRPC status SHOULD be used for complex error payloads if necessary.

## 3. Implementation Procedure (TDD Flow)

### 3.1 RED Phase
1.  Create `python-connector/tests/test_error_mapping.py`.
2.  Mock CCXT exceptions and verify that current implementation returns `UNKNOWN` or crashes.

### 3.2 GREEN Phase
1.  Implement `src/connector/errors.py` with a `handle_ccxt_exception` decorator or utility.
2.  Apply the error handler to all `ExchangeService` methods in `src/connector/binance.py`.
3.  Verify tests pass.

### 3.3 REFACTOR Phase
1.  Unify error handling logic across all RPC methods.
2.  Ensure logging captures the original traceback.
