package pbu

import (
	"fmt"
	"github.com/shopspring/decimal"
	googletype "google.golang.org/genproto/googleapis/type/decimal"
	"strings"
)

// ToGoDecimal converts a google.type.Decimal to a shopspring decimal.Decimal.
// ... (rest of the file)
// ...

// GenerateDeterministicOrderID generates a stable ClientOrderID for a given grid level.
// It is based on (StrategyID, Price, Side) to prevent double-fills on restart.
func GenerateDeterministicOrderID(strategyID string, price decimal.Decimal, side string, priceDecimals int) string {
	priceInt := price.Mul(decimal.NewFromFloat(10).Pow(decimal.NewFromInt(int64(priceDecimals)))).Round(0).IntPart()

	sideCode := "B"
	if side == "SELL" {
		sideCode = "S"
	}

	return fmt.Sprintf("%s_%d_%s", strategyID, priceInt, sideCode)
}

// GenerateCompactOrderID generates a compact ClientOrderID (< 18 chars)
// Deprecated: Use GenerateDeterministicOrderID for grid trading.
func GenerateCompactOrderID(price decimal.Decimal, side string, priceDecimals int) string {
	return GenerateDeterministicOrderID("G", price, side, priceDecimals)
}

// AddBrokerPrefix prepends broker-specific prefixes for commission tracking
func AddBrokerPrefix(exchangeName, clientOID string) string {
	switch strings.ToLower(exchangeName) {
	case "binance":
		prefix := "x-zdfVM8vY"
		return truncateID(prefix+clientOID, 36)
	case "gate":
		prefix := "t-"
		return truncateID(prefix+clientOID, 30)
	default:
		return clientOID
	}
}

func truncateID(id string, maxLen int) string {
	if len(id) > maxLen {
		return id[:maxLen]
	}
	return id
}

// ParseCompactOrderID reconstructs price and side from a compact ClientOrderID.
func ParseCompactOrderID(clientOID string, priceDecimals int) (decimal.Decimal, string, bool) {
	// Remove prefixes if any
	oid := clientOID
	if strings.HasPrefix(oid, "x-zdfVM8vY") {
		oid = strings.TrimPrefix(oid, "x-zdfVM8vY")
	} else if strings.HasPrefix(oid, "t-") {
		oid = strings.TrimPrefix(oid, "t-")
	}

	parts := strings.Split(oid, "_")
	if len(parts) != 3 {
		return decimal.Zero, "", false
	}

	// Format is {strategyID}_{priceInt}_{sideCode}
	priceInt, err := decimal.NewFromString(parts[1])
	if err != nil {
		return decimal.Zero, "", false
	}

	price := priceInt.Div(decimal.NewFromFloat(10).Pow(decimal.NewFromInt(int64(priceDecimals))))

	side := "BUY"
	if parts[2] == "S" {
		side = "SELL"
	}

	return price, side, true
}
func ToGoDecimal(d *googletype.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	val, err := decimal.NewFromString(d.Value)
	if err != nil {
		return decimal.Zero
	}
	return val
}

// FromGoDecimal converts a shopspring decimal.Decimal to a google.type.Decimal.
func FromGoDecimal(d decimal.Decimal) *googletype.Decimal {
	return &googletype.Decimal{
		Value: d.String(),
	}
}
