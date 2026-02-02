package telemetry

import (
	"log"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

// InitMetrics initializes the Prometheus exporter and sets the global meter provider
func InitMetrics() error {
	exporter, err := prometheus.New()
	if err != nil {
		return err
	}

	provider := metric.NewMeterProvider(
		metric.WithReader(exporter),
	)
	otel.SetMeterProvider(provider)

	// Initialize instruments
	holder := GetGlobalMetrics()
	meter := provider.Meter("market_maker_core")
	if err := holder.InitMetrics(meter); err != nil {
		log.Printf("Failed to initialize instruments: %v", err)
		return err
	}

	return nil
}
