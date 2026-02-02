package portfolio

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
	gdecimal "google.golang.org/genproto/googleapis/type/decimal"
)

type PortfolioServiceServer struct {
	pb.UnimplementedPortfolioServiceServer
	controller *PortfolioController
	marginSim  core.IMarginSimulator
}

func NewPortfolioServiceServer(controller *PortfolioController, marginSim core.IMarginSimulator) *PortfolioServiceServer {
	return &PortfolioServiceServer{
		controller: controller,
		marginSim:  marginSim,
	}
}

func (s *PortfolioServiceServer) GetRiskProfile(ctx context.Context, req *pb.GetRiskProfileRequest) (*pb.RiskProfile, error) {
	p := s.marginSim.GetRiskProfile()
	return &pb.RiskProfile{
		AdjustedEquity:         pbu.FromGoDecimal(p.AdjustedEquity),
		TotalMaintenanceMargin: pbu.FromGoDecimal(p.TotalMaintenanceMargin),
		AvailableHeadroom:      pbu.FromGoDecimal(p.AvailableHeadroom),
		HealthScore:            pbu.FromGoDecimal(p.HealthScore),
		IsUnified:              p.IsUnified,
	}, nil
}

func (s *PortfolioServiceServer) SimulateMargin(ctx context.Context, req *pb.SimulateMarginRequest) (*pb.SimulateMarginResponse, error) {
	proposals := make(map[string]decimal.Decimal)
	for sym, val := range req.Proposals {
		proposals[sym] = pbu.ToGoDecimal(val)
	}

	health := s.marginSim.SimulateImpact(proposals)

	// Simplified liquidation check: health < 0.1
	wouldLiquidate := health.LessThan(decimal.NewFromFloat(0.1))

	return &pb.SimulateMarginResponse{
		ProjectedHealthScore: pbu.FromGoDecimal(health),
		WouldLiquidate:       wouldLiquidate,
	}, nil
}

func (s *PortfolioServiceServer) GetTargetPositions(ctx context.Context, req *pb.GetTargetPositionsRequest) (*pb.GetTargetPositionsResponse, error) {
	targets := s.controller.GetLastTargets()
	pbTargets := make([]*pb.TargetPosition, len(targets))
	for i, t := range targets {
		pbTargets[i] = &pb.TargetPosition{
			Symbol:       t.Symbol,
			Weight:       pbu.FromGoDecimal(t.Weight),
			Notional:     pbu.FromGoDecimal(t.Notional),
			Exchange:     t.Exchange,
			QualityScore: pbu.FromGoDecimal(t.QualityScore),
		}
	}
	return &pb.GetTargetPositionsResponse{Targets: pbTargets}, nil
}

func (s *PortfolioServiceServer) GetMarketScores(ctx context.Context, req *pb.GetMarketScoresRequest) (*pb.GetMarketScoresResponse, error) {
	opps := s.controller.GetLastOpps()
	pbOpps := make([]*pb.PortfolioOpportunity, len(opps))
	for i, o := range opps {
		pbOpps[i] = &pb.PortfolioOpportunity{
			Symbol:        o.Symbol,
			Strategy:      o.Strategy,
			LongExchange:  o.LongExchange,
			ShortExchange: o.ShortExchange,
			Spread:        pbu.FromGoDecimal(o.Spread),
			SpreadApr:     pbu.FromGoDecimal(o.SpreadAPR),
			Basis:         pbu.FromGoDecimal(o.Basis),
			QualityScore:  pbu.FromGoDecimal(o.QualityScore),
			Metrics: &pb.FundingMetrics{
				Sma_1D:           pbu.FromGoDecimal(o.Metrics.SMA1d),
				Sma_7D:           pbu.FromGoDecimal(o.Metrics.SMA7d),
				Sma_30D:          pbu.FromGoDecimal(o.Metrics.SMA30d),
				StabilityScore:   pbu.FromGoDecimal(o.Metrics.StabilityScore),
				VolatilityScore:  pbu.FromGoDecimal(o.Metrics.VolatilityScore),
				Momentum:         pbu.FromGoDecimal(o.Metrics.Momentum),
				OiFactor:         pbu.FromGoDecimal(o.Metrics.OIFactor),
				PositiveRatio:    pbu.FromGoDecimal(o.Metrics.PositiveRatio),
				NumSignFlips:     int32(o.Metrics.NumSignFlips),
				CurrentDuration:  int32(o.Metrics.CurrentDuration),
				AverageAnnualApr: pbu.FromGoDecimal(o.Metrics.AverageAnnualAPR),
				NarrativeSector:  o.Metrics.NarrativeSector,
			},
			Timestamp: o.Timestamp.UnixMilli(),
		}
	}
	return &pb.GetMarketScoresResponse{Opportunities: pbOpps}, nil
}

func (s *PortfolioServiceServer) toPbDecimal(d decimal.Decimal) *gdecimal.Decimal {
	return pbu.FromGoDecimal(d)
}
