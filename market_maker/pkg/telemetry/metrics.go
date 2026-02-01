package telemetry

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metric names
const (
	MetricPnLRealizedTotal   = "market_maker_pnl_realized_total"
	MetricPnLUnrealized      = "market_maker_pnl_unrealized"
	MetricOrdersActive       = "market_maker_orders_active"
	MetricOrdersPlacedTotal  = "market_maker_orders_placed_total"
	MetricOrdersFilledTotal  = "market_maker_orders_filled_total"
	MetricVolumeTotal        = "market_maker_volume_total"
	MetricPositionSize       = "market_maker_position_size"
	MetricLatencyExchange    = "market_maker_latency_exchange_ms"
	MetricLatencyTickToTrade = "market_maker_latency_tick_to_trade_ms"
	MetricRiskTriggered      = "market_maker_risk_triggered"
	MetricCircuitBreakerOpen = "market_maker_circuit_breaker_open"
	MetricQualityScore       = "market_maker_quality_score"
	MetricDeltaNeutrality    = "market_maker_delta_neutrality"
	MetricToxicBasisCount    = "market_maker_toxic_basis_count"
)

// MetricsHolder holds initialized instruments
type MetricsHolder struct {
	PnLRealizedTotal   metric.Float64Counter
	PnLUnrealized      metric.Float64ObservableGauge
	OrdersActive       metric.Int64ObservableGauge
	OrdersPlacedTotal  metric.Int64Counter
	OrdersFilledTotal  metric.Int64Counter
	VolumeTotal        metric.Float64Counter
	PositionSize       metric.Float64ObservableGauge
	LatencyExchange    metric.Float64Histogram
	LatencyTickToTrade metric.Float64Histogram
	RiskTriggered      metric.Int64ObservableGauge
	CircuitBreakerOpen metric.Int64ObservableGauge
	QualityScore       metric.Float64ObservableGauge
	DeltaNeutrality    metric.Float64ObservableGauge
	ToxicBasisCount    metric.Int64ObservableGauge

	// State for observable gauges
	mu               sync.RWMutex
	unrealizedPnLMap map[string]float64
	activeOrdersMap  map[string]int64
	positionSizeMap  map[string]float64
	riskTriggeredMap map[string]int64
	cbOpenMap        map[string]int64
	qualityScoreMap  map[string]float64
	deltaNeutralMap  map[string]float64
	toxicBasisMap    map[string]int64
}

var (
	globalMetrics *MetricsHolder
	initOnce      sync.Once
)

// GetGlobalMetrics returns the singleton metrics holder
func GetGlobalMetrics() *MetricsHolder {
	initOnce.Do(func() {
		globalMetrics = &MetricsHolder{
			unrealizedPnLMap: make(map[string]float64),
			activeOrdersMap:  make(map[string]int64),
			positionSizeMap:  make(map[string]float64),
			riskTriggeredMap: make(map[string]int64),
			cbOpenMap:        make(map[string]int64),
			qualityScoreMap:  make(map[string]float64),
			deltaNeutralMap:  make(map[string]float64),
			toxicBasisMap:    make(map[string]int64),
		}
		// Initialization of instruments happens in InitMetrics
	})
	return globalMetrics
}

