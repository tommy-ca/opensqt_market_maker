---
status: completed
priority: p3
issue_id: "005"
tags: [python, grpc, ccxt]
dependencies: []
---

# Robust gRPC Decorator

## Problem Statement
The current `handle_ccxt_exception` decorator in Python finds the gRPC context by simply looking at `args[-1]`. This is fragile as it assumes the context is always the last argument, which may not hold true if method signatures change or other decorators are used.

## Findings
- Current implementation: `context = args[-1]` in `handle_ccxt_exception`.
- gRPC method signatures usually follow `(self, request, context)`, but decorators can wrap these in ways that shift arguments.

## Proposed Solutions
1. **Type Checking**: Iterate through `args` and check `isinstance(arg, grpc.ServicerContext)`.
2. **Named Argument**: Check `kwargs` for `context`.
3. **Combined Approach**: Check `kwargs['context']` first, then scan `args` for an object that looks like a gRPC context.

## Recommended Action
Implement a helper function `_get_context` that checks `kwargs` and then scans `args` for a gRPC context using both `isinstance` and duck typing (checking for `.abort`).

## Acceptance Criteria
- [x] `handle_ccxt_exception` safely identifies the gRPC context.
- [x] Decorator works even if context is not the last argument.
- [x] Unit tests verify context detection with various signatures.

## Work Log
### 2026-01-29 - Initial Creation
**By:** Antigravity
**Actions:** Created todo for robust gRPC decorator.

### 2026-01-29 - Implementation
**By:** Antigravity (pr-comment-resolver)
**Actions:**
- Refactored `handle_ccxt_exception` to use `_get_context` helper.
- Implemented robust context detection in `_get_context`.
- Added unit tests in `python-connector/tests/test_errors_robustness.py`.
- Verified fix with new tests.
