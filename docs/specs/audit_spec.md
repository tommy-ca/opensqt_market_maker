# Code Quality & Static Audit Specification (Phase 25)

## 1. Objective
Ensure the `market_maker` codebase adheres to high quality standards, is free of common Go pitfalls (e.g., lock copying, unreachable code), and maintains a clean static analysis profile.

## 2. Scope
- **Directory**: `market_maker/` (Core engine and connectors).
- **Tools**: `go vet`, `make audit` (if available), `golangci-lint` (if configured).

## 3. Requirements

### 3.1 Zero Warnings (REQ-AUDIT-001)
- The goal is 100% clean output for `go vet ./...`.
- All `make audit` targets must pass.

### 3.4 Generated Code Safety (REQ-AUDIT-004)
- Protobuf-generated files MUST NOT be edited to satisfy naming lint.
- Hand-written Go code should prefer `orderID` / `clientOrderID` for acronyms (ST1003), while protobuf Go structs may expose `OrderId` / `ClientOrderId` depending on codegen.

### 3.5 Vulnerability Scan Reliability (REQ-AUDIT-005)
- `make audit` includes `govulncheck`.
- If `govulncheck` fails due to vuln DB fetch restrictions (e.g., HTTP 403 to `vuln.go.dev`), the local workflow may warn-but-not-fail.
- CI must run `govulncheck` with network access and fail on vulnerabilities.

### 3.2 Concurrency Safety (REQ-AUDIT-002)
- Special attention to "locks being copied" or "mutexes used incorrectly".
- Shadowed variables in loops or deferred calls.

### 3.3 Dead Code Elimination (REQ-AUDIT-003)
- Remove or fix unreachable code identified by static analysis.

## 4. Audit Procedure (TDD Flow)

### 4.1 RED Phase
1.  Run `go vet ./...` in `market_maker/`.
2.  Capture and document all errors/warnings.
3.  Verification: The audit fails if any warnings are present.

### 4.2 GREEN Phase
1.  Systematically address each warning.
2.  For logic-related fixes, write a unit test to verify the fix and prevent regression.
3.  Rerun audit tools to verify clean output.

### 4.3 REFACTOR Phase
1.  Identify patterns in warnings (e.g., repeated misuse of a certain library).
2.  Refactor code to improve maintainability beyond just silencing the warning.
