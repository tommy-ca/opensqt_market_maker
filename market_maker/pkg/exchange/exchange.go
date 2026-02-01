// Package exchange provides reusable cryptocurrency exchange connectors
package exchange

import (
	"context"

	"market_maker/internal/pb"

	"github.com/shopspring/decimal"
)

// Exchange defines the interface for cryptocurrency exchanges
// This is the public interface used by both market_maker and live_server binaries
type Exchange interface {
	// Identity
	GetName() string
	CheckHealth(ctx context.Context) error

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
	GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error)
	FetchExchangeInfo(ctx context.Context, symbol string) error
	GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error)

	// Contract info
	GetPriceDecimals() int
	GetQuantityDecimals() int
	GetBaseAsset() string
	GetQuoteAsset() string
}

// Adapter wraps core.IExchange to implement pkg.Exchange interface
// This allows internal/exchange implementations to be used as pkg/exchange.Exchange
type Adapter struct {
	impl interface {
		GetName() string
		CheckHealth(ctx context.Context) error
		PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error)
		BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool)
		CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error
		BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error
		CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error
		GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error)
		GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error)
		GetAccount(ctx context.Context) (*pb.Account, error)
		GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error)
		GetBalance(ctx context.Context, asset string) (decimal.Decimal, error)
		StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error
		StopOrderStream() error
		StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error
		StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error
		StopKlineStream() error
		StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error
		StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error
		GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
		GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error)
		FetchExchangeInfo(ctx context.Context, symbol string) error
		GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error)
		GetPriceDecimals() int
		GetQuantityDecimals() int
		GetBaseAsset() string
		GetQuoteAsset() string
	}
}

// NewAdapter wraps an internal exchange implementation
func NewAdapter(impl interface{}) Exchange {
	// Type assertion to ensure impl has all required methods
	adapter := &Adapter{impl: impl.(interface {
		GetName() string
		CheckHealth(ctx context.Context) error
		PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error)
		BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool)
		CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error
		BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error
		CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error
		GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error)
		GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error)
		GetAccount(ctx context.Context) (*pb.Account, error)
		GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error)
		GetBalance(ctx context.Context, asset string) (decimal.Decimal, error)
		StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error
		StopOrderStream() error
		StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error
		StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error
		StopKlineStream() error
		StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error
		StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error
		GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
		GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error)
		FetchExchangeInfo(ctx context.Context, symbol string) error
		GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error)
		GetPriceDecimals() int
		GetQuantityDecimals() int
		GetBaseAsset() string
		GetQuoteAsset() string
	})}
	return adapter
}

// Implement Exchange interface by delegating to impl

func (a *Adapter) GetName() string {
	return a.impl.GetName()
}

func (a *Adapter) CheckHealth(ctx context.Context) error {
	return a.impl.CheckHealth(ctx)
}

func (a *Adapter) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	return a.impl.PlaceOrder(ctx, req)
}

func (a *Adapter) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	return a.impl.BatchPlaceOrders(ctx, orders)
}

func (a *Adapter) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	return a.impl.CancelOrder(ctx, symbol, orderID, useMargin)
}

func (a *Adapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	return a.impl.BatchCancelOrders(ctx, symbol, orderIDs, useMargin)
}

func (a *Adapter) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	return a.impl.CancelAllOrders(ctx, symbol, useMargin)
}

func (a *Adapter) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	return a.impl.GetOrder(ctx, symbol, orderID, clientOrderID, useMargin)
}

func (a *Adapter) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	return a.impl.GetOpenOrders(ctx, symbol, useMargin)
}

func (a *Adapter) GetAccount(ctx context.Context) (*pb.Account, error) {
	return a.impl.GetAccount(ctx)
}

func (a *Adapter) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	return a.impl.GetPositions(ctx, symbol)
}

func (a *Adapter) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	return a.impl.GetBalance(ctx, asset)
}

func (a *Adapter) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	return a.impl.StartOrderStream(ctx, callback)
}

func (a *Adapter) StopOrderStream() error {
	return a.impl.StopOrderStream()
}

func (a *Adapter) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	return a.impl.StartPriceStream(ctx, symbols, callback)
}

func (a *Adapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	return a.impl.StartKlineStream(ctx, symbols, interval, callback)
}

func (a *Adapter) StopKlineStream() error {
	return a.impl.StopKlineStream()
}

func (a *Adapter) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return a.impl.GetLatestPrice(ctx, symbol)
}

func (a *Adapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	return a.impl.GetHistoricalKlines(ctx, symbol, interval, limit)
}

func (a *Adapter) FetchExchangeInfo(ctx context.Context, symbol string) error {
	return a.impl.FetchExchangeInfo(ctx, symbol)
}

func (a *Adapter) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
	return a.impl.GetSymbolInfo(ctx, symbol)
}

func (a *Adapter) GetPriceDecimals() int {
	return a.impl.GetPriceDecimals()
}

func (a *Adapter) GetQuantityDecimals() int {
	return a.impl.GetQuantityDecimals()
}

func (a *Adapter) GetBaseAsset() string {
	return a.impl.GetBaseAsset()
}

func (a *Adapter) GetQuoteAsset() string {
	return a.impl.GetQuoteAsset()
}

func (a *Adapter) StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error {
	return a.impl.StartAccountStream(ctx, callback)
}

func (a *Adapter) StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error {
	return a.impl.StartPositionStream(ctx, callback)
}
