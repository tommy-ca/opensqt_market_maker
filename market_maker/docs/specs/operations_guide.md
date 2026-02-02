# Operations Guide: Grid Trading System

This guide provides actionable instructions for configuring, monitoring, and recovering the Grid Trading system.

## Table of Contents

1. [Configuration](#configuration)
2. [Monitoring](#monitoring)
3. [Emergency Runbooks](#emergency-runbooks)

## Configuration

The system is configured via `StrategyConfig`.

### Key Parameters

*   **GridSize**: Number of levels.
*   **SkewFactor**: Adjusts center price based on inventory.

## Monitoring

### Key Metrics

*   `realized_pnl`
*   `unrealized_pnl`
*   `active_slots`

## Emergency Runbooks

### Risk Mode Triggered

**Symptom**: `risk_mode_active` is true.

**Action**: Check volatility.

### Stuck Slot Recovery

**Symptom**: Slot LOCKED for > 5 minutes.

**Action**: Verify order status.
