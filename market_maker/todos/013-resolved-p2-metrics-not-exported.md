---
status: completed
priority: p2
issue_id: 013
tags: [code-review, agent-native, observability, metrics]
dependencies: []
---

# Observability Metrics Not Exported - Agents Cannot Query System State

## Problem Statement

Prometheus metrics are collected internally (`pkg/telemetry/metrics.go`) but **not exported** via HTTP endpoint. Agents cannot query PnL, volume, latency, or position metrics programmatically.

**Impact**:
- No programmatic access to system metrics
- Agents cannot monitor performance
- Manual scraping of logs required
- Cannot integrate with monitoring tools

## Findings

**From Agent-Native Reviewer**:

**Location**: `pkg/telemetry/metrics.go`

**Metrics Collected (Internal Only)**:
1. `market_maker_pnl_realized_total` (Counter)
2. `market_maker_pnl_unrealized` (Observable Gauge)
3. `market_maker_orders_active` (Observable Gauge)
4. `market_maker_orders_placed_total` (Counter)
5. `market_maker_orders_filled_total` (Counter)
6. `market_maker_volume_total` (Counter)
7. `market_maker_position_size` (Observable Gauge)
8. `market_maker_latency_exchange_ms` (Histogram)
9. `market_maker_latency_tick_to_trade_ms` (Histogram)

**Missing**: `/metrics` HTTP endpoint for Prometheus scraping

## Proposed Solutions

### Option 1: Add Standard Prometheus HTTP Endpoint (Recommended)
**Effort**: 2-3 hours
**Risk**: Very Low
**Pros**:
- Industry standard
- Works with all monitoring tools
- Simple implementation

**Cons**:
- None

**Implementation**:
```go
// In cmd/market_maker/main.go or pkg/liveserver/server.go
import (
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

func startMetricsServer(port int) {
    mux := http.NewServeMux()
    mux.Handle("/metrics", promhttp.Handler())

    server := &http.Server{
        Addr:    fmt.Sprintf(":%d", port),
        Handler: mux,
    }

    log.Printf("Metrics server listening on :%d", port)
    if err := server.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}

// In main()
go startMetricsServer(9090)  // Standard Prometheus port
```

**Configuration**:
```yaml
# configs/config.yaml
telemetry:
  metrics_port: 9090
  enable_metrics: true
```

### Option 2: gRPC Metrics Query Service
**Effort**: 1 day
**Risk**: Low
**Pros**:
- Consistent with gRPC architecture
- Structured queries

**Cons**:
- Not standard Prometheus format
- Need custom client

**Implementation**:
```protobuf
service MetricsService {
  rpc GetMetrics(GetMetricsRequest) returns (MetricsSnapshot);
  rpc QueryMetric(QueryMetricRequest) returns (MetricValue);
}

message MetricsSnapshot {
  google.type.Decimal unrealized_pnl = 1;
  int32 active_orders = 2;
  google.type.Decimal position_size = 3;
  // ... all metrics
}
```

### Option 3: Add to Health Check Response
**Effort**: 1 hour
**Risk**: Very Low
**Pros**:
- Quick win
- Already have health endpoint

**Cons**:
- Not standard Prometheus format
- Limited to health check semantics

## Recommended Action

**Option 1** (Prometheus endpoint) as primary, **Option 3** (include in health) as bonus.

## Technical Details

### Affected Files
- New file: `internal/infrastructure/metrics/server.go` (metrics HTTP server)
- `cmd/market_maker/main.go` (start metrics server)
- `configs/config.yaml` (metrics configuration)

### Standard Prometheus Scrape Config
```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'market_maker'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 15s
```

### Enhanced Health Check
```go
// pkg/liveserver/server.go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    metrics := telemetry.GetMetrics()

    health := map[string]interface{}{
        "status": "ok",
        "components": map[string]interface{}{
            "exchange": map[string]interface{}{
                "status":     "healthy",
                "last_check": time.Now(),
            },
            "risk_monitor": map[string]interface{}{
                "status": "ok",
                "triggered": false,
            },
        },
        "metrics": map[string]interface{}{
            "active_orders":   metrics.GetActiveOrders(),
            "unrealized_pnl":  metrics.GetUnrealizedPnL(),
            "position_size":   metrics.GetPositionSize(),
        },
    }

    json.NewEncoder(w).Encode(health)
}
```

### Security Considerations
- Metrics endpoint should be on internal port (not public)
- Consider basic auth for production
- Rate limiting recommended

## Acceptance Criteria

- [x] `/metrics` endpoint returns Prometheus format
- [x] Prometheus can scrape metrics successfully
- [x] All 9 metric types are exported
- [x] Grafana dashboard can visualize metrics
- [x] Health check includes key metrics
- [x] Configuration allows disabling metrics
- [x] Documentation for Prometheus setup

## Work Log

**2026-01-22**: Agent-native review identified missing observability API. Metrics collected but not accessible.

## Resources

- Prometheus Go Client: https://github.com/prometheus/client_golang
- Prometheus HTTP Exposition: https://prometheus.io/docs/instrumenting/exposition_formats/
- OpenTelemetry Prometheus Export: https://opentelemetry.io/docs/specs/otel/metrics/sdk_exporters/prometheus/
- Agent-Native Review: See agent output above
