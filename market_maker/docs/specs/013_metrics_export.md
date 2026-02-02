# Metrics Export Spec

## Problem
Internal metrics (PnL, volume, orders) are collected using OpenTelemetry meters in `pkg/telemetry`, but there is no exporter configured to expose these metrics to Prometheus.

## Solution
1.  **Configure Prometheus Exporter**: Initialize the OpenTelemetry Prometheus exporter.
2.  **Expose `/metrics` Endpoint**: Start a lightweight HTTP server (or attach to existing `liveserver`) to serve the metrics.

## Design

### 1. Telemetry Package Update (`pkg/telemetry/metrics.go` or new `exporter.go`)
Add function to initialize Prometheus exporter.

```go
import (
    "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/sdk/metric"
)

func InitPrometheusExporter() (*prometheus.Exporter, error) {
    exporter, err := prometheus.New()
    if err != nil {
        return nil, err
    }
    
    provider := metric.NewMeterProvider(
        metric.WithReader(exporter),
    )
    otel.SetMeterProvider(provider)
    
    return exporter, nil
}
```

### 2. Metrics Server
We can add the `/metrics` endpoint to the existing `LiveServer` since it already exposes HTTP/WS on a configurable port. This is cleaner than starting a separate server.

Update `pkg/liveserver/server.go`:
```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

// In NewServer or Start:
mux.Handle("/metrics", promhttp.Handler())
```

### 3. Verification
- Start server.
- Hit `http://localhost:8080/metrics`.
- Verify presence of `market_maker_...` metrics.

## Implementation Details

### File: `pkg/telemetry/exporter.go` (New)
Implement `InitMetrics`.

### File: `cmd/live_server/main.go`
Call `telemetry.InitMetrics()` at startup.

### File: `pkg/liveserver/server.go`
Add the handler.

## Acceptance Criteria
- `/metrics` endpoint is active.
- Returns standard Prometheus format.
- Includes `market_maker` metrics.
