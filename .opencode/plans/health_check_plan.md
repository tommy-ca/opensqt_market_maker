# Health Check Implementation Plan

## 1. Core Interfaces
- **`IHealthMonitor`**: Define in `internal/core/interfaces.go`.
    - `Register(component string, check func() error)`
    - `GetStatus() map[string]string`
    - `IsHealthy() bool`

## 2. Infrastructure Implementation
- **`HealthManager`**: Implement in `internal/infrastructure/health/manager.go`.
    - Uses `sync.RWMutex` to protect status map.
    - runs a background loop (e.g., every 5s) to execute registered checks.
    - Aggregates results: if any critical component fails, global health is "Down".

## 3. gRPC Health Check Integration
- **Standard Protocol**: Use `google.golang.org/grpc/health/grpc_health_v1`.
- **`RemoteExchange` Update**:
    - Add `healthClient grpc_health_v1.HealthClient`.
    - Implement `CheckHealth()` method that calls `healthClient.Check()`.
    - Register this check with `HealthManager`.

## 4. Component Integration
- **`PriceMonitor`**:
    - Track `lastUpdateTime`.
    - Check: `time.Since(lastUpdateTime) < 1 * time.Minute`.
- **`OrderExecutor`**:
    - Track `errorCount` in rolling window.
    - Check: `errorRate < threshold`.
- **`RiskMonitor`**:
    - Check: `!IsTriggered()` (or report status as "Warning").

## 5. API Exposure
- **`HealthServer` Update**:
    - Inject `HealthManager`.
    - `/health` endpoint: Returns 200 OK if `IsHealthy()`, else 503.
    - `/status` endpoint: Returns JSON dump of all component statuses.

## 6. Testing Strategy (TDD)
- **Unit Tests**: Test `HealthManager` aggregation logic.
- **Integration Tests**: Mock `RemoteExchange`'s health client and verify `CheckHealth` behavior.
- **E2E Tests**: Verify `/health` endpoint reflects component state changes.
