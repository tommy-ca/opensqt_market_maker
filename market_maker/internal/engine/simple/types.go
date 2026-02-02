package simple

import (
	"context"
	"market_maker/internal/pb"
)

// Store defines the interface for state persistence
type Store interface {
	SaveState(ctx context.Context, state *pb.State) error
	LoadState(ctx context.Context) (*pb.State, error)
}
