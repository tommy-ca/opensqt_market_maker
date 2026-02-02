package alert

import (
	"context"
	"market_maker/internal/core"
	"sync"
	"testing"
	"time"
)

type mockAlertChannel struct {
	name     string
	sent     []AlertPayload
	sendFunc func(ctx context.Context, alert AlertPayload) error
	mu       sync.Mutex
}

func (m *mockAlertChannel) Name() string {
	return m.name
}

func (m *mockAlertChannel) Send(ctx context.Context, alert AlertPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, alert)
	if m.sendFunc != nil {
		return m.sendFunc(ctx, alert)
	}
	return nil
}

func (m *mockAlertChannel) getSent() []AlertPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy to avoid race on slice elements if they were mutable
	res := make([]AlertPayload, len(m.sent))
	copy(res, m.sent)
	return res
}

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, f ...interface{})               {}
func (m *mockLogger) Info(msg string, f ...interface{})                {}
func (m *mockLogger) Warn(msg string, f ...interface{})                {}
func (m *mockLogger) Error(msg string, f ...interface{})               {}
func (m *mockLogger) Fatal(msg string, f ...interface{})               {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }

func TestAlertManager_Alert(t *testing.T) {
	am := NewAlertManager(&mockLogger{})

	ch1 := &mockAlertChannel{name: "mock1"}
	ch2 := &mockAlertChannel{name: "mock2"}

	am.AddChannel(ch1)
	am.AddChannel(ch2)

	am.Alert(context.Background(), "Test Alert", "This is a test", Info, map[string]string{"key": "value"})

	// Wait for goroutines (Alert is async)
	time.Sleep(100 * time.Millisecond)

	sent1 := ch1.getSent()
	sent2 := ch2.getSent()

	if len(sent1) != 1 {
		t.Errorf("Expected ch1 to receive 1 alert, got %d", len(sent1))
	}
	if len(sent2) != 1 {
		t.Errorf("Expected ch2 to receive 1 alert, got %d", len(sent2))
	}

	payload := sent1[0]
	if payload.Title != "Test Alert" {
		t.Errorf("Expected title 'Test Alert', got '%s'", payload.Title)
	}
	if payload.Level != Info {
		t.Errorf("Expected level INFO, got %s", payload.Level)
	}
	if payload.Fields["key"] != "value" {
		t.Errorf("Expected field key=value, got %s", payload.Fields["key"])
	}
}
