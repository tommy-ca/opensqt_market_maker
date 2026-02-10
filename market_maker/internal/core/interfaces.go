// Package core defines the core interfaces for the market maker system
package core

import (
	"context"
	"market_maker/internal/pb"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// StrategySlot represents the data required by the strategy logic for a single grid level
type StrategySlot struct {
	Price          decimal.Decimal
	PositionStatus pb.PositionStatus
	PositionQty    decimal.Decimal
	SlotStatus     pb.SlotStatus
	OrderSide      pb.OrderSide
	OrderPrice     decimal.Decimal
	OrderID        int64
}

// InventorySlot wraps the generated pb.InventorySlot with a mutex for runtime safety
type InventorySlot struct {
	*pb.InventorySlot
	Mu sync.RWMutex `json:"-"`

	// Cached decimal versions for hot path optimization
	PriceDec          decimal.Decimal `json:"-"`
	OrderPriceDec     decimal.Decimal `json:"-"`
	PositionQtyDec    decimal.Decimal `json:"-"`
	OriginalQtyDec    decimal.Decimal `json:"-"`
	OrderFilledQtyDec decimal.Decimal `json:"-"`
}

// OrderActionResult represents the result of applying an OrderAction
type OrderActionResult struct {
	Action *pb.OrderAction
	Order  *pb.Order // If PLACE succeeded
	Error  error
}

// IExchange defines the interface for cryptocurrency exchanges
type IExchange interface {
	// Identity
	GetName() string
	CheckHealth(ctx context.Context) error
	IsUnifiedMargin() bool

	// Order operations
	PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error)
	BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool)
	CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error
	BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error
	CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error
	GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error)
	GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error)

	// Account operations
	GetAccount(ctx context.Context) (*pb.Account, error)
	GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error)
	GetBalance(ctx context.Context, asset string) (decimal.Decimal, error)

	// WebSocket streams
	StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error
	StopOrderStream() error
	StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error
	StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error
	StopKlineStream() error
	StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error
	StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error

	// Funding Rate
	GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error)
	GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error)
	GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error)
	StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error

	// Market data
	GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
	GetTickers(ctx context.Context) ([]*pb.Ticker, error)
	GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error)
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error)
	FetchExchangeInfo(ctx context.Context, symbol string) error
	GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error)
	GetSymbols(ctx context.Context) ([]string, error)

	// Contract info
	GetPriceDecimals() int
	GetQuantityDecimals() int
	GetBaseAsset() string
	GetQuoteAsset() string
}

// IPriceMonitor defines the interface for price monitoring
type IPriceMonitor interface {
	Start(ctx context.Context) error
	Stop() error
	GetLatestPrice() (decimal.Decimal, error)
	GetLatestPriceChange() (*pb.PriceChange, error)
	SubscribePriceChanges() <-chan *pb.PriceChange
}

// IFundingMonitor defines the interface for funding rate monitoring
type IFundingMonitor interface {
	Start(ctx context.Context) error
	Stop() error
	GetRate(exchange, symbol string) (decimal.Decimal, error)
	GetNextFundingTime(exchange, symbol string) (time.Time, error)
	IsStale(exchange, symbol string, ttl time.Duration) bool
	Subscribe(exchange, symbol string) <-chan *pb.FundingUpdate
}

// IStrategy defines the interface for trading strategy logic
type IStrategy interface {
	CalculateActions(
		currentPrice decimal.Decimal,
		anchorPrice decimal.Decimal,
		atr decimal.Decimal,
		volatilityFactor float64,
		isRiskTriggered bool,
		regime pb.MarketRegime,
		slots []StrategySlot,
	) []*pb.OrderAction
}

// IPositionManager defines the interface for position management
type IPositionManager interface {
	Initialize(anchorPrice decimal.Decimal) error
	GetAnchorPrice() decimal.Decimal
	SetAnchorPrice(price decimal.Decimal)
	RestoreState(slots map[string]*pb.InventorySlot) error
	ApplyActionResults(results []OrderActionResult) error
	OnOrderUpdate(ctx context.Context, update *pb.OrderUpdate) error
	SyncOrders(orders []*pb.Order, exchangePosition decimal.Decimal)
	CancelAllBuyOrders(ctx context.Context) ([]*pb.OrderAction, error)
	CancelAllSellOrders(ctx context.Context) ([]*pb.OrderAction, error)
	GetSlots() map[string]*InventorySlot
	GetStrategySlots(target []StrategySlot) []StrategySlot
	GetSlotCount() int
	GetSnapshot() *pb.PositionManagerSnapshot
	UpdateOrderIndex(orderID int64, clientOID string, slot *InventorySlot)
	MarkSlotsPending(actions []*pb.OrderAction)
	ForceSync(ctx context.Context, symbol string, exchangeSize decimal.Decimal) error
	RestoreFromExchangePosition(totalPosition decimal.Decimal)
	OnUpdate(callback func(*pb.PositionUpdate))

	// Introspection API
	GetFills() []*pb.Fill
	GetOrderHistory() []*pb.Order
	GetPositionHistory() []*pb.PositionSnapshotData
	GetRealizedPnL() decimal.Decimal
}

// IOrderExecutor defines the interface for order execution
type IOrderExecutor interface {
	PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error)
	BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool)
	BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error
}

// ISafetyChecker defines the interface for safety checks
type ISafetyChecker interface {
	CheckAccountSafety(
		ctx context.Context,
		exchange IExchange,
		symbol string,
		currentPrice decimal.Decimal,
		orderAmount decimal.Decimal,
		priceInterval decimal.Decimal,
		feeRate decimal.Decimal,
		requiredPositions int,
		priceDecimals int,
	) error
}

// IRiskMonitor defines the interface for risk monitoring
type IRiskMonitor interface {
	Start(ctx context.Context) error
	Stop() error
	IsTriggered() bool
	GetVolatilityFactor(symbol string) float64
	GetATR(symbol string) decimal.Decimal
	GetAllSymbols() []string
	GetMetrics(symbol string) *pb.SymbolRiskMetrics
	Reset() error
}

// ICircuitBreaker defines the interface for risk-based circuit breakers
type ICircuitBreaker interface {
	IsTripped() bool
	RecordTrade(pnl decimal.Decimal)
	Reset()
	Open(symbol string, reason string) error
	GetStatus() *pb.CircuitBreakerStatus
}

// IReconciler defines the interface for position reconciliation
type IReconciler interface {
	Start(ctx context.Context) error
	Stop() error
	Reconcile(ctx context.Context) error
	GetStatus() *pb.GetReconciliationStatusResponse
	TriggerManual(ctx context.Context) error
}

// IOrderCleaner defines the interface for order cleanup
type IOrderCleaner interface {
	Start(ctx context.Context) error
	Stop() error
	Cleanup(ctx context.Context) error
}

// IHealthMonitor defines the interface for health monitoring
type IHealthMonitor interface {
	Register(component string, check func() error)
	GetStatus() map[string]string
	IsHealthy() bool
}

// IStateStore defines the interface for state persistence
type IStateStore interface {
	SaveState(ctx context.Context, state *pb.State) error
	LoadState(ctx context.Context) (*pb.State, error)
}

// ILogger defines the interface for logging
type ILogger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Fatal(msg string, fields ...interface{})
	WithField(key string, value interface{}) ILogger
	WithFields(fields map[string]interface{}) ILogger
}
