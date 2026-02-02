# Durable Workflow Refactor Specification (Phase 24.3)

## 1. Objective
Transition `DBOSGridEngine` from a goroutine-based execution model to full DBOS durable workflows. This ensures that every order placement or cancellation is tracked and resumed correctly even if the process crashes mid-execution.

## 2. Requirements

### 2.1 Atomic Side-Effects (REQ-DUR-001)
- Every `OrderAction` produced by the strategy MUST be executed within a DBOS workflow.
- Order placement MUST be a `dbos.RunStep` call within the workflow.
- State updates (updating local slots after execution) MUST also be tracked durably.

### 2.2 Panic Recovery & Resumption (REQ-DUR-002)
- The orchestrator MUST handle workflow failures and retries according to DBOS policies.
- On startup, the engine MUST check for incomplete workflows and allow them to finish.

### 2.3 Concurrent Workflow Management (REQ-DUR-003)
- Multiple `OrderAction`s for different slots SHOULD be executable in parallel workflows if supported by DBOS, or sequenced correctly to avoid state conflicts.

## 3. Implementation Procedure (TDD Flow)

### 3.1 RED Phase
1.  Review existing `durable_test.go` (if any) or create one.
2.  Simulate a crash during `executor.PlaceOrder` and verify that current implementation LOSES the state update.

### 3.2 GREEN Phase
1.  Define `ExecuteActionWorkflow` in `gridengine/durable.go`.
2.  Register the workflow with the DBOS runtime.
3.  Update `execute()` to call `dbosCtx.RunWorkflow`.
4.  Verify that crashed workflows are resumed by DBOS (simulated in tests).

### 3.3 REFACTOR Phase
1.  Optimize state snapshotting to minimize database overhead.
2.  Unify error handling between `SimpleEngine` and `DBOSGridEngine`.
