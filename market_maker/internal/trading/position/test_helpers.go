package position

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/pb"

	"github.com/shopspring/decimal"
)

// Mock implementations for testing

type mockRiskMonitor struct {
	triggered bool
	vol       float64
}

func (m *mockRiskMonitor) Start(ctx context.Context) error                { return nil }
func (m *mockRiskMonitor) Stop() error                                    { return nil }
func (m *mockRiskMonitor) IsTriggered() bool                              { return m.triggered }
func (m *mockRiskMonitor) GetVolatilityFactor(symbol string) float64      { return m.vol }
func (m *mockRiskMonitor) CheckHealth() error                             { return nil }
func (m *mockRiskMonitor) GetATR(symbol string) decimal.Decimal           { return decimal.Zero }
func (m *mockRiskMonitor) GetAllSymbols() []string                        { return nil }
func (m *mockRiskMonitor) GetMetrics(symbol string) *pb.SymbolRiskMetrics { return nil }
func (m *mockRiskMonitor) Reset() error                                   { return nil }

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, f ...interface{})               {}
func (m *mockLogger) Info(msg string, f ...interface{})                {}
func (m *mockLogger) Warn(msg string, f ...interface{})                {}
func (m *mockLogger) Error(msg string, f ...interface{})               {}
func (m *mockLogger) Fatal(msg string, f ...interface{})               {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }
