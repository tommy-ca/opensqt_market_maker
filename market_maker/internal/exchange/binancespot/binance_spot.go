// Package binance_spot provides Binance Spot exchange implementation
package binancespot

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/exchange/base"
	"market_maker/internal/pb"
	"market_maker/pkg/concurrency"
	apperrors "market_maker/pkg/errors"
	"market_maker/pkg/pbu"
	"market_maker/pkg/websocket"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultSpotURL = "https://api.binance.com"
	defaultSpotWS  = "wss://stream.binance.com:9443/ws"
)

// BinanceSpotExchange implements IExchange for Binance Spot
type BinanceSpotExchange struct {
	*base.BaseAdapter
	symbolInfo map[string]*pb.SymbolInfo
	mu         sync.RWMutex
	pool       *concurrency.WorkerPool
}

// NewBinanceSpotExchange creates a new Binance Spot exchange instance
func NewBinanceSpotExchange(cfg *config.ExchangeConfig, logger core.ILogger, pool *concurrency.WorkerPool) *BinanceSpotExchange {
	b := base.NewBaseAdapter("binance_spot", cfg, logger)
	e := &BinanceSpotExchange{
		BaseAdapter: b,
		symbolInfo:  make(map[string]*pb.SymbolInfo),
		pool:        pool,
	}

	b.SetSignRequest(e.SignRequest)
	b.SetParseError(e.parseError)

	return e
}

func (e *BinanceSpotExchange) GetName() string {
	return "binance_spot"
}

func (e *BinanceSpotExchange) IsUnifiedMargin() bool {
	return false
}

// SignRequest adds authentication headers and signature to the request
func (e *BinanceSpotExchange) SignRequest(req *http.Request, body []byte) error {
	// Add API Key header
	req.Header.Set("X-MBX-APIKEY", string(e.Config.APIKey))

	// Get current query params
	q := req.URL.Query()

	// Add timestamp if missing
	if q.Get("timestamp") == "" {
		q.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}

	// Calculate signature
	queryString := q.Encode()
	mac := hmac.New(sha256.New, []byte(e.Config.SecretKey))
	mac.Write([]byte(queryString))
	signature := hex.EncodeToString(mac.Sum(nil))

	// Add signature to query params
	q.Set("signature", signature)
	req.URL.RawQuery = q.Encode()

	return nil
}

func (e *BinanceSpotExchange) parseError(body []byte) error {
	var errResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("binance spot error (unmarshal failed): %s", string(body))
	}

	// Map Binance error codes
	switch errResp.Code {
	case -2015:
		return apperrors.ErrAuthenticationFailed
	case -1013, -1111:
		return apperrors.ErrInvalidOrderParameter
	case -2010:
		return apperrors.ErrInsufficientFunds
	case -2011:
		return apperrors.ErrOrderNotFound
	case -1003:
		return apperrors.ErrRateLimitExceeded
	case -1021:
		return apperrors.ErrTimestampOutOfBounds
	}

	return fmt.Errorf("binance spot error %d: %s", errResp.Code, errResp.Msg)
}

