---
status: completed
priority: p1
issue_id: "003"
tags: [python, precision, binance]
dependencies: []
---

# Problem Statement
The Python connector for Binance is using `float()` for quantity and price conversions, which can lead to numerical precision issues. Financial applications should use `decimal.Decimal` or strings to avoid floating-point errors.

# Findings
- `python-connector/src/connector/binance.py` (lines 96, 98, 144, 146) converts `decimal_pb2.Decimal` values to `float`.
- Floating point math can introduce tiny errors (e.g., 0.1 + 0.2 != 0.3) which cause order rejection or incorrect sizing on exchanges.

# Proposed Solutions
1. **Use `decimal.Decimal`**: Convert the incoming protobuf decimal strings directly to Python's `decimal.Decimal`.
2. **String Passing**: Pass values as strings to CCXT if it supports them (CCXT generally handles strings well for precision).

# Recommended Action
Refactor `BinanceConnector` to use `decimal.Decimal` for all internal calculations and ensure CCXT receives high-precision inputs.

# Acceptance Criteria
- [x] No `float()` conversions for price or quantity in `BinanceConnector`.
- [x] All decimal protobuf values are handled using `decimal.Decimal` or strings.
- [x] Tests confirm that values like "0.00000001" are preserved exactly.

# Work Log
### 2026-01-29 - Todo Created
**By:** Antigravity
**Actions:** Initialized todo based on critical finding.

### 2026-01-29 - Refactored to use Decimal
**By:** Antigravity (pr-comment-resolver)
**Actions:** Refactored `BinanceConnector` to use `Decimal` for all calculations and pass strings to CCXT. Added `python-connector/tests/test_precision.py`.
