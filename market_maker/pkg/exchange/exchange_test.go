package exchange

import (
	"context"
	"testing"

	"market_maker/internal/core"
	"market_maker/internal/mock"

	"github.com/stretchr/testify/assert"
)

// TestAdapterImplementsInterface verifies the Adapter implements Exchange interface
func TestAdapterImplementsInterface(t *testing.T) {
	var _ Exchange = (*Adapter)(nil)
}

// TestAdapterWrapsIExchange verifies Adapter can wrap core.IExchange
func TestAdapterWrapsIExchange(t *testing.T) {
	// Create a mock exchange that implements core.IExchange
	mockExch := mock.NewMockExchange("test")

	// Wrap it with adapter
	adapter := NewAdapter(mockExch)

	// Verify it implements Exchange interface
	var _ Exchange = adapter

	// Verify methods are delegated correctly
	assert.Equal(t, "test", adapter.GetName())
}

// TestAdapterDelegatesGetName verifies GetName is delegated
func TestAdapterDelegatesGetName(t *testing.T) {
	mockExch := mock.NewMockExchange("binance")
	adapter := NewAdapter(mockExch)

	assert.Equal(t, "binance", adapter.GetName())
}

// TestAdapterDelegatesCheckHealth verifies CheckHealth is delegated
func TestAdapterDelegatesCheckHealth(t *testing.T) {
	mockExch := mock.NewMockExchange("test")
	adapter := NewAdapter(mockExch)

	err := adapter.CheckHealth(context.Background())
	assert.NoError(t, err)
}

// TestAdapterDelegatesPriceDecimals verifies GetPriceDecimals is delegated
func TestAdapterDelegatesPriceDecimals(t *testing.T) {
	mockExch := mock.NewMockExchange("test")
	adapter := NewAdapter(mockExch)

	decimals := adapter.GetPriceDecimals()
	assert.Equal(t, 2, decimals) // mock exchange returns 2
}

// TestNewAdapterAcceptsIExchange verifies NewAdapter works with core.IExchange
func TestNewAdapterAcceptsIExchange(t *testing.T) {
	var iexch core.IExchange = mock.NewMockExchange("test")

	// This should compile and work
	adapter := NewAdapter(iexch)

	assert.NotNil(t, adapter)
	assert.Equal(t, "test", adapter.GetName())
}
