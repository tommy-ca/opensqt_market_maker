package mock

import (
	"context"
	"fmt"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MockExchange implements IExchange for testing
type MockExchange struct {
	name           string
	orders         map[int64]*pb.Order
	orderMapMu     sync.RWMutex
	orderIDCounter int64
	clientOrderMap map[string]int64
	account        *pb.Account
	positions      map[string][]*pb.Position
	mu             sync.RWMutex

	// Callbacks for streams
	priceCallbacks    []func(*pb.PriceChange)
	orderCallbacks    []func(*pb.OrderUpdate)
	klineCallbacks    []func(*pb.Candle)
	accountCallbacks  []func(*pb.Account)
	positionCallbacks []func(*pb.Position)

	isPriceStreamRunning    bool
	isOrderStreamRunning    bool
	isKlineStreamRunning    bool
	isAccountStreamRunning  bool
	isPositionStreamRunning bool

	// Mock Data Overrides
	fundingRates     map[string]decimal.Decimal
	histFundingRates map[string][]*pb.FundingRate
	tickers          map[string]*pb.Ticker
}

func NewMockExchange(name string) *MockExchange {
	return &MockExchange{
		name:           name,
		orders:         make(map[int64]*pb.Order),
		clientOrderMap: make(map[string]int64),
		account: &pb.Account{
			TotalWalletBalance: pbu.FromGoDecimal(decimal.NewFromFloat(10000.0)),
			AvailableBalance:   pbu.FromGoDecimal(decimal.NewFromFloat(10000.0)),
			AccountLeverage:    10,
		},
		positions:        make(map[string][]*pb.Position),
		orderIDCounter:   1000,
		fundingRates:     make(map[string]decimal.Decimal),
		histFundingRates: make(map[string][]*pb.FundingRate),
		tickers:          make(map[string]*pb.Ticker),
	}
}

func (m *MockExchange) SetHistoricalFundingRates(symbol string, rates []*pb.FundingRate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histFundingRates[symbol] = rates
}

func (m *MockExchange) SetTicker(ticker *pb.Ticker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickers[ticker.Symbol] = ticker
}

func (m *MockExchange) SetFundingRate(symbol string, rate decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fundingRates[symbol] = rate
}

func (m *MockExchange) SetPosition(symbol string, size decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.positions[symbol] = []*pb.Position{
		{
			Symbol:    symbol,
			Size:      pbu.FromGoDecimal(size),
			MarkPrice: pbu.FromGoDecimal(decimal.NewFromInt(100)),
		},
	}
}

func (m *MockExchange) GetName() string {
	return m.name
}

func (m *MockExchange) IsUnifiedMargin() bool {
	return false // Default to false for mock
}

func (m *MockExchange) CheckHealth(ctx context.Context) error {
	return nil
}

// PlaceOrder places an order into the mock exchange.
func (m *MockExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Idempotency: if client_order_id provided and already exists, return existing order
	if req.ClientOrderId != "" {
		if existingID, exists := m.clientOrderMap[req.ClientOrderId]; exists {
			if existingOrder, ok := m.orders[existingID]; ok {
				return existingOrder, nil
			}
		}
	}

	m.orderIDCounter++
	id := m.orderIDCounter

	executedQty := decimal.Zero
	status := pb.OrderStatus_ORDER_STATUS_NEW
	if req.Type == pb.OrderType_ORDER_TYPE_MARKET {
		executedQty = pbu.ToGoDecimal(req.Quantity)
		status = pb.OrderStatus_ORDER_STATUS_FILLED
	}

	order := &pb.Order{
		OrderId:       id,
		ClientOrderId: req.ClientOrderId,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Status:        status,
		Price:         req.Price,
		Quantity:      req.Quantity,
		ExecutedQty:   pbu.FromGoDecimal(executedQty),
		UpdateTime:    time.Now().UnixMilli(),
		CreatedAt:     timestamppb.Now(),
	}

	m.orders[id] = order
	if order.ClientOrderId != "" {
		m.clientOrderMap[order.ClientOrderId] = order.OrderId
	}

	// Notify order stream
	if m.isOrderStreamRunning {
		update := pb.OrderUpdate{
			OrderId:       id,
			ClientOrderId: req.ClientOrderId,
			Symbol:        req.Symbol,
			Side:          req.Side,
			Type:          req.Type,
			Status:        pb.OrderStatus_ORDER_STATUS_NEW,
			Price:         req.Price,
			Quantity:      req.Quantity,
			ExecutedQty:   pbu.FromGoDecimal(decimal.Zero),
			UpdateTime:    order.UpdateTime,
		}
		for _, callback := range m.orderCallbacks {
			go callback(&update)
		}
	}

	return order, nil
}

func (m *MockExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	var placed []*pb.Order
	for _, req := range orders {
		o, _ := m.PlaceOrder(ctx, req)
		placed = append(placed, o)
	}
	return placed, true
}

func (m *MockExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, exists := m.orders[orderID]
	if !exists {
		return fmt.Errorf("order not found: %d", orderID)
	}

	if order.Status == pb.OrderStatus_ORDER_STATUS_FILLED || order.Status == pb.OrderStatus_ORDER_STATUS_CANCELED {
		return fmt.Errorf("cannot cancel order in status %s", order.Status)
	}

	order.Status = pb.OrderStatus_ORDER_STATUS_CANCELED
	order.UpdateTime = time.Now().UnixMilli()

	// Notify order stream
	if m.isOrderStreamRunning {
		update := pb.OrderUpdate{
			OrderId:       order.OrderId,
			ClientOrderId: order.ClientOrderId,
			Symbol:        order.Symbol,
			Status:        pb.OrderStatus_ORDER_STATUS_CANCELED,
			UpdateTime:    order.UpdateTime,
		}
		for _, callback := range m.orderCallbacks {
			go callback(&update)
		}
	}

	return nil
}

func (m *MockExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	for _, id := range orderIDs {
		m.CancelOrder(ctx, symbol, id, useMargin)
	}
	return nil
}

func (m *MockExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, order := range m.orders {
		if order.Symbol == symbol && order.Status == pb.OrderStatus_ORDER_STATUS_NEW {
			order.Status = pb.OrderStatus_ORDER_STATUS_CANCELED
			order.UpdateTime = time.Now().UnixMilli()

			if m.isOrderStreamRunning {
				update := pb.OrderUpdate{
					OrderId:    order.OrderId,
					Symbol:     order.Symbol,
					Status:     pb.OrderStatus_ORDER_STATUS_CANCELED,
					UpdateTime: order.UpdateTime,
				}
				for _, callback := range m.orderCallbacks {
					go callback(&update)
				}
			}
		}
	}

	return nil
}

func (m *MockExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if orderID != 0 {
		order, exists := m.orders[orderID]
		if !exists {
			return nil, fmt.Errorf("order not found: %d", orderID)
		}
		return order, nil
	}

	if clientOrderID != "" {
		for _, order := range m.orders {
			if order.ClientOrderId == clientOrderID {
				return order, nil
			}
		}
		return nil, fmt.Errorf("order not found by clientOrderId: %s", clientOrderID)
	}

	return nil, fmt.Errorf("either orderId or clientOrderId must be provided")
}

func (m *MockExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var orders []*pb.Order
	for _, order := range m.orders {
		if order.Symbol == symbol && order.Status == pb.OrderStatus_ORDER_STATUS_NEW {
			orders = append(orders, order)
		}
	}
	return orders, nil
}

func (m *MockExchange) GetAccount(ctx context.Context) (*pb.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Attach positions to account
	var allPositions []*pb.Position
	for _, pos := range m.positions {
		allPositions = append(allPositions, pos...)
	}
	m.account.Positions = allPositions

	return m.account, nil
}

func (m *MockExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if symbol != "" {
		return m.positions[symbol], nil
	}

	var all []*pb.Position
	for _, pos := range m.positions {
		all = append(all, pos...)
	}
	return all, nil
}

func (m *MockExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if asset == "USDT" {
		return pbu.ToGoDecimal(m.account.AvailableBalance), nil
	}
	return decimal.Zero, nil
}

func (m *MockExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.orderCallbacks = append(m.orderCallbacks, callback)
	m.isOrderStreamRunning = true

	return nil
}

func (m *MockExchange) StopOrderStream() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isOrderStreamRunning = false
	return nil
}

