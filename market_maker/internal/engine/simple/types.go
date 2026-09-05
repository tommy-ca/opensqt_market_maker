package simple

import (
	"market_maker/internal/core"
)

// Store defines the interface for state persistence
// Deprecated: Use core.IStateStore instead
type Store = core.IStateStore
