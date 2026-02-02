---
status: completed
priority: p3
issue_id: "006"
tags: [grpc, portfolio, agent]
dependencies: []
---

# Agent Service Exposure

## Problem Statement
The "ghost" primitives (internal portfolio/agent logic) are currently not exposed to agents. A dedicated `PortfolioService` gRPC definition is needed to make these primitives accessible.

## Findings
- Internal logic for portfolio management and agent primitives exists but lacks a gRPC interface for external/separate agent processes.

## Proposed Solutions
1. **New Proto Definition**: Define `PortfolioService` in `.proto` files with methods for balance, positions, and internal state.
2. **Service Implementation**: Implement the service in Python/Go to bridge internal state to the gRPC interface.
3. **Agent SDK Update**: Update agent communication layers to utilize the new service.

## Recommended Action
Defined `PortfolioService` in `portfolio.proto` and implemented the gRPC server in `internal/trading/portfolio/grpc_service.go`.

## Acceptance Criteria
- [x] `PortfolioService` defined in `.proto`.
- [x] Service implementation exposes key portfolio metrics and primitives.
- [x] Agents can query the service via gRPC.

## Work Log
### 2026-01-29 - Initial Creation
**By:** Antigravity
**Actions:** Created todo for agent service exposure.

### 2026-01-29 - Resolution
**By:** Antigravity
**Actions:** 
- Defined `PortfolioService` in `market_maker/api/proto/opensqt/market_maker/v1/portfolio.proto`.
- Implemented `PortfolioServiceServer` in `market_maker/internal/trading/portfolio/grpc_service.go`.
- Updated `PortfolioController` to expose `lastTargets` and `lastOpps`.
- Regenerated Go protobuf files.