func (m *MockExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.priceCallbacks = append(m.priceCallbacks, callback)
	m.isPriceStreamRunning = true

	// Simulate updates in background
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.mu.RLock()
				if !m.isPriceStreamRunning {
					m.mu.RUnlock()
					return
				}
				m.mu.RUnlock()

				for _, sym := range symbols {
					// Default mock price
					price := decimal.NewFromFloat(45000.0)
					if sym == "ETHUSDT" {
						price = decimal.NewFromFloat(3000.0)
					}

					change := &pb.PriceChange{
						Symbol:    sym,
						Price:     pbu.FromGoDecimal(price),
						Timestamp: timestamppb.Now(),
					}
					callback(change)
				}
			}
		}
	}()

	return nil
}

func (m *MockExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.klineCallbacks = append(m.klineCallbacks, callback)
	m.isKlineStreamRunning = true

	return nil
}

func (m *MockExchange) StopKlineStream() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isKlineStreamRunning = false
	return nil
}

func (m *MockExchange) StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.accountCallbacks = append(m.accountCallbacks, callback)
	m.isAccountStreamRunning = true

	return nil
}

func (m *MockExchange) StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.positionCallbacks = append(m.positionCallbacks, callback)
	m.isPositionStreamRunning = true

	return nil
}

