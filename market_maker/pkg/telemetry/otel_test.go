package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestTelemetrySetup(t *testing.T) {
	tel, err := Setup("test-service")
	if err != nil {
		t.Fatalf("Failed to setup telemetry: %v", err)
	}

	// Verify providers are set
	if otel.GetTracerProvider() == nil {
		t.Error("Tracer provider not set")
	}
	if otel.GetMeterProvider() == nil {
		t.Error("Meter provider not set")
	}

	// Test GetTracer/GetMeter
	tracer := GetTracer("test-tracer")
	if tracer == nil {
		t.Error("Failed to get tracer")
	}

	meter := GetMeter("test-meter")
	if meter == nil {
		t.Error("Failed to get meter")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := tel.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}
