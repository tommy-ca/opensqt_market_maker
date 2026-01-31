---
status: complete
priority: p1
issue_id: "015"
tags: [security, architecture, python, code-review]
dependencies: []
---

# Problem Statement
The `handle_ccxt_exception` decorator currently only binds the gRPC context if it's passed as the last positional argument (`args[-1]`). This breaks if the context is passed as a keyword argument or if it's not the last argument in the signature.

# Findings
- `handle_ccxt_exception` in `python-connector/src/connector/errors.py` uses `_get_grpc_context`.
- Many gRPC methods are called with keyword arguments or have signatures where `context` is not at the end.

# Proposed Solutions
1. **Robust Inspection**: Use `inspect.signature` to find the `context` argument regardless of its position or whether it was passed as a positional or keyword argument.
2. **Standardize Signatures**: Enforce a strict signature for all decorated methods, which is less flexible and harder to maintain.

# Recommended Action
TBD during triage. Robust inspection-based binding is the preferred solution.

# Acceptance Criteria
- [ ] `handle_ccxt_exception` correctly identifies and binds the gRPC context in all call patterns (positional, keyword, mixed).
- [ ] Unit tests cover various signature patterns to ensure context is always found if present.
- [ ] Decorator handles cases where no context is present (e.g., internal calls) gracefully.

# Work Log
### 2026-01-30 - Todo Created
**By:** Antigravity
**Actions:** Created todo to fix context binding in the CCXT exception decorator.

### 2026-01-30 - Task Implemented
**By:** Antigravity
**Actions:** Task was implemented and verified with tests.