// GetFundingRate returns the configured funding rate for a symbol.
func (m *MockExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	m.mu.RLock()
	rate, ok := m.fundingRates[symbol]
	m.mu.RUnlock()

	if !ok {
		// Default
		rate = decimal.NewFromFloat(0.0001)
	}

	return &pb.FundingRate{
		Exchange:        m.name,
		Symbol:          symbol,
		Rate:            pbu.FromGoDecimal(rate),
		PredictedRate:   pbu.FromGoDecimal(rate),
		NextFundingTime: time.Now().Add(1 * time.Hour).UnixMilli(),
		Timestamp:       time.Now().UnixMilli(),
	}, nil
}

func (m *MockExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a default list based on symbols we know
	symbols, _ := m.GetSymbols(ctx)
	rates := make([]*pb.FundingRate, 0, len(symbols))

	for _, sym := range symbols {
		rate, ok := m.fundingRates[sym]
		if !ok {
			rate = decimal.NewFromFloat(0.0001)
		}
		rates = append(rates, &pb.FundingRate{
			Exchange:        m.name,
			Symbol:          sym,
			Rate:            pbu.FromGoDecimal(rate),
			PredictedRate:   pbu.FromGoDecimal(rate),
			NextFundingTime: time.Now().Add(1 * time.Hour).UnixMilli(),
			Timestamp:       time.Now().UnixMilli(),
		})
	}
	return rates, nil
}

func (m *MockExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if rates, ok := m.histFundingRates[symbol]; ok {
		if limit > 0 && limit < len(rates) {
			return rates[:limit], nil
		}
		return rates, nil
	}

	rate, ok := m.fundingRates[symbol]
	if !ok {
		rate = decimal.NewFromFloat(0.0001)
	}

	if limit <= 0 {
		limit = 10
	}

	rates := make([]*pb.FundingRate, limit)
	now := time.Now()
	for i := 0; i < limit; i++ {
		rates[i] = &pb.FundingRate{
			Exchange:  m.name,
			Symbol:    symbol,
			Rate:      pbu.FromGoDecimal(rate),
			Timestamp: now.Add(-time.Duration(i*8) * time.Hour).UnixMilli(),
		}
	}
	return rates, nil
}

func (m *MockExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	symbols, _ := m.GetSymbols(ctx)
	tickers := make([]*pb.Ticker, 0, len(symbols))

	for _, sym := range symbols {
		if t, ok := m.tickers[sym]; ok {
			tickers = append(tickers, t)
			continue
		}

		price, _ := m.GetLatestPrice(ctx, sym)
		tickers = append(tickers, &pb.Ticker{
			Symbol:             sym,
			PriceChange:        pbu.FromGoDecimal(decimal.NewFromInt(1)),
			PriceChangePercent: pbu.FromGoDecimal(decimal.NewFromFloat(0.01)),
			LastPrice:          pbu.FromGoDecimal(price),
			Volume:             pbu.FromGoDecimal(decimal.NewFromInt(1000)),
			QuoteVolume:        pbu.FromGoDecimal(decimal.NewFromInt(50000000)), // Default high volume
			OpenInterest:       pbu.FromGoDecimal(decimal.NewFromInt(10000000)),
			Timestamp:          time.Now().UnixMilli(),
		})
	}
	return tickers, nil
}

func (m *MockExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.NewFromInt(10000000), nil
}

