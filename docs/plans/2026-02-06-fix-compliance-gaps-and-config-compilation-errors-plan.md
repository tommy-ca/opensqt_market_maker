---
title: Fix Compliance Gaps and Config Compilation Errors
type: fix
date: 2026-02-06
---

# Fix Compliance Gaps and Config Compilation Errors

## Overview

Address critical findings from the Compliance Report (2026-02-03) regarding strict type safety for secrets and execution stability documentation. This plan focuses on fixing immediate compilation errors caused by strict `Secret` vs `string` typing in the configuration and exchange adapters.

## Problem Statement / Motivation

The codebase currently fails to compile because of strict type safety enforcement on the `Secret` type.
1.  **Compilation Errors**: `Secret` (a distinct type) is being passed directly to functions expecting `string` (e.g., `http.Header.Set`), causing build failures.
2.  **Security Obfuscation**: The current behavior of `Secret` might result in `"[REDACTED]"` being sent to exchanges if passed via `json.Marshal` in loose maps, or completely opaque logging when debugging requires partial visibility.
3.  **Compliance**: The system must explicitly handle secret exposure (via casting) to confirm intent, and the execution loop behavior needs to be formally accepted as compliant.

## Proposed Solution

1.  **Explicit Casting**: Update all exchange adapters to explicitly cast `Secret` to `string` at the network boundary (headers, signatures). This fixes compilation and asserts "intent to expose".
2.  **Config Masking**: Update `Config.String()` to properly use `maskString` by casting `Secret` -> `string` -> `mask` -> `Secret`, allowing for safe logging of configuration state (e.g., `api_key: ****1234`).
3.  **GridEngine Acceptance**: No code changes for `GridEngine` logic, but implicitly accepting the "Manual Loop" by successfully building and running the application.

## Technical Considerations

-   **Secret Safety**: We are NOT changing the `Secret` type definition. We are fixing its *usage*.
-   **JSON Marshaling**: In `bitget.go` and `okx.go`, constructing maps like `map[string]interface{}{"apiKey": config.APIKey}` is dangerous because `json.Marshal` will use `Secret.MarshalJSON()` and send `"[REDACTED]"`. Casting to `string` is mandatory here.

## Acceptance Criteria

-   [ ] `go build ./...` passes without errors.
-   [ ] `gate.go`: Header `KEY` is set using `string(e.Config.APIKey)`.
-   [ ] `binance_spot.go`: Header `X-MBX-APIKEY` is set using `string(e.Config.APIKey)`.
-   [ ] `okx.go`: Headers and `map[string]string` usage explicitly cast secrets.
-   [ ] `bitget.go`: JSON payload maps explicitly cast secrets to prevent sending "REDACTED".
-   [ ] `config.go`: `Config.String()` implementation uses `maskString` to show partially masked keys instead of full redaction or plaintext.

## Implementation Steps

### 1. Fix Exchange Adapters

Update the following files to cast `Secret` to `string` where required:

#### `market_maker/internal/exchange/gate/gate.go`
```go
// Before (Error)
httpReq.Header.Set("KEY", e.Config.APIKey)

// After (Fix)
httpReq.Header.Set("KEY", string(e.Config.APIKey))
```

#### `market_maker/internal/exchange/binancespot/binance_spot.go`
```go
// Before (Error)
req.Header.Set("X-MBX-APIKEY", e.Config.APIKey)

// After (Fix)
req.Header.Set("X-MBX-APIKEY", string(e.Config.APIKey))
```

#### `market_maker/internal/exchange/okx/okx.go`
Fix headers and the payload map:
```go
req.Header.Set("OK-ACCESS-KEY", string(e.Config.APIKey))
req.Header.Set("OK-ACCESS-PASSPHRASE", string(e.Config.Passphrase))

// In payload map
"apiKey": string(e.Config.APIKey),
```

#### `market_maker/internal/exchange/bitget/bitget.go`
Fix the payload map to ensure actual key is sent:
```go
// In payload map
"apiKey": string(e.Config.APIKey),
```

### 2. Update Config Masking

#### `market_maker/internal/config/config.go`
Update `String()` to use `maskString`:

```go
func (c *Config) String() string {
    configCopy := *c
    configCopy.Exchanges = make(map[string]ExchangeConfig)
    for name, exchange := range c.Exchanges {
        // Explicitly mask sensitive fields
        maskedExchange := exchange
        maskedExchange.APIKey = Secret(maskString(string(exchange.APIKey)))
        maskedExchange.SecretKey = Secret(maskString(string(exchange.SecretKey)))
        maskedExchange.Passphrase = Secret(maskString(string(exchange.Passphrase)))
        configCopy.Exchanges[name] = maskedExchange
    }
    data, _ := yaml.Marshal(configCopy)
    return string(data)
}
```

## Success Metrics

-   Build success (`go build ./...`).
-   Exchange connectivity works (authentication succeeds) after fixes.
-   Logs show masked keys (e.g., `****1234`) instead of `[REDACTED]`.

## Dependencies & Risks

-   **Risk**: If a cast is missed in a `map[string]interface{}` (like Bitget), the API call will fail at runtime with an "Invalid API Key" error because it sent "REDACTED". Review these maps carefully.
-   **Dependency**: None.

## References

-   Brainstorm: `docs/brainstorms/2026-02-03-fix-compliance-gaps-brainstorm.md`
