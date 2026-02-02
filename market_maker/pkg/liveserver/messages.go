package liveserver

// Message represents a WebSocket message
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// MessageType constants
const (
	TypeKline      = "kline"
	TypeAccount    = "account"
	TypeOrders     = "orders"
	TypeTradeEvent = "trade_event"
	TypePosition   = "position"
	TypeHistory    = "history"
	TypeRiskStatus = "risk_status"
	TypeSlots      = "slots"
)
