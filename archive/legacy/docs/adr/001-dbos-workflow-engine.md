# ADR 001: Durable Workflow Engine with DBOS

## Status
Proposed

## Context
The current `SimpleEngine` implementation manages system durability by manually snapshotting the `PositionManager` state and persisting it to a SQLite database after every price or order update. While functional, this approach has several limitations:
1.  **Complexity**: State snapshotting and restoration logic must be manually maintained.
2.  **Atomicity**: Side effects (like placing an order on an exchange) and state updates are not transactionally guaranteed together.
3.  **Scalability**: Frequent full-state snapshots may become a performance bottleneck.

We need a more robust, durable, and transactional way to handle complex trading workflows.

## Proposed Change
Introduce [DBOS Transact for Go](https://github.com/dbos-inc/dbos-transact-golang) to build durable trading workflows. DBOS provides:
-   **Durable Execution**: Guaranteed completion of workflows even across system crashes.
-   **Transactional State**: Seamlessly integrates side effects and state updates in a single transaction.
-   **Automatic Persistence**: Replaces the manual SQLite snapshotting logic.

## Design
-   The `SimpleEngine` will be replaced by a DBOS-based orchestrator.
-   `AdjustOrders` and `OnOrderUpdate` will become DBOS workflows.
-   The state of the `PositionManager` will be stored in a DBOS-managed Postgres or SQLite database.

## Consequences
-   **Pros**:
    -   Simplified durability logic.
    -   Transactional guarantees for order placement and state updates.
    -   Built-in observability and replayability.
-   **Cons**:
    -   Introduction of a new framework and potentially Postgres as a dependency.
    -   Overhead of transactional execution may impact ultra-low latency requirements.
    -   Learning curve for the development team.

## Alternatives Considered
-   **Temporal**: Powerful but requires a separate server and has higher complexity.
-   **Custom Event Sourcing**: High implementation cost and complexity.
-   **Current SQLite Snapshots**: Lacks transactional guarantees across side effects.
