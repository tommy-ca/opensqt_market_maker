# Brainstorm: Fix Compliance Gaps & Config Errors

**Date:** 2026-02-03
**Topic:** Address Findings from Compliance Report 2026-02-03
**Status:** Approved

## 1. What We're Building
A targeted set of fixes to resolve compilation errors and security type misuses identified in the compliance check. We are specifically addressing:
1.  **Secret Type Safety**: Fixing type conversion errors in `config.go` (masking) and exchange connectors (signing/headers).
2.  **Execution Stability**: Formally accepting the current "Manual Loop" in `GridEngine` as the correct implementation for this phase, deferring `SmartExecutor` adoption.

## 2. Why This Approach?
*   **Compilation First**: The current codebase cannot build due to strict type checking on `Secret` vs `string`. These must be fixed immediately.
*   **Explicit Casting**: While annoying, explicit casting `string(apiKey)` forces the developer to acknowledge they are exposing a secret, which is the intended security design pattern.
*   **YAGNI on Executor**: Extending `SmartExecutor` to handle polymorphic `OrderAction` types (Place/Cancel) is a significant refactor. Since the GridEngine's manual loop works and supports concurrency, we will not over-engineer a shared executor at this moment.

## 3. Key Decisions
*   **Fix, Don't Refactor**: We will fix the type errors in place rather than changing the `Secret` type implementation.
*   **Config Redaction**: `Config.String()` must explicitly cast to string, apply the mask, and then cast back to `Secret` to satisfy the struct definition.
*   **Grid Engine**: We acknowledge `GridEngine` handles its own execution loop. We will update the compliance report to mark this as "Accepted Deviation" rather than a failure.

## 4. Open Questions
*   *None.* The path forward is strictly mechanical code fixes.

## 5. Next Steps
Run `/workflows:plan` to execute the fixes.
1.  Update `market_maker/internal/config/config.go` to fix `maskString` usage.
2.  Update `bybit.go`, `gate.go`, `binance_spot.go` to cast `Secret` to `string` for headers and signatures.
3.  Verify compilation with `go build ./...`.