func (e *BinanceSpotExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	path := "/api/v3/order"
	if req.UseMargin {
		path = "/sapi/v1/margin/order"
	}
	url := fmt.Sprintf("%s%s", baseURL, path)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, err
	}

	q := httpReq.URL.Query()
	q.Add("symbol", req.Symbol)

	switch req.Side {
	case pb.OrderSide_ORDER_SIDE_BUY:
		q.Add("side", "BUY")
	case pb.OrderSide_ORDER_SIDE_SELL:
		q.Add("side", "SELL")
	default:
		return nil, fmt.Errorf("invalid order side: %s", req.Side)
	}

	switch req.Type {
	case pb.OrderType_ORDER_TYPE_LIMIT:
		q.Add("type", "LIMIT")
		q.Add("price", pbu.ToGoDecimal(req.Price).String())
		q.Add("timeInForce", "GTC")
	case pb.OrderType_ORDER_TYPE_MARKET:
		q.Add("type", "MARKET")
	default:
		return nil, fmt.Errorf("invalid order type: %s", req.Type)
	}

	q.Add("quantity", pbu.ToGoDecimal(req.Quantity).String())

	if req.ClientOrderId != "" {
		q.Add("newClientOrderId", req.ClientOrderId)
	}

	httpReq.URL.RawQuery = q.Encode()

	if err := e.SignRequest(httpReq, nil); err != nil {
		return nil, err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(body)
	}

	var rawOrder struct {
		OrderID       int64  `json:"orderId"`
		Symbol        string `json:"symbol"`
		Status        string `json:"status"`
		ClientOrderID string `json:"clientOrderId"`
		Price         string `json:"price"`
		OrigQty       string `json:"origQty"`
		ExecutedQty   string `json:"executedQty"`
		Type          string `json:"type"`
		Side          string `json:"side"`
		TransactTime  int64  `json:"transactTime"`
	}

	if err := json.Unmarshal(body, &rawOrder); err != nil {
		return nil, err
	}

	// Map status
	var status pb.OrderStatus
	switch rawOrder.Status {
	case "NEW":
		status = pb.OrderStatus_ORDER_STATUS_NEW
	case "PARTIALLY_FILLED":
		status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case "FILLED":
		status = pb.OrderStatus_ORDER_STATUS_FILLED
	case "CANCELED":
		status = pb.OrderStatus_ORDER_STATUS_CANCELED
	case "REJECTED":
		status = pb.OrderStatus_ORDER_STATUS_REJECTED
	case "EXPIRED":
		status = pb.OrderStatus_ORDER_STATUS_EXPIRED
	default:
		status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}

	price, _ := decimal.NewFromString(rawOrder.Price)
	qty, _ := decimal.NewFromString(rawOrder.OrigQty)
	execQty, _ := decimal.NewFromString(rawOrder.ExecutedQty)

	return &pb.Order{
		OrderId:       rawOrder.OrderID,
		ClientOrderId: rawOrder.ClientOrderID,
		Symbol:        rawOrder.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Status:        status,
		Price:         pbu.FromGoDecimal(price),
		Quantity:      pbu.FromGoDecimal(qty),
		ExecutedQty:   pbu.FromGoDecimal(execQty),
		UpdateTime:    rawOrder.TransactTime,
		CreatedAt:     timestamppb.New(time.UnixMilli(rawOrder.TransactTime)),
	}, nil
}

func (e *BinanceSpotExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	return nil, false
}

func (e *BinanceSpotExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	path := "/api/v3/order"
	if useMargin {
		path = "/sapi/v1/margin/order"
	}
	url := fmt.Sprintf("%s%s", baseURL, path)

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	q := httpReq.URL.Query()
	q.Add("symbol", symbol)
	q.Add("orderId", fmt.Sprintf("%d", orderID))

	httpReq.URL.RawQuery = q.Encode()

	if err := e.SignRequest(httpReq, nil); err != nil {
		return err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return e.parseError(body)
	}

	return nil
}

func (e *BinanceSpotExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	return fmt.Errorf("not implemented")
}

func (e *BinanceSpotExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	path := "/api/v3/openOrders"
	if useMargin {
		path = "/sapi/v1/margin/openOrders"
	}
	url := fmt.Sprintf("%s%s", baseURL, path)

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	q := httpReq.URL.Query()
	q.Add("symbol", symbol)
	httpReq.URL.RawQuery = q.Encode()

	if err := e.SignRequest(httpReq, nil); err != nil {
		return err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return e.parseError(body)
	}

	return nil
}

