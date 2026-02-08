package pbu

import (
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
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
