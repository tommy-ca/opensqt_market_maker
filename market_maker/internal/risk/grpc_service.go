package risk

import (
	"context"
	"time"

	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RiskServiceServer struct {
	pb.UnimplementedRiskServiceServer
	monitor        *RiskMonitor
	reconciler     *Reconciler
	circuitBreaker *CircuitBreaker
}

func NewRiskServiceServer(monitor *RiskMonitor, reconciler *Reconciler, cb *CircuitBreaker) *RiskServiceServer {
	return &RiskServiceServer{
		monitor:        monitor,
		reconciler:     reconciler,
		circuitBreaker: cb,
	}
}

func (s *RiskServiceServer) GetRiskMetrics(ctx context.Context, req *pb.GetRiskMetricsRequest) (*pb.GetRiskMetricsResponse, error) {
	symbols := req.Symbols
	if len(symbols) == 0 {
		symbols = s.monitor.GetAllSymbols()
	}

	var metrics []*pb.SymbolRiskMetrics
	for _, symbol := range symbols {
		stats := s.monitor.GetInternalStats(symbol)
		if stats == nil {
			continue
		}

		stats.mu.RLock()

		metric := &pb.SymbolRiskMetrics{
			Symbol:        symbol,
			PositionSize:  pbu.FromGoDecimal(stats.PositionSize),
			NotionalValue: pbu.FromGoDecimal(stats.NotionalValue),
			UnrealizedPnl: pbu.FromGoDecimal(stats.UnrealizedPnL),
			Leverage:      pbu.FromGoDecimal(stats.Leverage),
			RiskScore:     pbu.FromGoDecimal(stats.RiskScore),
			LimitBreach:   stats.IsTriggered,
			LimitType:     "volatility_anomaly",
		}

		// If this is the symbol managed by our reconciler, we can get more accurate position info
		if s.reconciler != nil && s.reconciler.symbol == symbol {
			pm := s.reconciler.GetPositionManager()
			if pm != nil {
				snap := pm.GetSnapshot()
				totalQty := decimal.Zero
				for _, slot := range snap.Slots {
					if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
						totalQty = totalQty.Add(pbu.ToGoDecimal(slot.PositionQty))
					}
				}
				metric.PositionSize = pbu.FromGoDecimal(totalQty)
				metric.NotionalValue = pbu.FromGoDecimal(totalQty.Mul(stats.LastPrice).Abs())
			}
		}
		stats.mu.RUnlock()

		metrics = append(metrics, metric)
	}

	return &pb.GetRiskMetricsResponse{Metrics: metrics}, nil
}

func (s *RiskServiceServer) GetPositionLimits(ctx context.Context, req *pb.GetPositionLimitsRequest) (*pb.GetPositionLimitsResponse, error) {
	return &pb.GetPositionLimitsResponse{
		Symbol:           req.Symbol,
		MaxPositionSize:  pbu.FromGoDecimal(decimal.Zero),
		MaxNotionalValue: pbu.FromGoDecimal(decimal.Zero),
		MaxLeverage:      pbu.FromGoDecimal(decimal.Zero),
		MaxOrderSize:     pbu.FromGoDecimal(decimal.Zero),
	}, nil
}

func (s *RiskServiceServer) UpdatePositionLimits(ctx context.Context, req *pb.UpdatePositionLimitsRequest) (*pb.UpdatePositionLimitsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "UpdatePositionLimits not implemented")
}

func (s *RiskServiceServer) GetExposure(ctx context.Context, req *pb.GetExposureRequest) (*pb.GetExposureResponse, error) {
	symbols := s.monitor.GetAllSymbols()
	var exposures []*pb.SymbolExposure
	totalNotional := decimal.Zero

	for _, symbol := range symbols {
		stats := s.monitor.GetInternalStats(symbol)
		if stats == nil {
			continue
		}

		stats.mu.RLock()

		notional := stats.NotionalValue
		if s.reconciler != nil && s.reconciler.symbol == symbol {
			pm := s.reconciler.GetPositionManager()
			if pm != nil {
				snap := pm.GetSnapshot()
				totalQty := decimal.Zero
				for _, slot := range snap.Slots {
					if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
						totalQty = totalQty.Add(pbu.ToGoDecimal(slot.PositionQty))
					}
				}
				notional = totalQty.Mul(stats.LastPrice).Abs()
			}
		}
		stats.mu.RUnlock()

		if !notional.IsZero() {
			exposures = append(exposures, &pb.SymbolExposure{
				Symbol:        symbol,
				NotionalValue: pbu.FromGoDecimal(notional),
			})
			totalNotional = totalNotional.Add(notional)
		}
	}

	// Calculate percentages
	if !totalNotional.IsZero() {
		for _, exp := range exposures {
			notional := pbu.ToGoDecimal(exp.NotionalValue)
			exp.PercentageOfPortfolio = pbu.FromGoDecimal(notional.Div(totalNotional).Mul(decimal.NewFromInt(100)))
		}
	}

	return &pb.GetExposureResponse{
		TotalNotional:      pbu.FromGoDecimal(totalNotional),
		TotalUnrealizedPnl: pbu.FromGoDecimal(decimal.Zero),
		PortfolioLeverage:  pbu.FromGoDecimal(decimal.Zero),
		Exposures:          exposures,
	}, nil
}

