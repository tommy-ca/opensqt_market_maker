# Phase 22: Production Readiness Audit Report

**Date**: Jan 22, 2026
**Auditor**: OpenSQT Agent

## 1. Findings Summary

| Severity | Category | Issue | Remediation |
| :--- | :--- | :--- | :--- |
| 🔴 **CRITICAL** | Code | `OrderExecutor` uses fixed RNG (`0.5`) for retry jitter | Replace with `math/rand` or `crypto/rand` |
| 🔴 **CRITICAL** | Code | `BatchPlaceOrders` never sets `marginError` flag | Update logic to detect margin errors in batch loop |
| 🟡 **HIGH** | Code | Inefficient/Custom string search in `isPostOnlyError` | Replace with `strings.Contains` |
| 🟡 **HIGH** | Config | `postgres` default password is "secret" in docker-compose | Add warning to README / Enforce strong passwords in prod env vars |
| 🟡 **HIGH** | Docs | `deployment.md` recommends deprecated "Standalone Mode" | Update doc to mandate gRPC mode |
| 🟢 **LOW** | Code | Manual `pow` implementation | Replace with `math.Pow` |
| 🟢 **LOW** | Config | `market_maker` exposes port 8080 (Health) publicly | Ensure firewall rules or reverse proxy handle auth if exposed |

## 2. Detailed Findings

### 2.1 Code Quality (`OrderExecutor`)

**Issue 1: Broken Jitter (`randFloat`)**
The `randFloat` function in `executor.go` returns a static `0.5`. This causes all retrying instances to sync up (thundering herd), negating the benefit of jitter.
```go
func randFloat() float64 {
    // Simple pseudo-random for testing - in production use crypto/rand
    return 0.5 // Fixed for deterministic testing
}
```

**Issue 2: Broken Batch Error Reporting**
In `BatchPlaceOrders`, the `marginError` variable is defined but never updated, even if an error occurs. This effectively disables "Fail Fast" logic for margin issues during batch operations.

**Issue 3: Inefficient String Search**
A custom recursive `containsString` function is used instead of the standard `strings.Contains`. This is inefficient and harder to maintain.

### 2.2 Configuration

**Issue 4: Default Secrets**
`docker-compose.yml` sets `POSTGRES_PASSWORD` to "secret" by default. While acceptable for dev, this is a risk for production if users blindly deploy.

### 2.3 Documentation

**Issue 5: Outdated Deployment Guide**
`docs/deployment.md` currently lists "Standalone Mode (Go Native)" as the "recommended" mode. This contradicts the Phase 16 gRPC Architecture mandate which requires the split architecture for all production deployments.

## 3. Remediation Plan

1.  **Fix `OrderExecutor`**:
    - Import `math/rand` (or `crypto/rand`) and `math`.
    - Replace `randFloat` with real RNG.
    - Replace `containsString` with `strings.Contains`.
    - Implement error classification in `BatchPlaceOrders` to set `marginError = true` on relevant errors.
2.  **Update Documentation**:
    - Rewrite `docs/deployment.md` to feature gRPC deployment as the primary/only production method.
    - Add security warning about default passwords in `README.md`.
