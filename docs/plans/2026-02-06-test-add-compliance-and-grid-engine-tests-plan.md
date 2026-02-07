---
title: Add Compliance and Grid Engine Tests
type: test
date: 2026-02-06
---

# Add Compliance and Grid Engine Tests

## Overview

Add unit tests to verify recent compliance fixes (secret masking, strict type casting) and `GridEngine` refactoring. This ensures that sensitive data is correctly masked in logs and that the refactored trading logic functions as expected without regressions.

## Problem Statement / Motivation

Recent changes introduced critical compliance fixes:
1.  **Secret Masking**: `Config.String()` was updated to use explicit masking (`****`) instead of the default `[REDACTED]`. This needs verification to ensure keys aren't logged in plaintext or fully redacted when debugging requires partial visibility.
2.  **Type Safety**: Exchange adapters were updated to cast `Secret` to `string` for API calls. We need to ensure this casting works correctly and doesn't break request signing.
3.  **Refactoring**: `GridEngine` was refactored to use a new `CalculateActions` interface. Currently, it lacks dedicated unit tests (`engine_test.go`), relying only on E2E tests.

## Proposed Solution

1.  **Fix Existing Tests**: The recent `GridEngine` refactor broke `grid_test.go`, `dynamic_grid_test.go`, and `trend_following_test.go`. These must be updated to use `NewStrategy`, `CalculateActions`, and `Slot` types instead of the deprecated `NewGridStrategy`, `CalculateTargetState`, and `GridLevel`.
2.  **Config Tests**: Add `TestConfig_String` to `market_maker/internal/config/config_test.go` to verify `String()` output masks secrets correctly.
3.  **Exchange Tests**: Add or update signing tests for `gate`, `binance_spot`, `okx`, and `bitget` to verify that `Secret` fields are correctly cast and used in headers/signatures.
4.  **GridEngine Tests**: Create `market_maker/internal/engine/gridengine/engine_test.go` to unit test `OnPriceUpdate`, verifying the flow from price update -> strategy calculation -> execution.

## Acceptance Criteria

- [ ] **Fix Breakage**: `go test ./market_maker/internal/trading/grid/...` passes without compilation errors.
- [ ] `market_maker/internal/config/config_test.go` includes a test case for `Config.String()` that asserts output contains masked keys (e.g., `****1234`).
- [ ] `gate_test.go` (or new file) verifies `SignREST` produces correct headers with `Secret` config.
- [ ] `bitget_test.go` (or new file) verifies `SignRequest` produces correct headers with `Secret` config.
- [ ] `binance_spot_test.go` verifies `SignRequest` works with updated `Secret` casting.
- [ ] `okx_test.go` verifies `SignRequest` works with updated `Secret` casting.
- [ ] `market_maker/internal/engine/gridengine/engine_test.go` is created and tests `OnPriceUpdate` triggering order placement.

## Technical Considerations

-   **Mocking**: Use existing manual mocks where possible (`market_maker/internal/mock`), or `testify` for simple assertions.
-   **Security**: Ensure tests themselves do not log real secrets (use dummy values like `test_secret_key`).
-   **Refactoring**: The existing grid tests rely on `CalculateTargetState` which returned a declarative state. The new `CalculateActions` returns imperative `OrderAction`s. Tests must be updated to assert on the *actions* returned (e.g., "expect 1 buy order") rather than the *target state*.

## Implementation Steps

### 1. Fix Broken Grid Tests

Update `market_maker/internal/trading/grid/*.go`:
-   Replace `NewGridStrategy` with `NewStrategy`.
-   Replace `CalculateTargetState` calls with `CalculateActions`.
-   Convert `GridLevel` struct usage to `Slot` struct.
-   Update assertions: check `[]*pb.OrderAction` results.

### 2. Config Masking Test

Update `market_maker/internal/config/config_test.go`:

```go
func TestConfig_String(t *testing.T) {
    cfg := &Config{
        Exchanges: map[string]ExchangeConfig{
            "test": {
                APIKey:    Secret("my_super_secret_api_key"),
                SecretKey: Secret("my_super_secret_secret_key"),
            },
        },
    }
    output := cfg.String()
    assert.Contains(t, output, "my_s****_key") // Check for partial mask
    assert.NotContains(t, output, "my_super_secret_api_key") // Ensure full cleartext is gone
}
```

### 2. Grid Engine Unit Test

Create `market_maker/internal/engine/gridengine/engine_test.go`:

```go
package gridengine

import (
    "testing"
    "context"
    "market_maker/internal/core"
    "market_maker/internal/mock"
    "github.com/stretchr/testify/assert"
)

func TestGridEngine_OnPriceUpdate(t *testing.T) {
    // Setup mocks
    mockExec := mock.NewMockOrderExecutor()
    mockSlotMgr := mock.NewMockPositionManager()
    // ... setup engine with real Strategy ...

    // Trigger update
    err := engine.OnPriceUpdate(ctx, &pb.PriceChange{Price: "50000"})
    
    // Assert calls
    assert.NoError(t, err)
    // assert executor called if logic dictates
}
```

### 3. Exchange Signing Tests

#### `gate_test.go`
Test `SignREST` directly as it's a pure function:
```go
func TestGateSignREST(t *testing.T) {
    // Setup exchange with known secret
    e := NewGateExchange(&config.ExchangeConfig{SecretKey: "secret"}, logger)
    
    // Call SignREST
    sig := e.SignREST("POST", "/path", "", "body", 1234567890)
    
    // Verify against expected HMAC-SHA512
    // ...
}
```

#### `binance_spot_test.go`
Test `SignRequest` by inspecting the modified request:
```go
func TestBinanceSpotSignRequest(t *testing.T) {
    e := NewBinanceSpotExchange(&config.ExchangeConfig{APIKey: "key", SecretKey: "secret"}, logger, nil)
    req := httptest.NewRequest("GET", "/api/v3/account", nil)
    
    err := e.SignRequest(req, nil)
    assert.NoError(t, err)
    
    assert.Equal(t, "key", req.Header.Get("X-MBX-APIKEY"))
    assert.NotEmpty(t, req.URL.Query().Get("signature"))
}
```

#### `okx_test.go` & `bitget_test.go`
Similar to Binance Spot, inspect headers after `SignRequest`.

## References

-   `market_maker/internal/config/config.go`
-   `market_maker/internal/engine/gridengine/engine.go`
-   `docs/brainstorms/2026-02-03-fix-compliance-gaps-brainstorm.md`
