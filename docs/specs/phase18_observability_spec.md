# Phase 18.3: Observability & Monitoring - Technical Specification

**Project**: OpenSQT Market Maker
**Status**: DRAFT
**Phase**: 18.3

---

## 1. Overview

This phase focuses on exposing internal state and events as standardized metrics for Prometheus and providing real-time alerts via Slack/Telegram.

## 2. Metrics (Prometheus)

The system already uses OpenTelemetry. We will standardize the metrics exposed.

### 2.1 Metric Definitions

| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `market_maker_pnl_realized_total` | Counter | `symbol`, `strategy` | Cumulative realized profit/loss |
| `market_maker_pnl_unrealized` | Gauge | `symbol`, `strategy` | Current unrealized PnL based on mark price |
| `market_maker_orders_active` | Gauge | `symbol`, `side` | Number of currently open orders |
| `market_maker_orders_placed_total` | Counter | `symbol`, `side`, `type` | Total orders placed |
| `market_maker_orders_filled_total` | Counter | `symbol`, `side` | Total orders filled (trades) |
| `market_maker_volume_total` | Counter | `symbol` | Total trading volume in base asset |
| `market_maker_position_size` | Gauge | `symbol` | Current position size (signed) |
| `market_maker_latency_exchange_ms` | Histogram | `exchange`, `operation` | Latency of exchange API calls |
| `market_maker_latency_tick_to_trade_ms` | Histogram | `symbol` | Time from price update to order action |

### 2.2 Exporter Configuration

- **Endpoint**: `/metrics`
- **Port**: Re-use Health Server port (default 8080) or configurable `metrics_port`.
- **Registry**: Use the global Prometheus registry initialized by OTel.

## 3. Alerting

### 3.1 Alert Manager

A new component `internal/alert/AlertManager` will handle dispatching notifications.

```go
type AlertLevel string

const (
    Info    AlertLevel = "INFO"
    Warning AlertLevel = "WARNING"
    Error   AlertLevel = "ERROR"
    Critical AlertLevel = "CRITICAL"
)

type AlertPayload struct {
    Level     AlertLevel
    Title     string
    Message   string
    Timestamp time.Time
    Fields    map[string]string
}

type AlertChannel interface {
    Send(ctx context.Context, alert AlertPayload) error
}
```

### 3.2 Alert Conditions

The system will trigger alerts for:
1.  **Circuit Breaker Trip**: Critical. Trading stopped.
2.  **Risk Limit Breach**: Warning/Error.
3.  **Exchange Disconnect**: Warning (if reconnecting) -> Error (if persistent).
4.  **Strategy Panic**: Critical.
5.  **Large Execution**: Info (e.g. fill > threshold).

### 3.3 Channels

- **Slack**: Webhook integration.
- **Telegram**: Bot API integration.
- **Log**: Fallback channel (always enabled).

## 4. Implementation Plan

1.  **Metrics**:
    - Update `pkg/telemetry` to provide easy access to metric instruments.
    - Instrument `PositionManager` for PnL and Position metrics.
    - Instrument `OrderExecutor` for Order metrics.
    - Instrument `RemoteExchange` for latency metrics.
    - Expose `/metrics` handler in `metrics_server.go` (or `health_server.go`).

2.  **Alerting**:
    - Implement `pkg/alert` with `SlackChannel` and `TelegramChannel`.
    - Integrate `AlertManager` into `Orchestrator` and `RiskMonitor`.

3.  **Configuration**:
    - Add `telemetry` and `alerts` sections to `config.yaml`.

---

## 5. Configuration Schema

```yaml
telemetry:
  metrics_enabled: true
  metrics_port: 9090 # Optional, defaults to health port if not set

alerts:
  enabled: true
  slack:
    enabled: false
    webhook_url: "https://hooks.slack.com/..."
    channel: "#trading-alerts"
  telegram:
    enabled: false
    bot_token: "..."
    chat_id: "..."
```