func (s *RiskServiceServer) SubscribeRiskAlerts(req *pb.SubscribeRiskAlertsRequest, stream pb.RiskService_SubscribeRiskAlertsServer) error {
	alerts := make(chan *pb.RiskAlert, 100)
	s.monitor.Subscribe(alerts)
	defer s.monitor.Unsubscribe(alerts)

	if err := stream.Send(&pb.RiskAlert{
		Message:   "Connected to Risk Service",
		Severity:  "info",
		Timestamp: time.Now().Unix(),
	}); err != nil {
		return err
	}

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case alert := <-alerts:
			if err := stream.Send(alert); err != nil {
				return err
			}
		}
	}
}

func (s *RiskServiceServer) TriggerReconciliation(ctx context.Context, req *pb.TriggerReconciliationRequest) (*pb.TriggerReconciliationResponse, error) {
	if s.reconciler == nil {
		return nil, status.Error(codes.Unavailable, "reconciler not configured")
	}

	if req.Symbol != "" && req.Symbol != s.reconciler.symbol {
		return nil, status.Errorf(codes.InvalidArgument, "reconciler only supports symbol %s", s.reconciler.symbol)
	}

	err := s.reconciler.TriggerManual(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to trigger reconciliation: %v", err)
	}

	st := s.reconciler.GetStatus()
	return &pb.TriggerReconciliationResponse{
		ReconciliationId: st.ReconciliationId,
		Status:           st.Status,
	}, nil
}

func (s *RiskServiceServer) GetReconciliationStatus(ctx context.Context, req *pb.GetReconciliationStatusRequest) (*pb.GetReconciliationStatusResponse, error) {
	if s.reconciler == nil {
		return nil, status.Error(codes.Unavailable, "reconciler not configured")
	}

	st := s.reconciler.GetStatus()
	if req.ReconciliationId != "" && req.ReconciliationId != st.ReconciliationId {
		return nil, status.Errorf(codes.NotFound, "reconciliation history not found (only latest is kept)")
	}

	return st, nil
}

func (s *RiskServiceServer) GetCircuitBreakerStatus(ctx context.Context, req *pb.GetCircuitBreakerStatusRequest) (*pb.GetCircuitBreakerStatusResponse, error) {
	if s.circuitBreaker == nil {
		return nil, status.Error(codes.Unavailable, "circuit breaker not configured")
	}

	st := s.circuitBreaker.GetStatus()
	return &pb.GetCircuitBreakerStatusResponse{
		Status: st,
	}, nil
}

func (s *RiskServiceServer) OpenCircuitBreaker(ctx context.Context, req *pb.OpenCircuitBreakerRequest) (*pb.OpenCircuitBreakerResponse, error) {
	if s.circuitBreaker == nil {
		return nil, status.Error(codes.Unavailable, "circuit breaker not configured")
	}

	err := s.circuitBreaker.Open(req.Symbol, req.Reason)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to open circuit breaker: %v", err)
	}

	return &pb.OpenCircuitBreakerResponse{Success: true}, nil
}

func (s *RiskServiceServer) CloseCircuitBreaker(ctx context.Context, req *pb.CloseCircuitBreakerRequest) (*pb.CloseCircuitBreakerResponse, error) {
	if s.circuitBreaker == nil {
		return nil, status.Error(codes.Unavailable, "circuit breaker not configured")
	}

	s.circuitBreaker.Reset()
	return &pb.CloseCircuitBreakerResponse{Success: true}, nil
}
