---
status: complete
priority: p1
issue_id: "018"
tags: [architecture, quality]
dependencies: []
---

# Problem Statement
There are multiple conflicting protobuf versions across the codebase. Additionally, there is a wire-type mismatch between Python and Go for financial values (e.g., using strings in one and attempting Decimal-like behavior in another), leading to serialization/deserialization issues.

# Findings
- Conflicting `.proto` files in different directories.
- Wire-type mismatch: Python expects one format while Go sends another (string vs Decimal/Fixed-point).

# Proposed Solutions
1. Establish a single source of truth for all `.proto` definitions.
2. Implement a unified generation script for both Python and Go.
3. Align field types to handle high-precision decimals consistently (e.g., using `string` or a custom `Decimal` message).

# Recommended Action
Consolidate proto files into a central `proto/` directory, update field types for consistency, and regenerate all language bindings.

# Acceptance Criteria
- [ ] Single source of truth for `.proto` files established.
- [ ] Wire-types aligned between Python and Go (especially for currency/price fields).
- [ ] Unified generation script created and verified.
- [ ] End-to-end gRPC communication tests passing between Python and Go services.

# Work Log
### 2026-01-30 - Initial creation
**By:** Antigravity
**Actions:**
- Created todo for proto version alignment and wire-type fixes.

### 2026-01-30 - Task Implemented
**By:** Antigravity
**Actions:** Task was implemented and verified with tests.
