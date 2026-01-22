# ADR 002: Decoupled Exchange Connectors with gRPC

## Status
Proposed

## Context
Currently, exchange adapters (Binance, Bitget, Gate.io) are implemented as internal modules within the modular monolith. They run in the same process as the orchestrator and position manager. This leads to:
1.  **Process Fate Sharing**: A crash or memory leak in one exchange adapter can take down the entire trading engine.
2.  **Scalability Limits**: All adapters must scale together with the main process.
3.  **Language Lock-in**: All adapters must be written in Go.

## Proposed Change
Decouple exchange connectors into separate external processes communicating with the main engine via gRPC and Protocol Buffers.

## Design
-   **Exchange Connector Service**: A standalone process responsible for one exchange and symbol.
-   **gRPC Interface**: Standardized `ExchangeService` proto defining methods for order placement, account queries, and streaming market data.
-   **Bidirectional Streaming**: Use gRPC streams for real-time price feeds and order updates.
-   **Engine Proxy**: An internal `RemoteExchange` adapter in the main engine that communicates with the external connector via gRPC.

## Consequences
-   **Pros**:
    -   **Isolation**: Faults in connectors do not affect the main engine.
    -   **Polyglot**: Connectors can be written in any language (e.g., Python for ML-heavy exchanges, C++ for ultra-low latency).
    -   **Independent Scaling**: Connectors for different exchanges can run on different hardware.
    -   **Simplified Updates**: Connectors can be restarted independently.
-   **Cons**:
    -   **Increased Latency**: Network overhead (even over loopback/IPC) for gRPC calls.
    -   **Operational Complexity**: More processes to manage, monitor, and deploy.
    -   **Serialization Overhead**: Cost of Protobuf encoding/decoding.

## Alternatives Considered
-   **Internal Modules (Current)**: Low latency but lack isolation.
-   **NATS/Message Bus**: Decoupled but adds complexity of a broker and lacks the strong typing/RPC semantics of gRPC.