// InitMetrics initializes instruments using the meter
func (m *MetricsHolder) InitMetrics(meter metric.Meter) error {
	var err error

	m.PnLRealizedTotal, err = meter.Float64Counter(MetricPnLRealizedTotal, metric.WithDescription("Cumulative realized profit/loss"))
	if err != nil {
		return err
	}

	m.OrdersPlacedTotal, err = meter.Int64Counter(MetricOrdersPlacedTotal, metric.WithDescription("Total orders placed"))
	if err != nil {
		return err
	}

	m.OrdersFilledTotal, err = meter.Int64Counter(MetricOrdersFilledTotal, metric.WithDescription("Total orders filled"))
	if err != nil {
		return err
	}

	m.VolumeTotal, err = meter.Float64Counter(MetricVolumeTotal, metric.WithDescription("Total trading volume in base asset"))
	if err != nil {
		return err
	}

	m.LatencyExchange, err = meter.Float64Histogram(MetricLatencyExchange, metric.WithDescription("Latency of exchange API calls"), metric.WithUnit("ms"))
	if err != nil {
		return err
	}

	m.LatencyTickToTrade, err = meter.Float64Histogram(MetricLatencyTickToTrade, metric.WithDescription("Time from price update to order action"), metric.WithUnit("ms"))
	if err != nil {
		return err
	}

	// Observables
	m.PnLUnrealized, err = meter.Float64ObservableGauge(MetricPnLUnrealized, metric.WithDescription("Current unrealized PnL"),
		metric.WithFloat64Callback(func(ctx context.Context, obs metric.Float64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.unrealizedPnLMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	m.OrdersActive, err = meter.Int64ObservableGauge(MetricOrdersActive, metric.WithDescription("Number of currently open orders"),
		metric.WithInt64Callback(func(ctx context.Context, obs metric.Int64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.activeOrdersMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	m.PositionSize, err = meter.Float64ObservableGauge(MetricPositionSize, metric.WithDescription("Current position size"),
		metric.WithFloat64Callback(func(ctx context.Context, obs metric.Float64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.positionSizeMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	m.RiskTriggered, err = meter.Int64ObservableGauge(MetricRiskTriggered, metric.WithDescription("Risk monitor triggered state (1=triggered, 0=normal)"),
		metric.WithInt64Callback(func(ctx context.Context, obs metric.Int64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.riskTriggeredMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	m.CircuitBreakerOpen, err = meter.Int64ObservableGauge(MetricCircuitBreakerOpen, metric.WithDescription("Circuit breaker open state (1=open, 0=closed)"),
		metric.WithInt64Callback(func(ctx context.Context, obs metric.Int64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.cbOpenMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	m.QualityScore, err = meter.Float64ObservableGauge(MetricQualityScore, metric.WithDescription("Current arbitrage quality score"),
		metric.WithFloat64Callback(func(ctx context.Context, obs metric.Float64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.qualityScoreMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	m.DeltaNeutrality, err = meter.Float64ObservableGauge(MetricDeltaNeutrality, metric.WithDescription("Current delta neutrality (1=perfectly hedged, 0=unhedged)"),
		metric.WithFloat64Callback(func(ctx context.Context, obs metric.Float64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.deltaNeutralMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	m.ToxicBasisCount, err = meter.Int64ObservableGauge(MetricToxicBasisCount, metric.WithDescription("Current consecutive toxic basis intervals"),
		metric.WithInt64Callback(func(ctx context.Context, obs metric.Int64Observer) error {
			m.mu.RLock()
			defer m.mu.RUnlock()
			for sym, val := range m.toxicBasisMap {
				obs.Observe(val, metric.WithAttributes(attribute.String("symbol", sym)))
			}
			return nil
		}))
	if err != nil {
		return err
	}

	return nil
}

// Helpers to update observable state

func (m *MetricsHolder) SetRiskTriggered(symbol string, triggered bool) {
	val := int64(0)
	if triggered {
		val = 1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.riskTriggeredMap[symbol] = val
}

func (m *MetricsHolder) SetCircuitBreakerOpen(symbol string, open bool) {
	val := int64(0)
	if open {
		val = 1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cbOpenMap[symbol] = val
}

func (m *MetricsHolder) SetUnrealizedPnL(symbol string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unrealizedPnLMap[symbol] = value
}

func (m *MetricsHolder) SetActiveOrders(symbol string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeOrdersMap[symbol] = count
}

func (m *MetricsHolder) SetPositionSize(symbol string, size float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.positionSizeMap[symbol] = size
}

func (m *MetricsHolder) SetQualityScore(symbol string, score float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.qualityScoreMap[symbol] = score
}

func (m *MetricsHolder) SetDeltaNeutrality(symbol string, neutrality float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deltaNeutralMap[symbol] = neutrality
}

func (m *MetricsHolder) SetToxicBasisCount(symbol string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toxicBasisMap[symbol] = count
}

func (m *MetricsHolder) GetUnrealizedPnL() map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]float64)
	for k, v := range m.unrealizedPnLMap {
		res[k] = v
	}
	return res
}

func (m *MetricsHolder) GetActiveOrders() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]int64)
	for k, v := range m.activeOrdersMap {
		res[k] = v
	}
	return res
}

func (m *MetricsHolder) GetPositionSize() map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]float64)
	for k, v := range m.positionSizeMap {
		res[k] = v
	}
	return res
}
