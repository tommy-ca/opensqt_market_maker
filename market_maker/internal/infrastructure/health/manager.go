package health

import (
	"market_maker/internal/core"
	"sync"
)

// HealthManager aggregates health status from different components
type HealthManager struct {
	logger core.ILogger
	mu     sync.RWMutex
	checks map[string]func() error
}

// NewHealthManager creates a new health manager
func NewHealthManager(logger core.ILogger) *HealthManager {
	if logger == nil {
		return &HealthManager{
			checks: make(map[string]func() error),
		}
	}
	return &HealthManager{
		logger: logger.WithField("component", "health_manager"),
		checks: make(map[string]func() error),
	}
}

// Register adds a new health check for a component
func (hm *HealthManager) Register(component string, check func() error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.checks[component] = check
}

// GetStatus returns the current status of all registered components
func (hm *HealthManager) GetStatus() map[string]string {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	status := make(map[string]string)
	for component, check := range hm.checks {
		if err := check(); err != nil {
			status[component] = "Unhealthy: " + err.Error()
		} else {
			status[component] = "Healthy"
		}
	}
	return status
}

// IsHealthy returns true if all critical components are healthy
func (hm *HealthManager) IsHealthy() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	for _, check := range hm.checks {
		if err := check(); err != nil {
			return false
		}
	}
	return true
}
