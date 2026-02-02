package safety

import (
	"context"
	"testing"

	"market_maker/internal/core"
	"market_maker/internal/mock"

	"github.com/shopspring/decimal"
)

func TestSafetyChecker_CheckAccountSafety(t *testing.T) {
	// Create mock exchange
	exchange := mock.NewMockExchange("test_exchange")

	// Create safety checker with mock logger
	logger := &mockLogger{}
	checker := NewSafetyChecker(logger)

	// Test parameters
	ctx := context.Background()
	symbol := "BTCUSDT"
	currentPrice := decimal.NewFromFloat(45000.0)
	orderAmount := decimal.NewFromFloat(30.0)
	priceInterval := decimal.NewFromFloat(100.0) // Big interval for success
	feeRate := decimal.NewFromFloat(0.0002)      // 0.02%
	requiredPositions := 10
	priceDecimals := 2

	// Test successful safety check
	err := checker.CheckAccountSafety(
		ctx, exchange, symbol, currentPrice,
		orderAmount, priceInterval, feeRate, requiredPositions, priceDecimals,
	)

	if err != nil {
		t.Fatalf("Safety check failed unexpectedly: %v", err)
	}

	// Test profitability failure
	smallInterval := decimal.NewFromFloat(1.0) // Interval too small for fees
	err = checker.CheckAccountSafety(
		ctx, exchange, symbol, currentPrice,
		orderAmount, smallInterval, feeRate, requiredPositions, priceDecimals,
	)

	if err == nil {
		t.Error("Expected profitability check to fail, but it passed")
	}
}

func TestSafetyChecker_ValidateTradingParameters(t *testing.T) {
	logger := &mockLogger{}
	checker := NewSafetyChecker(logger)

	tests := []struct {
		name           string
		symbol         string
		priceInterval  float64
		orderQuantity  float64
		minOrderValue  float64
		buyWindowSize  int
		sellWindowSize int
		expectError    bool
	}{
		{
			name:           "valid parameters",
			symbol:         "BTCUSDT",
			priceInterval:  1.0,
			orderQuantity:  30.0,
			minOrderValue:  5.0,
			buyWindowSize:  10,
			sellWindowSize: 10,
			expectError:    false,
		},
		{
			name:           "empty symbol",
			symbol:         "",
			priceInterval:  1.0,
			orderQuantity:  30.0,
			minOrderValue:  5.0,
			buyWindowSize:  10,
			sellWindowSize: 10,
			expectError:    true,
		},
		{
			name:           "negative price interval",
			symbol:         "BTCUSDT",
			priceInterval:  -1.0,
			orderQuantity:  30.0,
			minOrderValue:  5.0,
			buyWindowSize:  10,
			sellWindowSize: 10,
			expectError:    true,
		},
		{
			name:           "zero order quantity",
			symbol:         "BTCUSDT",
			priceInterval:  1.0,
			orderQuantity:  0.0,
			minOrderValue:  5.0,
			buyWindowSize:  10,
			sellWindowSize: 10,
			expectError:    true,
		},
		{
			name:           "large window sizes",
			symbol:         "BTCUSDT",
			priceInterval:  1.0,
			orderQuantity:  30.0,
			minOrderValue:  5.0,
			buyWindowSize:  150,
			sellWindowSize: 150,
			expectError:    false, // Should allow but warn
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checker.ValidateTradingParameters(
				tt.symbol,
				decimal.NewFromFloat(tt.priceInterval),
				decimal.NewFromFloat(tt.orderQuantity),
				decimal.NewFromFloat(tt.minOrderValue),
				tt.buyWindowSize,
				tt.sellWindowSize,
			)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

// mockLogger implements core.ILogger for testing
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...interface{})               {}
func (m *mockLogger) Info(msg string, fields ...interface{})                {}
func (m *mockLogger) Warn(msg string, fields ...interface{})                {}
func (m *mockLogger) Error(msg string, fields ...interface{})               {}
func (m *mockLogger) Fatal(msg string, fields ...interface{})               {}
func (m *mockLogger) WithField(key string, value interface{}) core.ILogger  { return m }
func (m *mockLogger) WithFields(fields map[string]interface{}) core.ILogger { return m }