func (e *BinanceSpotExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	path := "/api/v3/order"
	if useMargin {
		path = "/sapi/v1/margin/order"
	}
	url := fmt.Sprintf("%s%s", baseURL, path)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	q := httpReq.URL.Query()
	q.Add("symbol", symbol)
	if orderID != 0 {
		q.Add("orderId", fmt.Sprintf("%d", orderID))
	} else if clientOrderID != "" {
		q.Add("origClientOrderId", clientOrderID)
	}
	httpReq.URL.RawQuery = q.Encode()

	if err := e.SignRequest(httpReq, nil); err != nil {
		return nil, err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(body)
	}

	var rawOrder struct {
		OrderID       int64  `json:"orderId"`
		Symbol        string `json:"symbol"`
		Status        string `json:"status"`
		ClientOrderID string `json:"clientOrderId"`
		Price         string `json:"price"`
		OrigQty       string `json:"origQty"`
		ExecutedQty   string `json:"executedQty"`
		Type          string `json:"type"`
		Side          string `json:"side"`
		Time          int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &rawOrder); err != nil {
		return nil, err
	}

	// Map status
	var status pb.OrderStatus
	switch rawOrder.Status {
	case "NEW":
		status = pb.OrderStatus_ORDER_STATUS_NEW
	case "PARTIALLY_FILLED":
		status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case "FILLED":
		status = pb.OrderStatus_ORDER_STATUS_FILLED
	case "CANCELED":
		status = pb.OrderStatus_ORDER_STATUS_CANCELED
	case "REJECTED":
		status = pb.OrderStatus_ORDER_STATUS_REJECTED
	case "EXPIRED":
		status = pb.OrderStatus_ORDER_STATUS_EXPIRED
	}

	price, _ := decimal.NewFromString(rawOrder.Price)
	qty, _ := decimal.NewFromString(rawOrder.OrigQty)
	execQty, _ := decimal.NewFromString(rawOrder.ExecutedQty)

	var side pb.OrderSide
	if rawOrder.Side == "BUY" {
		side = pb.OrderSide_ORDER_SIDE_BUY
	} else {
		side = pb.OrderSide_ORDER_SIDE_SELL
	}

	var otype pb.OrderType
	if rawOrder.Type == "LIMIT" {
		otype = pb.OrderType_ORDER_TYPE_LIMIT
	} else {
		otype = pb.OrderType_ORDER_TYPE_MARKET
	}

	return &pb.Order{
		OrderId:       rawOrder.OrderID,
		ClientOrderId: rawOrder.ClientOrderID,
		Symbol:        rawOrder.Symbol,
		Side:          side,
		Type:          otype,
		Status:        status,
		Price:         pbu.FromGoDecimal(price),
		Quantity:      pbu.FromGoDecimal(qty),
		ExecutedQty:   pbu.FromGoDecimal(execQty),
		UpdateTime:    rawOrder.Time,
		CreatedAt:     timestamppb.New(time.UnixMilli(rawOrder.Time)),
	}, nil
}

func (e *BinanceSpotExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	path := "/api/v3/openOrders"
	if useMargin {
		path = "/sapi/v1/margin/openOrders"
	}
	url := fmt.Sprintf("%s%s", baseURL, path)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	q := httpReq.URL.Query()
	q.Add("symbol", symbol)
	httpReq.URL.RawQuery = q.Encode()

	if err := e.SignRequest(httpReq, nil); err != nil {
		return nil, err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(body)
	}

	var rawOrders []struct {
		OrderID       int64  `json:"orderId"`
		Symbol        string `json:"symbol"`
		Status        string `json:"status"`
		ClientOrderID string `json:"clientOrderId"`

		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		Type        string `json:"type"`
		Side        string `json:"side"`
		Time        int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &rawOrders); err != nil {
		return nil, err
	}

	orders := make([]*pb.Order, len(rawOrders))
	for i, raw := range rawOrders {
		var status pb.OrderStatus
		switch raw.Status {
		case "NEW":
			status = pb.OrderStatus_ORDER_STATUS_NEW
		case "PARTIALLY_FILLED":
			status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
		case "FILLED":
			status = pb.OrderStatus_ORDER_STATUS_FILLED
		case "CANCELED":
			status = pb.OrderStatus_ORDER_STATUS_CANCELED
		default:
			status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
		}

		price, _ := decimal.NewFromString(raw.Price)
		qty, _ := decimal.NewFromString(raw.OrigQty)
		execQty, _ := decimal.NewFromString(raw.ExecutedQty)

		var side pb.OrderSide
		if raw.Side == "BUY" {
			side = pb.OrderSide_ORDER_SIDE_BUY
		} else {
			side = pb.OrderSide_ORDER_SIDE_SELL
		}

		orders[i] = &pb.Order{
			OrderId:       raw.OrderID,
			ClientOrderId: raw.ClientOrderID,
			Symbol:        raw.Symbol,
			Side:          side,
			Status:        status,
			Price:         pbu.FromGoDecimal(price),
			Quantity:      pbu.FromGoDecimal(qty),
			ExecutedQty:   pbu.FromGoDecimal(execQty),
			UpdateTime:    raw.Time,
			CreatedAt:     timestamppb.New(time.UnixMilli(raw.Time)),
		}
	}

	return orders, nil
}

