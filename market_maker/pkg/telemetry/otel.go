package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	tracetype "go.opentelemetry.io/otel/trace"
)

// Telemetry provides OTel setup
type Telemetry struct {
	tp *trace.TracerProvider
	mp *sdkmetric.MeterProvider
	lp *sdklog.LoggerProvider
}

// Setup initializes OTel tracing, metrics, and logging
func Setup(serviceName string) (*Telemetry, error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 1. Trace Provider
	traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// 2. Metric Provider
	metricExporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricExporter),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Initialize application metrics
	metricsHolder := GetGlobalMetrics()
	if err := metricsHolder.InitMetrics(mp.Meter(serviceName)); err != nil {
		return nil, fmt.Errorf("failed to init metrics: %w", err)
	}

	// 3. Log Provider
	logExporter, err := stdoutlog.New(stdoutlog.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	global.SetLoggerProvider(lp)

	return &Telemetry{
		tp: tp,
		mp: mp,
		lp: lp,
	}, nil
}

// Shutdown flushes and stops the providers
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	if err := t.tp.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("trace provider shutdown failed: %w", err))
	}
	if err := t.mp.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("meter provider shutdown failed: %w", err))
	}
	if err := t.lp.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("log provider shutdown failed: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("telemetry shutdown errors: %v", errs)
	}
	return nil
}

// GetMeter returns a meter for the given name
func GetMeter(name string) metric.Meter {
	return otel.GetMeterProvider().Meter(name)
}

// GetTracer returns a tracer for the given name
func GetTracer(name string) tracetype.Tracer {
	return otel.GetTracerProvider().Tracer(name)
}
