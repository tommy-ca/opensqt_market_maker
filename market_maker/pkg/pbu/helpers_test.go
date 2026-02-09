package pbu

import (
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestGenerateDeterministicOrderID(t *testing.T) {
	price := decimal.NewFromFloat(100.5)
	side := "BUY"
	decimals := 2
	strategyID := "BTCG"

	oid1 := GenerateDeterministicOrderID(strategyID, price, side, decimals)
	oid2 := GenerateDeterministicOrderID(strategyID, price, side, decimals)

	assert.Equal(t, oid1, oid2, "Deterministic OID should be stable")
	assert.Contains(t, oid1, strategyID)
	assert.Contains(t, oid1, "10050")
	assert.Contains(t, oid1, "B")

	// Different price should give different OID
	oid3 := GenerateDeterministicOrderID(strategyID, decimal.NewFromInt(101), side, decimals)
	assert.NotEqual(t, oid1, oid3)

	// Different side should give different OID
	oid4 := GenerateDeterministicOrderID(strategyID, price, "SELL", decimals)
	assert.NotEqual(t, oid1, oid4)
}

func TestParseCompactOrderID(t *testing.T) {
	price := decimal.NewFromFloat(100.5)
	side := "SELL"
	decimals := 2
	strategyID := "BTCG"

	oid := GenerateDeterministicOrderID(strategyID, price, side, decimals)
	p, s, ok := ParseCompactOrderID(oid, decimals)

	assert.True(t, ok)
	assert.True(t, price.Equal(p))
	assert.Equal(t, side, s)
}

func TestAddBrokerPrefixTruncationSafety(t *testing.T) {
	// Long strategy ID that will cause truncation
	strategyID := "VeryLongStrategyNameThatWillCauseTruncation"
	price1 := decimal.NewFromFloat(100.5)
	price2 := decimal.NewFromFloat(100.6)
	side := "BUY"
	decimals := 2

	oid1 := GenerateDeterministicOrderID(strategyID, price1, side, decimals)
	oid2 := GenerateDeterministicOrderID(strategyID, price2, side, decimals)

	// They should be different
	assert.NotEqual(t, oid1, oid2)

	// Prepend prefix and truncate (original behavior)
	prefix := "x-zdfVM8vY"

	// Test the new AddBrokerPrefix

	b1 := AddBrokerPrefix("binance", oid1)
	b2 := AddBrokerPrefix("binance", oid2)

	assert.NotEqual(t, b1, b2, "Truncated IDs should still be unique")
	assert.True(t, len(b1) <= 36)
	assert.True(t, len(b2) <= 36)
	assert.True(t, strings.HasPrefix(b1, prefix))
	assert.True(t, strings.HasPrefix(b2, prefix))

	// Verify we can still parse them
	p1, s1, ok1 := ParseCompactOrderID(b1, decimals)
	assert.True(t, ok1)
	assert.True(t, price1.Equal(p1))
	assert.Equal(t, side, s1)

	p2, s2, ok2 := ParseCompactOrderID(b2, decimals)
	assert.True(t, ok2)
	assert.True(t, price2.Equal(p2))
	assert.Equal(t, side, s2)
}
