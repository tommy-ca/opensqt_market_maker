package simple

import (
	"context"
	"market_maker/internal/pb"
	"sync"
)

// MemoryStore implements Store in memory
type MemoryStore struct {
	state *pb.State
	mu    sync.RWMutex
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		state: nil,
	}
}

func (s *MemoryStore) SaveState(ctx context.Context, state *pb.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	return nil
}

func (s *MemoryStore) LoadState(ctx context.Context) (*pb.State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, nil
}
