# Slot Manager Technical Reference

## Overview
The Slot Manager is the component responsible for maintaining the state of the grid. It maps price levels ("Slots") to their current execution status and any associated exchange positions or orders.

## Data Models (Protobuf)

The core data structure is the `InventorySlot`, defined in `api/proto/opensqt/market_maker/v1/state.proto`.

### InventorySlot
| Field | Type | Description |
| :--- | :--- | :--- |
| `price` | `Decimal` | The defined price level for this grid slot. |
| `slot_status` | `SlotStatus` | The operational status: `FREE`, `PENDING`, `LOCKED`. |
| `position_status` | `PositionStatus`| Whether the slot has a filled position: `EMPTY`, `FILLED`. |
| `order_id` | `int64` | The active exchange order ID (if status is `LOCKED`). |
| `client_oid` | `string` | The deterministic client-side order ID. |
| `position_qty` | `Decimal` | The quantity currently held at this level. |

## State Machine Transitions

### 1. Level Initialization
When the grid starts, it creates slots within the defined windows.
- Status: `FREE`
- Position: `EMPTY`

### 2. Order Placement (Reconciliation)
The Reconciler identifies that a slot should be active and issues a `PLACE` action.
- Transition: `FREE` -> `LOCKED` (via `ApplyActionResults`)
- A deterministic `client_oid` is generated to ensure idempotency.

### 3. Order Execution (Update)
The exchange sends an execution report (`OnOrderUpdate`).
- **If Filled**:
    - Transition: `LOCKED` -> `FREE`
    - Position: `EMPTY` -> `FILLED`
    - `position_qty` is updated to the executed quantity.
- **If Canceled**:
    - Transition: `LOCKED` -> `FREE`
    - Position remains `EMPTY`.

### 4. Position Closing
When the price moves and the strategy decides to take profit or cut loss.
- A `PLACE` action (ReduceOnly) is issued for the filled position.
- Transition: `FREE` -> `LOCKED`.
- When the closing order fills, Position moves to `EMPTY`.

## Idempotency and Recovery

### Deterministic IDs
Client Order IDs are derived from the slot price and side. This ensures that even if the process restarts during order placement, the same ID is used, allowing the exchange to reject duplicates and allowing the system to re-bind to existing orders.

### Durable Workflows
The Durable Engine (DBOS) wraps slot updates in transactional steps. This ensures that the internal state (`SlotManager`) never drifts from the expected outcome of an exchange operation.

## Implementation Details
The Go implementation is located in `market_maker/internal/trading/grid/slot_manager.go`. It implements the `IPositionManager` interface found in `core/interfaces.go`.