func (m *MockExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	// Stub implementation
	return nil
}

// GetLatestPrice returns a deterministic mock price for the symbol.
func (m *MockExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	// Return mock price based on symbol
	switch symbol {
	case "BTCUSDT":
		return decimal.NewFromFloat(45000.0), nil
	case "ETHUSDT":
		return decimal.NewFromFloat(3000.0), nil
	default:
		return decimal.NewFromFloat(100.0), nil
	}
}

func (m *MockExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	var candles []*pb.Candle

	basePrice := 45000.0
	if symbol == "ETHUSDT" {
		basePrice = 3000.0
	}

	for i := 0; i < limit; i++ {
		candle := &pb.Candle{
			Symbol:    symbol,
			Open:      pbu.FromGoDecimal(decimal.NewFromFloat(basePrice + float64(i)*10)),
			High:      pbu.FromGoDecimal(decimal.NewFromFloat(basePrice + float64(i)*10 + 5)),
			Low:       pbu.FromGoDecimal(decimal.NewFromFloat(basePrice + float64(i)*10 - 5)),
			Close:     pbu.FromGoDecimal(decimal.NewFromFloat(basePrice + float64(i)*10 + 2)),
			Volume:    pbu.FromGoDecimal(decimal.NewFromFloat(100.0 + float64(i))),
			Timestamp: time.Now().Add(-time.Duration(i) * time.Minute).UnixMilli(),
			IsClosed:  true,
		}
		candles = append(candles, candle)
	}

	return candles, nil
}

func (m *MockExchange) FetchExchangeInfo(ctx context.Context, symbol string) error {
	return nil
}

func (m *MockExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
	return &pb.SymbolInfo{
		Symbol:            symbol,
		PricePrecision:    2,
		QuantityPrecision: 3,
		BaseAsset:         "BTC",
		QuoteAsset:        "USDT",
	}, nil
}

func (m *MockExchange) GetSymbols(ctx context.Context) ([]string, error) {
	return []string{"BTCUSDT", "ETHUSDT"}, nil
}

// GetPriceDecimals returns the price precision.
func (m *MockExchange) GetPriceDecimals() int {
	return 2
}

func (m *MockExchange) GetQuantityDecimals() int {
	return 3
}

func (m *MockExchange) GetBaseAsset() string {
	return "BTC"
}

func (m *MockExchange) GetQuoteAsset() string {
	return "USDT"
}

// GetOrders returns all orders.
func (m *MockExchange) GetOrders() []*pb.Order {
	m.mu.RLock()
	defer m.mu.RUnlock()
	orders := make([]*pb.Order, 0, len(m.orders))
	for _, o := range m.orders {
		orders = append(orders, o)
	}
	return orders
}

func (m *MockExchange) GetPosition(symbol string) decimal.Decimal {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if pos, ok := m.positions[symbol]; ok && len(pos) > 0 {
		return pbu.ToGoDecimal(pos[0].Size)
	}
	return decimal.Zero
}

// SimulateOrderFill simulates an order being filled.
func (m *MockExchange) SimulateOrderFill(orderID int64, filledQty decimal.Decimal, avgPrice decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, exists := m.orders[orderID]
	if !exists {
		return
	}

	order.Status = pb.OrderStatus_ORDER_STATUS_FILLED
	order.ExecutedQty = pbu.FromGoDecimal(filledQty)
	order.AvgPrice = pbu.FromGoDecimal(avgPrice)
	order.UpdateTime = time.Now().UnixMilli()

	// Update positions
	symbol := order.Symbol
	currentSize := m.GetPosition(symbol)
	delta := filledQty
	if order.Side == pb.OrderSide_ORDER_SIDE_SELL {
		delta = delta.Neg()
	}
	newSize := currentSize.Add(delta)
	m.SetPosition(symbol, newSize)

	// Notify order stream
	if m.isOrderStreamRunning {
		update := pb.OrderUpdate{
			OrderId:       order.OrderId,
			ClientOrderId: order.ClientOrderId,
			Symbol:        order.Symbol,
			Side:          order.Side,
			Status:        pb.OrderStatus_ORDER_STATUS_FILLED,
			ExecutedQty:   order.ExecutedQty,
			AvgPrice:      order.AvgPrice,
			UpdateTime:    order.UpdateTime,
		}
		for _, callback := range m.orderCallbacks {
			go callback(&update)
		}
	}
}
