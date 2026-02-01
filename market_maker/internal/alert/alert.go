package alert

import (
	"context"
	"sync"
	"time"

	"market_maker/internal/core"
)

type AlertLevel string

const (
	Info     AlertLevel = "INFO"
	Warning  AlertLevel = "WARNING"
	Error    AlertLevel = "ERROR"
	Critical AlertLevel = "CRITICAL"
)

type AlertPayload struct {
	Level     AlertLevel
	Title     string
	Message   string
	Timestamp time.Time
	Fields    map[string]string
}

type AlertChannel interface {
	Send(ctx context.Context, alert AlertPayload) error
	Name() string
}

type AlertManager struct {
	channels []AlertChannel
	logger   core.ILogger
	mu       sync.RWMutex
}

func NewAlertManager(logger core.ILogger) *AlertManager {
	return &AlertManager{
		channels: make([]AlertChannel, 0),
		logger:   logger.WithField("component", "alert_manager"),
	}
}

func (am *AlertManager) AddChannel(ch AlertChannel) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.channels = append(am.channels, ch)
	am.logger.Info("Added alert channel", "name", ch.Name())
}

func (am *AlertManager) Alert(ctx context.Context, title, message string, level AlertLevel, fields map[string]string) {
	payload := AlertPayload{
		Level:     level,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Fields:    fields,
	}

	am.logger.Info("Triggering alert", "title", title, "level", level)

	am.mu.RLock()
	defer am.mu.RUnlock()

	var wg sync.WaitGroup
	for _, ch := range am.channels {
		wg.Add(1)
		go func(c AlertChannel) {
			defer wg.Done()
			// Create a timeout context for each channel
			timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if err := c.Send(timeoutCtx, payload); err != nil {
				am.logger.Error("Failed to send alert", "channel", c.Name(), "error", err)
			}
		}(ch)
	}
	// We don't wait here to avoid blocking the caller?
	// Or we wait? If critical, we might want to ensure delivery.
	// But usually alerting should be async to trading path.
	// Let's not wait.
}
