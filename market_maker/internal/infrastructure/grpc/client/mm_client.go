package client

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// MarketMakerClient provides a high-level wrapper for market_maker gRPC services
type MarketMakerClient struct {
	conn           *grpc.ClientConn
	riskClient     pb.RiskServiceClient
	positionClient pb.PositionServiceClient
	logger         core.ILogger
	target         string
}

// NewMarketMakerClient creates a new gRPC client for the market maker
func NewMarketMakerClient(target string, logger core.ILogger) (*MarketMakerClient, error) {
	// For now, using insecure credentials. TLS can be added later via configuration.
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to dial gRPC server at %s: %w", target, err)
	}

	return &MarketMakerClient{
		conn:           conn,
		riskClient:     pb.NewRiskServiceClient(conn),
		positionClient: pb.NewPositionServiceClient(conn),
		logger:         logger.WithField("component", "mm_grpc_client").WithField("target", target),
		target:         target,
	}, nil
}

// Close closes the underlying gRPC connection
func (c *MarketMakerClient) Close() error {
	return c.conn.Close()
}

// SubscribePositions streams position updates from the market maker with automatic reconnection
func (c *MarketMakerClient) SubscribePositions(ctx context.Context, symbols []string, callback func(*pb.PositionUpdate)) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err := c.runPositionStream(ctx, symbols, callback)
				if err != nil {
					c.logger.Error("Position stream disconnected, retrying...", "error", err)
					time.Sleep(2 * time.Second)
				}
			}
		}
	}()
}

func (c *MarketMakerClient) runPositionStream(ctx context.Context, symbols []string, callback func(*pb.PositionUpdate)) error {
	stream, err := c.positionClient.SubscribePositions(ctx, &pb.PositionServiceSubscribePositionsRequest{
		Symbols: symbols,
	})
	if err != nil {
		return err
	}

	for {
		update, err := stream.Recv()
		if err != nil {
			return err
		}
		callback(update)
	}
}

// SubscribeRiskAlerts streams risk alerts from the market maker with automatic reconnection
func (c *MarketMakerClient) SubscribeRiskAlerts(ctx context.Context, callback func(*pb.RiskAlert)) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err := c.runRiskStream(ctx, callback)
				if err != nil {
					c.logger.Error("Risk stream disconnected, retrying...", "error", err)
					time.Sleep(2 * time.Second)
				}
			}
		}
	}()
}

func (c *MarketMakerClient) runRiskStream(ctx context.Context, callback func(*pb.RiskAlert)) error {
	stream, err := c.riskClient.SubscribeRiskAlerts(ctx, &pb.SubscribeRiskAlertsRequest{})
	if err != nil {
		return err
	}

	for {
		alert, err := stream.Recv()
		if err != nil {
			return err
		}
		callback(alert)
	}
}

// GetRiskMetrics fetches current risk metrics
func (c *MarketMakerClient) GetRiskMetrics(ctx context.Context, symbols []string) ([]*pb.SymbolRiskMetrics, error) {
	resp, err := c.riskClient.GetRiskMetrics(ctx, &pb.GetRiskMetricsRequest{Symbols: symbols})
	if err != nil {
		return nil, err
	}
	return resp.Metrics, nil
}

// GetOpenOrders fetches open orders from the engine's perspective
func (c *MarketMakerClient) GetOpenOrders(ctx context.Context, symbols []string) ([]*pb.Order, error) {
	resp, err := c.positionClient.GetOpenOrders(ctx, &pb.PositionServiceGetOpenOrdersRequest{Symbols: symbols})
	if err != nil {
		return nil, err
	}
	return resp.Orders, nil
}