func (e *BinanceSpotExchange) GetAccount(ctx context.Context) (*pb.Account, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	url := fmt.Sprintf("%s/api/v3/account", baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if err := e.SignRequest(httpReq, nil); err != nil {
		return nil, err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(body)
	}

	var rawAccount struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}

	if err := json.Unmarshal(body, &rawAccount); err != nil {
		return nil, err
	}

	positions := make([]*pb.Position, 0)
	totalBalance := decimal.Zero // We can't easily calculate total USD balance without prices

	// Convert balances to "Positions" for unified view, though Spot doesn't really have "positions" in same way
	// We map non-zero balances to positions
	for _, b := range rawAccount.Balances {
		free, _ := decimal.NewFromString(b.Free)
		locked, _ := decimal.NewFromString(b.Locked)
		total := free.Add(locked)

		if total.IsZero() {
			continue
		}

		// Spot "Position"
		positions = append(positions, &pb.Position{
			Symbol:     b.Asset, // Note: Asset, not Symbol (e.g. BTC, not BTCUSDT)
			Size:       pbu.FromGoDecimal(total),
			EntryPrice: pbu.FromGoDecimal(decimal.Zero), // Unknown
		})
	}

	return &pb.Account{
		TotalWalletBalance: pbu.FromGoDecimal(totalBalance), // Placeholder
		Positions:          positions,
	}, nil
}

func (e *BinanceSpotExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	// For Spot, GetPositions is just a filtered view of GetAccount balances
	account, err := e.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	// If symbol is provided (e.g. BTCUSDT), we need to extract the base asset (BTC)
	// Ideally we look up symbol info to get BaseAsset
	// For now, if symbol is provided, we try to match it or filter?
	// The interface implies "GetPositions" for a trading symbol.
	// For Spot-Perp Arb, we need to know the amount of BTC we hold.

	// If symbol == "", return all.
	if symbol == "" {
		return account.Positions, nil
	}

	// Hacky: Try to guess asset from symbol if not available from Info
	// Better: Use FetchExchangeInfo to get BaseAsset
	info, err := e.GetSymbolInfo(ctx, symbol)
	targetAsset := symbol
	if err == nil {
		targetAsset = info.BaseAsset
	} else {
		// Fallback simple stripping
		targetAsset = strings.TrimSuffix(symbol, "USDT")
	}

	var filtered []*pb.Position
	for _, p := range account.Positions {
		if p.Symbol == targetAsset {
			// Re-wrap to use the trading Pair symbol for consistency?
			// Or keep Asset name?
			// Strategies usually expect Symbol (BTCUSDT).
			// Let's modify the return to match the requested Symbol
			p.Symbol = symbol
			filtered = append(filtered, p)
		}
	}

	return filtered, nil
}

func (e *BinanceSpotExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	account, err := e.GetAccount(ctx)
	if err != nil {
		return decimal.Zero, err
	}

	for _, p := range account.Positions {
		// In GetAccount we stored Asset name in Symbol field
		if p.Symbol == asset {
			return pbu.ToGoDecimal(p.Size), nil
		}
	}

	return decimal.Zero, nil
}

