package engine

import (
	"context"
	"encoding/json"
	"market_maker/internal/pb"
)

// Engine defines the unified interface for both simple and durable trading engines.
type Engine interface {
	Start(ctx context.Context) error
	Stop() error
	OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error
	OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error
	OnFundingUpdate(ctx context.Context, update *pb.FundingUpdate) error
	OnPositionUpdate(ctx context.Context, position *pb.Position) error
	OnAccountUpdate(ctx context.Context, account *pb.Account) error
}

// EngineFactory creates an engine instance from configuration
type EngineFactory interface {
	CreateEngine(symbol string, config json.RawMessage) (Engine, error)
}
