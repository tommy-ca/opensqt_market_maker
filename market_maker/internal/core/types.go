package core

import (
	"github.com/shopspring/decimal"
)

// TargetPosition represents a desired position for a specific venue and symbol
type TargetPosition struct {
	Exchange string
	Symbol   string
	Size     decimal.Decimal
}

// TargetOrder represents a desired active order
type TargetOrder struct {
	Exchange      string
	Symbol        string
	Price         decimal.Decimal
	Quantity      decimal.Decimal
	Side          string // "BUY", "SELL"
	Type          string // "LIMIT", "MARKET", "IOC", "FOK"
	ClientOrderID string
	ReduceOnly    bool
	PostOnly      bool
}

// TargetState represents the holistic desired state for a strategy
type TargetState struct {
	Timestamp int64
	Positions []TargetPosition
	Orders    []TargetOrder
}
