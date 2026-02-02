---
status: completed
priority: p1
issue_id: "023"
tags: [concurrency, critical, code-review]
dependencies: []
---

# Problem Statement
The `ArbitrageEngine` currently holds the main mutex `e.mu` while performing network I/O operations inside `OnFundingUpdate` and `OnAccountUpdate` handlers. This causes the entire engine to freeze whenever a network delay or timeout occurs during these updates.

# Findings
- `ArbitrageEngine.OnFundingUpdate` locks `e.mu` and then makes blocking network calls.
- `ArbitrageEngine.OnAccountUpdate` locks `e.mu` and then makes blocking network calls.
- This prevents other critical engine functions (like order placement or price updates) from executing while waiting for I/O.

# Proposed Solutions
1. **Fine-grained Locking**: Only hold the mutex while updating internal state, and release it before making network calls.
2. **Asynchronous Updates**: Use a worker pool or goroutines to handle the I/O-heavy parts of the updates without blocking the main engine loop.

# Recommended Action
Implement fine-grained locking to ensure `e.mu` is not held during blocking I/O operations.

# Acceptance Criteria
- [x] Mutex `e.mu` is released before any network I/O in `OnFundingUpdate`.
- [x] Mutex `e.mu` is released before any network I/O in `OnAccountUpdate`.
- [x] Engine remains responsive during simulated network latency in these handlers.

# Work Log
### 2026-02-01 - Todo Created
**By:** opencode
**Actions:** Created todo to address critical blocking mutex issue during I/O.
