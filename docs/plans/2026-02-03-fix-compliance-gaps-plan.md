---
title: Fix Compliance Gaps & Config Errors
type: fix
date: 2026-02-03
---

# Fix Compliance Gaps & Config Errors

## Overview

Address blocking compilation errors caused by the introduction of the `Secret` type for API credentials. This plan mechanically fixes type mismatches in configuration loading and exchange connector signing logic.

## Problem Statement

The codebase fails to compile because:
1.  **Config Masking**: `maskString` operates on `string`, but `ExchangeConfig` fields are now `Secret`.
2.  **Exchange Connectors**: `http.Header.Set` and HMAC signing functions expect `string`, but are receiving `Secret`.
3.  **Arbitrage Engine**: `LegManager` is missing `GetSignedSize` method.

## Proposed Solution

Apply explicit type casting `string(secret)` where the raw secret is legitimately needed (signing, headers) or where masking logic is applied. Add the missing method to `LegManager`.

## Implementation Steps

### Phase 1: Fix Configuration
- [ ] Update `market_maker/internal/config/config.go` to cast `Secret` to `string` before masking, and back to `Secret` after.

### Phase 2: Fix Exchange Connectors
- [ ] Fix `market_maker/internal/exchange/bybit/bybit.go`: Cast `APIKey` and `SecretKey` to `string` in `SignRequest`.
- [ ] Fix `market_maker/internal/exchange/gate/gate.go`: Cast keys in headers and JSON maps.
- [ ] Fix `market_maker/internal/exchange/binancespot/binance_spot.go`: Cast keys in `SignRequest`.
- [ ] Fix `market_maker/internal/exchange/binance/binance.go`: Cast keys in `SignRequest`.
- [ ] Fix `market_maker/internal/exchange/okx/okx.go`: Cast keys in `SignRequest`.
- [ ] Fix `market_maker/internal/exchange/bitget/bitget.go`: Cast keys in `SignRequest`.

### Phase 3: Fix Arbitrage Engine
- [ ] Add `GetSignedSize` method to `market_maker/internal/trading/arbitrage/leg_manager.go`.

### Phase 4: Verification
- [ ] Run `go build ./...` to confirm compilation success.

## Acceptance Criteria
- [ ] `go build ./...` passes without type errors.
- [ ] `Secret` type prevents accidental logging (verified by inspection of usage).