func (e *BinanceSpotExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	// Listen Key logic similar to Futures but different endpoint
	listenKey, err := e.getListenKey(ctx)
	if err != nil {
		return err
	}

	go e.keepAliveListenKey(ctx, listenKey)

	baseURL := e.Config.BaseURL
	wsURL := defaultSpotWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	streamURL := fmt.Sprintf("%s/%s", wsURL, listenKey)

	client := websocket.NewClient(streamURL, func(message []byte) {
		var event struct {
			Event     string `json:"e"`
			EventTime int64  `json:"E"`
			Symbol    string `json:"s"`
			ClientOid string `json:"c"`
			Side      string `json:"S"`
			Type      string `json:"o"`
			Status    string `json:"X"`
			OrderID   int64  `json:"i"`
			LastQty   string `json:"l"`
			CumQty    string `json:"z"`
			LastPrice string `json:"L"`
			OrderTime int64  `json:"T"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal spot order update", "error", err)
			return
		}

		if event.Event != "executionReport" {
			return
		}

		var status pb.OrderStatus
		switch event.Status {
		case "NEW":
			status = pb.OrderStatus_ORDER_STATUS_NEW
		case "PARTIALLY_FILLED":
			status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
		case "FILLED":
			status = pb.OrderStatus_ORDER_STATUS_FILLED
		case "CANCELED":
			status = pb.OrderStatus_ORDER_STATUS_CANCELED
		case "REJECTED":
			status = pb.OrderStatus_ORDER_STATUS_REJECTED
		case "EXPIRED":
			status = pb.OrderStatus_ORDER_STATUS_EXPIRED
		default:
			status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
		}

		execQty, _ := decimal.NewFromString(event.LastQty)
		price, _ := decimal.NewFromString(event.LastPrice)

		// Spot execution report doesn't always have Average Price directly, usually just Last Price of fill
		// For simplicity, using LastPrice as AvgPrice for single fill updates

		update := pb.OrderUpdate{
			Exchange:      "binance_spot",
			OrderId:       event.OrderID,
			ClientOrderId: event.ClientOid,
			Symbol:        event.Symbol,
			Status:        status,
			ExecutedQty:   pbu.FromGoDecimal(execQty),
			Price:         pbu.FromGoDecimal(price),
			AvgPrice:      pbu.FromGoDecimal(price),
			UpdateTime:    event.OrderTime,
		}

		if e.pool != nil {
			e.pool.Submit(func() { callback(&update) })
		} else {
			callback(&update)
		}
	}, e.Logger)

	go func() {
		client.Start()
		<-ctx.Done()
		client.Stop()
	}()

	return nil
}

func (e *BinanceSpotExchange) StopOrderStream() error {
	return nil
}

func (e *BinanceSpotExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultSpotWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	for _, symbol := range symbols {
		streamURL := fmt.Sprintf("%s/%s@bookTicker", wsURL, strings.ToLower(symbol))

		client := websocket.NewClient(streamURL, func(message []byte) {
			var event struct {
				Symbol   string `json:"s"`
				BidPrice string `json:"b"`
				// ... other fields
			}
			if err := json.Unmarshal(message, &event); err != nil {
				return
			}

			price, _ := decimal.NewFromString(event.BidPrice)

			change := pb.PriceChange{
				Symbol:    event.Symbol,
				Price:     pbu.FromGoDecimal(price),
				Timestamp: timestamppb.Now(),
			}

			if e.pool != nil {
				e.pool.Submit(func() { callback(&change) })
			} else {
				callback(&change)
			}
		}, e.Logger)

		go func() {
			client.Start()
			<-ctx.Done()
			client.Stop()
		}()
	}
	return nil
}

func (e *BinanceSpotExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	return fmt.Errorf("not implemented")
}

func (e *BinanceSpotExchange) StopKlineStream() error {
	return nil
}

func (e *BinanceSpotExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	url := fmt.Sprintf("%s/api/v3/ticker/price", baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Zero, err
	}

	q := httpReq.URL.Query()
	q.Add("symbol", symbol)
	httpReq.URL.RawQuery = q.Encode()

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return decimal.Zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	var res struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromString(res.Price)
}

func (e *BinanceSpotExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *BinanceSpotExchange) FetchExchangeInfo(ctx context.Context, symbol string) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	url := fmt.Sprintf("%s/api/v3/exchangeInfo", baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	// Optimize: filter by symbol if API supports it (Binance Spot does: ?symbol=BTCUSDT)
	q := httpReq.URL.Query()
	if symbol != "" {
		q.Add("symbol", symbol)
	}
	httpReq.URL.RawQuery = q.Encode()

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	var response struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
			// ... filters
		} `json:"symbols"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, s := range response.Symbols {
		info := &pb.SymbolInfo{
			Symbol:     s.Symbol,
			BaseAsset:  s.BaseAsset,
			QuoteAsset: s.QuoteAsset,
		}
		e.symbolInfo[s.Symbol] = info
	}

	return nil
}

func (e *BinanceSpotExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
	e.mu.RLock()
	info, ok := e.symbolInfo[symbol]
	e.mu.RUnlock()

	if ok {
		return info, nil
	}

	if err := e.FetchExchangeInfo(ctx, symbol); err != nil {
		return nil, err
	}

	e.mu.RLock()
	info, ok = e.symbolInfo[symbol]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("symbol info not found for %s", symbol)
	}

	return info, nil
}

func (e *BinanceSpotExchange) GetSymbols(ctx context.Context) ([]string, error) {
	e.mu.RLock()
	if len(e.symbolInfo) == 0 {
		e.mu.RUnlock()
		if err := e.FetchExchangeInfo(ctx, ""); err != nil {
			return nil, err
		}
		e.mu.RLock()
	}
	defer e.mu.RUnlock()

	symbols := make([]string, 0, len(e.symbolInfo))
	for s := range e.symbolInfo {
		symbols = append(symbols, s)
	}
	return symbols, nil
}

func (e *BinanceSpotExchange) GetPriceDecimals() int {
	return 2
}

func (e *BinanceSpotExchange) GetQuantityDecimals() int {
	return 5
}

func (e *BinanceSpotExchange) GetBaseAsset() string {
	return "BTC"
}

func (e *BinanceSpotExchange) GetQuoteAsset() string {
	return "USDT"
}

func (e *BinanceSpotExchange) CheckHealth(ctx context.Context) error {
	return nil
}

// GetFundingRate returns the current funding rate for a symbol (always 0 for spot)
func (e *BinanceSpotExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	return &pb.FundingRate{
		Exchange:  "binance_spot",
		Symbol:    symbol,
		Rate:      pbu.FromGoDecimal(decimal.Zero),
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

func (e *BinanceSpotExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	// Spot has 0 funding for all symbols
	symbols, err := e.GetSymbols(ctx)
	if err != nil {
		return nil, err
	}

	rates := make([]*pb.FundingRate, len(symbols))
	now := time.Now().UnixMilli()
	for i, s := range symbols {
		rates[i] = &pb.FundingRate{
			Exchange:  "binance_spot",
			Symbol:    s,
			Rate:      pbu.FromGoDecimal(decimal.Zero),
			Timestamp: now,
		}
	}
	return rates, nil
}

func (e *BinanceSpotExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	// Spot doesn't have Open Interest
	return decimal.Zero, nil
}

func (e *BinanceSpotExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}

	url := fmt.Sprintf("%s/api/v3/ticker/24hr", baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(body)
	}

	var data []struct {
		Symbol             string `json:"symbol"`
		PriceChange        string `json:"priceChange"`
		PriceChangePercent string `json:"priceChangePercent"`
		LastPrice          string `json:"lastPrice"`
		Volume             string `json:"volume"`
		QuoteVolume        string `json:"quoteVolume"`
		CloseTime          int64  `json:"closeTime"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	tickers := make([]*pb.Ticker, 0, len(data))
	for _, item := range data {
		pc, _ := decimal.NewFromString(item.PriceChange)
		pcp, _ := decimal.NewFromString(item.PriceChangePercent)
		lp, _ := decimal.NewFromString(item.LastPrice)
		vol, _ := decimal.NewFromString(item.Volume)
		qv, _ := decimal.NewFromString(item.QuoteVolume)

		tickers = append(tickers, &pb.Ticker{
			Symbol:             item.Symbol,
			PriceChange:        pbu.FromGoDecimal(pc),
			PriceChangePercent: pbu.FromGoDecimal(pcp.Div(decimal.NewFromInt(100))), // Convert percent to ratio
			LastPrice:          pbu.FromGoDecimal(lp),
			Volume:             pbu.FromGoDecimal(vol),
			QuoteVolume:        pbu.FromGoDecimal(qv),
			Timestamp:          item.CloseTime,
		})
	}

	return tickers, nil
}

func (e *BinanceSpotExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	// No funding rate stream for spot
	return nil
}

// StartAccountStream implements account balance streaming via polling (gRPC stub)
func (e *BinanceSpotExchange) StartAccountStream(ctx context.Context, callback func(*pb.Account)) error {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				account, err := e.GetAccount(ctx)
				if err != nil {
					e.Logger.Warn("Account polling failed in stream", "error", err)
					continue
				}
				callback(account)
			}
		}
	}()
	return nil
}

// StartPositionStream implements position streaming via polling (gRPC stub)
func (e *BinanceSpotExchange) StartPositionStream(ctx context.Context, callback func(*pb.Position)) error {
	// For Spot, positions are balances
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				positions, err := e.GetPositions(ctx, "")
				if err != nil {
					continue
				}
				for _, p := range positions {
					callback(p)
				}
			}
		}
	}()
	return nil
}

// Helper methods for ListenKey
func (e *BinanceSpotExchange) getListenKey(ctx context.Context) (string, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}
	url := fmt.Sprintf("%s/api/v3/userDataStream", baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-MBX-APIKEY", string(e.Config.APIKey))

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get listenKey: %s", string(body))
	}

	var result struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ListenKey, nil
}

func (e *BinanceSpotExchange) keepAliveListenKey(ctx context.Context, listenKey string) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultSpotURL
	}
	url := fmt.Sprintf("%s/api/v3/userDataStream", baseURL)

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(ctx, "PUT", url, nil)
			req.Header.Set("X-MBX-APIKEY", string(e.Config.APIKey))
			q := req.URL.Query()
			q.Add("listenKey", listenKey)
			req.URL.RawQuery = q.Encode()

			resp, err := e.HTTPClient.Do(req)
			if err != nil {
				e.Logger.Error("Failed to refresh listenKey", "error", err)
				continue
			}
			resp.Body.Close()
		}
	}
}
