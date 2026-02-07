---
title: "Mock State Persistence Failure in GridEngine Tests"
date: 2026-02-07
status: resolved
severity: high
category: test-failures
tags: [unit-tests, mocks, go, state-verification, grid-engine]
related_issues: []
---

# Mock State Persistence Failure in GridEngine Tests

## Problem Statement

The `GridEngine` unit tests were reporting success (green), but the underlying state changes were not being verified correctly. Specifically, the `MockPositionManager` was silently ignoring state updates, leading to a false sense of security where broken logic could pass the tests.

### Symptoms
- Unit tests pass consistently even when logic is intentionally broken.
- `MockPositionManager` receives calls but does not retain state changes.
- Subsequent assertions on position state reflect the initial state, not the updated state.
- Debug logs show `ApplyActionResults` being called, but `GetPosition` returns old data.

## Investigation & Findings

### Root Cause Analysis
The `MockPositionManager` was implemented as a stateless mock or with incomplete state tracking. When `ApplyActionResults` was called by the `GridEngine`, the mock would acknowledge the call (returning no error) but would not update its internal representation of the position. Consequently, when the test later queried the state, it received the stale, unmodified data.

This is a common issue when mocks are generated or implemented only to satisfy interface requirements ("happy path" returns `nil`) rather than simulating behavior.

## Solution

The solution involved implementing a functional in-memory state tracker within the `MockPositionManager`.

### Implementation Details
Updated `ApplyActionResults` to actively mutate the internal state map of the mock.

```go
// MockPositionManager implementation

func (m *MockPositionManager) ApplyActionResults(ctx context.Context, results []ActionResult) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    for _, res := range results {
        // Update the internal in-memory state
        if current, exists := m.positions[res.Symbol]; exists {
            current.Quantity += res.Quantity
            current.AveragePrice = calculateNewAvg(current, res) // Simplified logic
            m.positions[res.Symbol] = current
        } else {
            m.positions[res.Symbol] = &Position{
                Symbol: res.Symbol,
                Quantity: res.Quantity,
                AveragePrice: res.Price,
            }
        }
    }
    return nil
}
```

By making the mock stateful, the unit tests can now legitimately assert that:
1. The engine calculated the correct action.
2. The action was applied to the position manager.
3. The final state matches expectations.

## Prevention & Best Practices

### 1. Verify State Transitions
Don't just verify that methods were called (e.g., `AssertCalled`). Verify the *result* of the call by checking state changes.

### 2. Use Fakes over Mocks for State
For complex stateful logic, prefer using a "Fake" (a lightweight, working implementation) rather than a "Mock" (record/replay expectations). Fakes are more robust to implementation details and better verify system behavior.

### 3. Mutational Testing
Occasionally introduce bugs (e.g., comment out the update line) to ensure tests fail as expected.
