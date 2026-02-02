package durable

import (
	"context"
	"market_maker/internal/pb"
)

// State represents the durable state of the trading system
type State struct {
	Slots map[string]*pb.InventorySlot
	// We can add RiskStatus, LastPrice, etc.
	LastPrice      float64
	LastUpdateTime int64
}

// Store defines the interface for state persistence
type Store interface {
	SaveState(ctx context.Context, state *State) error
	LoadState(ctx context.Context) (*State, error)
}

// Engine defines the interface for the workflow engine
type Engine interface {
	Start(ctx context.Context) error
	Stop() error
	OnPriceUpdate(ctx context.Context, price *pb.PriceChange) error
	OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error
}
