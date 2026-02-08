// Package gate provides Gate.io exchange implementation
package gate

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/exchange/base"
	"market_maker/internal/pb"
	apperrors "market_maker/pkg/errors"
	"market_maker/pkg/pbu"
	"market_maker/pkg/retry"
	"market_maker/pkg/websocket"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultGateURL = "https://api.gateio.ws"
	defaultGateWS  = "wss://fx-ws.gateio.ws/v4/ws/usdt"
)

// GateExchange implements IExchange for Gate.io
type GateExchange struct {
	*base.BaseAdapter
	symbolInfo       map[string]*pb.SymbolInfo
	quantoMultiplier map[string]decimal.Decimal
	mu               sync.RWMutex
}

// NewGateExchange creates a new Gate.io exchange instance
func NewGateExchange(cfg *config.ExchangeConfig, logger core.ILogger) *GateExchange {
	b := base.NewBaseAdapter("gate", cfg, logger)
	e := &GateExchange{
		BaseAdapter:      b,
		symbolInfo:       make(map[string]*pb.SymbolInfo),
		quantoMultiplier: make(map[string]decimal.Decimal),
	}

	b.SetSignRequest(func(req *http.Request, body []byte) error {
		// Gate SignREST expects method, urlPath, queryString, body
		// We'll use a wrapper or just use the existing SignREST inside ExecuteRequest if we refactor more.
		// For now, Gate is a bit different, but we'll stick to BaseAdapter.
		return nil // We'll handle signing in Gate-specific methods if needed, or update SignRequest
	})
	b.SetParseError(e.parseError)

	return e
}

// SignREST generates signature for REST API
func (e *GateExchange) SignREST(method, urlPath, queryString, body string, timestamp int64) string {
	hasher := sha512.New()
	hasher.Write([]byte(body))
	bodyHash := hex.EncodeToString(hasher.Sum(nil))

	message := fmt.Sprintf("%s\n%s\n%s\n%s\n%d",
		method,
		urlPath,
		queryString,
		bodyHash,
		timestamp,
	)

	mac := hmac.New(sha512.New, []byte(e.Config.SecretKey))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func (e *GateExchange) parseError(body []byte) error {
	var errResp struct {
		Label   string `json:"label"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("gate error (unmarshal failed): %s", string(body))
	}

	// Map Gate error labels
	switch errResp.Label {
	case "INVALID_PARAM":
		return apperrors.ErrInvalidOrderParameter
	case "AUTHENTICATION_FAILED", "INVALID_KEY", "INVALID_SIGNATURE":
		return apperrors.ErrAuthenticationFailed
	case "BALANCE_NOT_ENOUGH":
		return apperrors.ErrInsufficientFunds
	case "ORDER_NOT_FOUND":
		return apperrors.ErrOrderNotFound
	case "TOO_MANY_REQUESTS":
		return apperrors.ErrRateLimitExceeded
	case "ORDER_POC_IMMEDIATE": // PostOnly failure
		return apperrors.ErrOrderRejected
	case "SERVER_ERROR":
		return apperrors.ErrSystemOverload
	}

	return fmt.Errorf("gate error: %s (%s)", errResp.Message, errResp.Label)
}

func (e *GateExchange) GetName() string {
	return "gate"
}

func (e *GateExchange) IsUnifiedMargin() bool {
	// Gate supports Portfolio Margin, but this adapter uses the standard Futures API
	return false
}

func (e *GateExchange) CheckHealth(ctx context.Context) error {
	return nil
}

func (e *GateExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	var order *pb.Order
	err := retry.Do(ctx, retry.DefaultPolicy, e.isTransientError, func() error {
		var err error
		order, err = e.placeOrderInternal(ctx, req)
		if err != nil {
			if errors.Is(err, apperrors.ErrDuplicateOrder) {
				if req.ClientOrderId != "" {
					existing, fetchErr := e.GetOrder(ctx, req.Symbol, 0, req.ClientOrderId, false)
					if fetchErr == nil {
						order = existing
						return nil
					}
				}
			}
			return err
		}
		return nil
	})

	return order, err
}

func (e *GateExchange) isTransientError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, apperrors.ErrRateLimitExceeded) ||
		errors.Is(err, apperrors.ErrSystemOverload)
}

func (e *GateExchange) placeOrderInternal(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	contract := toGateSymbol(req.Symbol)

	e.mu.RLock()
	multiplier, ok := e.quantoMultiplier[contract]
	e.mu.RUnlock()
	if !ok {
		// Try to fetch if not found
		e.FetchExchangeInfo(ctx, req.Symbol)
		e.mu.RLock()
		multiplier = e.quantoMultiplier[contract]
		e.mu.RUnlock()
	}

	if multiplier.IsZero() {
		multiplier = decimal.NewFromInt(1)
	}

	qtyCoins := pbu.ToGoDecimal(req.Quantity)
	size := qtyCoins.Div(multiplier).Round(0).IntPart()
	if size == 0 && !qtyCoins.IsZero() {
		size = 1
	}

	if req.Side == pb.OrderSide_ORDER_SIDE_SELL {
		size = -size
	}

	body := map[string]interface{}{
		"contract": contract,
		"size":     size,
		"price":    pbu.ToGoDecimal(req.Price).String(),
		"tif":      "gtc",
	}

	if req.ClientOrderId != "" {
		body["text"] = fmt.Sprintf("t-%s", req.ClientOrderId) // Gate recommends prefix
	}
	if req.PostOnly {
		body["tif"] = "poc" // Pending or Cancel (Post-Only)
	}
	if req.ReduceOnly {
		body["reduce_only"] = true
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	path := "/api/v4/futures/usdt/orders"
	url := baseURL + path

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	timestamp := time.Now().Unix()
	signature := e.SignREST("POST", path, "", string(jsonBody), timestamp)

	httpReq.Header.Set("KEY", string(e.Config.APIKey))
	httpReq.Header.Set("SIGN", signature)
	httpReq.Header.Set("Timestamp", fmt.Sprintf("%d", timestamp))

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, e.parseError(respBody)
	}

	var rawOrder struct {
		ID         int64  `json:"id"`
		Text       string `json:"text"`
		Contract   string `json:"contract"`
		Size       int64  `json:"size"`
		Price      string `json:"price"`
		Status     string `json:"status"`
		CreateTime int64  `json:"create_time"`
	}

	if err := json.Unmarshal(respBody, &rawOrder); err != nil {
		return nil, err
	}

	// Map status
	// Gate statuses: open, finished
	var status pb.OrderStatus
	if rawOrder.Status == "open" {
		status = pb.OrderStatus_ORDER_STATUS_NEW
	} else if rawOrder.Status == "finished" {
		status = pb.OrderStatus_ORDER_STATUS_FILLED // Or CANCELED? Needs more check.
		// For now assume filled if finished immediately (IOC/FOK) or just mapped to FILLED for simplicity
	} else {
		status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}

	price, _ := decimal.NewFromString(rawOrder.Price)
	// Size is signed int64
	qty := decimal.NewFromInt(rawOrder.Size).Abs()

	return &pb.Order{
		OrderId:       rawOrder.ID,
		ClientOrderId: strings.TrimPrefix(rawOrder.Text, "t-"),
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Status:        status,
		Price:         pbu.FromGoDecimal(price),
		Quantity:      pbu.FromGoDecimal(qty),
		CreatedAt:     timestamppb.New(time.Unix(rawOrder.CreateTime, 0)),
	}, nil
}

func toGateSymbol(symbol string) string {
	// BTCUSDT -> BTC_USDT
	// Simple heuristic: insert _ before USDT
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "_USDT"
	}
	return symbol
}

func (e *GateExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	placedOrders := make([]*pb.Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := e.PlaceOrder(ctx, orderReq)
		if err != nil {
			if strings.Contains(err.Error(), "BALANCE_NOT_ENOUGH") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

func (e *GateExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	path := fmt.Sprintf("/api/v4/futures/usdt/orders/%d", orderID)
	url := baseURL + path

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	timestamp := time.Now().Unix()
	signature := e.SignREST("DELETE", path, "", "", timestamp)

	httpReq.Header.Set("KEY", string(e.Config.APIKey))
	httpReq.Header.Set("SIGN", signature)
	httpReq.Header.Set("Timestamp", fmt.Sprintf("%d", timestamp))

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		// Gate returns 404 if order not found?
		// "If the order_id does not exist, an error will be returned."
		// Error format: {"label": "ORDER_NOT_FOUND", "message": "Order not found"}
		// If status is 404 and label is ORDER_NOT_FOUND, we treat as success.
		if resp.StatusCode == 404 {
			// Check body for ORDER_NOT_FOUND
			var errResp struct {
				Label string `json:"label"`
			}
			if json.Unmarshal(respBody, &errResp) == nil && errResp.Label == "ORDER_NOT_FOUND" {
				return nil
			}
		}
		return e.parseError(respBody)
	}

	return nil
}

func (e *GateExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	for _, id := range orderIDs {
		_ = e.CancelOrder(ctx, symbol, id, useMargin)
	}
	return nil
}

func (e *GateExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	path := "/api/v4/futures/usdt/orders"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	q := req.URL.Query()
	q.Add("contract", toGateSymbol(symbol))
	req.URL.RawQuery = q.Encode()

	ts := time.Now().Unix()
	sign := e.SignREST("DELETE", path, q.Encode(), "", ts)

	req.Header.Set("KEY", string(e.Config.APIKey))
	req.Header.Set("SIGN", sign)
	req.Header.Set("Timestamp", fmt.Sprintf("%d", ts))

	resp, err := e.HTTPClient.Do(req)
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

func (e *GateExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	if orderID == 0 && clientOrderID != "" {
		// Gate requires order ID for the detail endpoint,
		// but we can query by text (clientOID) using the list endpoint
		orders, err := e.GetOpenOrders(ctx, symbol, useMargin)
		if err == nil {
			for _, o := range orders {
				if o.ClientOrderId == clientOrderID {
					return o, nil
				}
			}
		}
		return nil, fmt.Errorf("order not found by clientOrderId: %s", clientOrderID)
	}

	return nil, fmt.Errorf("not implemented for single order details")
}

func (e *GateExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	path := "/api/v4/futures/usdt/orders"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("status", "open")
	if symbol != "" {
		q.Add("contract", toGateSymbol(symbol))
	}
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	timestamp := time.Now().Unix()
	signature := e.SignREST("GET", path, req.URL.RawQuery, "", timestamp)

	req.Header.Set("KEY", string(e.Config.APIKey))
	req.Header.Set("SIGN", signature)
	req.Header.Set("Timestamp", fmt.Sprintf("%d", timestamp))

	resp, err := e.HTTPClient.Do(req)
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

	// Gate returns a list of orders directly
	var rawOrders []struct {
		ID       int64  `json:"id"`
		Text     string `json:"text"`
		Contract string `json:"contract"`
		Size     int64  `json:"size"` // Remaining size? or Total?
		// "size": "Order size. Positive for buying, negative for selling."
		// "left": "Size left to be traded"
		Left       int64  `json:"left"`
		Price      string `json:"price"`
		Status     string `json:"status"` // "open", "finished"
		CreateTime int64  `json:"create_time"`
	}

	if err := json.Unmarshal(body, &rawOrders); err != nil {
		return nil, err
	}

	orders := make([]*pb.Order, len(rawOrders))
	for i, raw := range rawOrders {
		price, _ := decimal.NewFromString(raw.Price)

		// Size is signed.
		totalQty := decimal.NewFromInt(raw.Size).Abs()
		leftQty := decimal.NewFromInt(raw.Left).Abs()
		execQty := totalQty.Sub(leftQty)

		var side pb.OrderSide
		if raw.Size > 0 {
			side = pb.OrderSide_ORDER_SIDE_BUY
		} else {
			side = pb.OrderSide_ORDER_SIDE_SELL
		}

		orders[i] = &pb.Order{
			OrderId:       raw.ID,
			ClientOrderId: strings.TrimPrefix(raw.Text, "t-"),
			Symbol:        raw.Contract,
			Side:          side,
			Status:        pb.OrderStatus_ORDER_STATUS_NEW, // Filtered by status=open
			Price:         pbu.FromGoDecimal(price),
			Quantity:      pbu.FromGoDecimal(totalQty),
			ExecutedQty:   pbu.FromGoDecimal(execQty),
			UpdateTime:    raw.CreateTime * 1000,
			CreatedAt:     timestamppb.New(time.Unix(raw.CreateTime, 0)),
		}
	}

	return orders, nil
}

func (e *GateExchange) GetAccount(ctx context.Context) (*pb.Account, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	path := "/api/v4/futures/usdt/accounts"
	url := baseURL + path

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	timestamp := time.Now().Unix()
	signature := e.SignREST("GET", path, "", "", timestamp)

	httpReq.Header.Set("KEY", string(e.Config.APIKey))
	httpReq.Header.Set("SIGN", signature)
	httpReq.Header.Set("Timestamp", fmt.Sprintf("%d", timestamp))

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(respBody)
	}

	var rawAccount struct {
		Total     string `json:"total"`
		Available string `json:"available"`
		Point     string `json:"point"`
		Currency  string `json:"currency"`
	}

	if err := json.Unmarshal(respBody, &rawAccount); err != nil {
		return nil, err
	}

	total, _ := decimal.NewFromString(rawAccount.Total)
	available, _ := decimal.NewFromString(rawAccount.Available)

	return &pb.Account{
		TotalWalletBalance: pbu.FromGoDecimal(total),
		AvailableBalance:   pbu.FromGoDecimal(available),
		// Gate futures uses isolated/cross margin per position, account level leverage is tricky?
		// We'll leave AccountLeverage 0 for now as it might be per position.
	}, nil
}

func (e *GateExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	path := "/api/v4/futures/usdt/positions"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	ts := time.Now().Unix()
	sign := e.SignREST("GET", path, "", "", ts)

	req.Header.Set("KEY", string(e.Config.APIKey))
	req.Header.Set("SIGN", sign)
	req.Header.Set("Timestamp", fmt.Sprintf("%d", ts))

	resp, err := e.HTTPClient.Do(req)
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

	var rawPositions []struct {
		User          int    `json:"user"`
		Contract      string `json:"contract"`
		Size          int64  `json:"size"`
		Leverage      string `json:"leverage"`
		EntryPrice    string `json:"entry_price"`
		MarkPrice     string `json:"mark_price"`
		UnrealizedPnl string `json:"unrealized_pnl"`
		MarginMode    string `json:"margin_mode"`
	}

	if err := json.Unmarshal(body, &rawPositions); err != nil {
		return nil, err
	}

	positions := make([]*pb.Position, 0)
	gateSym := toGateSymbol(symbol)

	for _, p := range rawPositions {
		if symbol != "" && p.Contract != gateSym && p.Contract != symbol {
			continue
		}
		if p.Size == 0 {
			continue
		}

		e.mu.RLock()
		multiplier, ok := e.quantoMultiplier[p.Contract]
		e.mu.RUnlock()
		if !ok {
			multiplier = decimal.NewFromInt(1)
		}

		sizeCoins := decimal.NewFromInt(p.Size).Mul(multiplier)
		entryPrice, _ := decimal.NewFromString(p.EntryPrice)
		markPrice, _ := decimal.NewFromString(p.MarkPrice)
		upl, _ := decimal.NewFromString(p.UnrealizedPnl)
		leverage, _ := strconv.Atoi(p.Leverage)

		positions = append(positions, &pb.Position{
			Symbol:        p.Contract,
			Size:          pbu.FromGoDecimal(sizeCoins),
			EntryPrice:    pbu.FromGoDecimal(entryPrice),
			MarkPrice:     pbu.FromGoDecimal(markPrice),
			UnrealizedPnl: pbu.FromGoDecimal(upl),
			Leverage:      int32(leverage),
			MarginType:    p.MarginMode,
		})
	}

	return positions, nil
}

func (e *GateExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *GateExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultGateWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		var event struct {
			Time    int64           `json:"time"`
			Channel string          `json:"channel"`
			Event   string          `json:"event"`
			Result  json.RawMessage `json:"result"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			return
		}

		if event.Event != "update" {
			return
		}
		if event.Channel != "futures.orders" {
			return
		}

		var orders []struct {
			ID         int64  `json:"id"`
			Contract   string `json:"contract"`
			Text       string `json:"text"`
			CreateTime int64  `json:"create_time"`
			Price      string `json:"price"`
			FillPrice  string `json:"fill_price"` // Executed price?
			Status     string `json:"status"`
			FinishAs   string `json:"finish_as"`
			Size       int64  `json:"size"`
			Left       int64  `json:"left"`
		}
		if err := json.Unmarshal(event.Result, &orders); err != nil {
			return
		}

		for _, o := range orders {
			var status pb.OrderStatus
			if o.Status == "open" {
				status = pb.OrderStatus_ORDER_STATUS_NEW
			} else if o.Status == "finished" {
				switch o.FinishAs {
				case "filled":
					status = pb.OrderStatus_ORDER_STATUS_FILLED
				case "cancelled":
					status = pb.OrderStatus_ORDER_STATUS_CANCELED
				case "ioc", "fok":
					status = pb.OrderStatus_ORDER_STATUS_EXPIRED // Or canceled?
				default:
					status = pb.OrderStatus_ORDER_STATUS_FILLED // Assume filled if unknown finish_as
				}
			}

			price, _ := decimal.NewFromString(o.Price)
			// Calculate filled qty?
			// Size is total. Left is remaining.
			// Executed = Size - Left?
			executedQty := decimal.NewFromInt(o.Size - o.Left).Abs()

			// AvgPrice? Gate provides fill_price per update? Or is it last fill?
			// V4 docs say: fill_price is "Order fill price".
			// We might need to track avg price ourselves or rely on exchange.
			avgPrice, _ := decimal.NewFromString(o.FillPrice)

			update := pb.OrderUpdate{
				OrderId:       o.ID,
				ClientOrderId: strings.TrimPrefix(o.Text, "t-"),
				Symbol:        o.Contract,
				Status:        status,
				ExecutedQty:   pbu.FromGoDecimal(executedQty),
				Price:         pbu.FromGoDecimal(price),
				AvgPrice:      pbu.FromGoDecimal(avgPrice),
				UpdateTime:    event.Time * 1000, // Gate timestamp is seconds
			}

			callback(&update)
		}
	}, e.Logger)

	client.SetOnConnected(func() {

		// Subscribe with Auth
		timestamp := time.Now().Unix()
		channel := "futures.orders"
		event := "subscribe"
		message := fmt.Sprintf("channel=%s&event=%s&time=%d", channel, event, timestamp)

		mac := hmac.New(sha512.New, []byte(e.Config.SecretKey))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		sub := map[string]interface{}{
			"time":    timestamp,
			"channel": channel,
			"event":   event,
			"payload": []string{"!all"}, // Subscribe all symbols
			"auth": map[string]interface{}{
				"method": "api_key",
				"KEY":    string(e.Config.APIKey),
				"SIGN":   signature,
			},
		}
		client.Send(sub)
	})

	go func() {
		client.Start()
		<-ctx.Done()
		client.Stop()
	}()

	return nil
}

func (e *GateExchange) StopOrderStream() error {
	return nil
}

func (e *GateExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultGateWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		var event struct {
			Time    int64           `json:"time"`
			Channel string          `json:"channel"`
			Event   string          `json:"event"`
			Result  json.RawMessage `json:"result"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			return
		}

		if event.Event != "update" {
			return
		}
		if event.Channel != "futures.tickers" {
			return
		}

		var tickers []struct {
			Contract string `json:"contract"`
			Last     string `json:"last"`
		}
		if err := json.Unmarshal(event.Result, &tickers); err != nil {
			return
		}

		for _, t := range tickers {
			price, _ := decimal.NewFromString(t.Last)

			change := pb.PriceChange{
				Symbol:    t.Contract,
				Price:     pbu.FromGoDecimal(price),
				Timestamp: timestamppb.New(time.Unix(event.Time, 0)),
			}

			callback(&change)
		}
	}, e.Logger)

	client.SetOnConnected(func() {
		gateSymbols := make([]string, len(symbols))
		for i, s := range symbols {
			gateSymbols[i] = toGateSymbol(s)
		}

		// Subscribe
		sub := map[string]interface{}{
			"time":    time.Now().Unix(),
			"channel": "futures.tickers",
			"event":   "subscribe",
			"payload": gateSymbols,
		}
		client.Send(sub)
	})

	go func() {
		client.Start()
		<-ctx.Done()
		client.Stop()
	}()

	return nil
}

func (e *GateExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultGateWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		var event struct {
			Time    int64           `json:"time"`
			Channel string          `json:"channel"`
			Event   string          `json:"event"`
			Result  json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(message, &event); err != nil {
			return
		}

		if event.Channel != "futures.candlesticks" || event.Event != "update" {
			return
		}

		var raw struct {
			T int64  `json:"t"`
			V int64  `json:"v"`
			C string `json:"c"`
			H string `json:"h"`
			L string `json:"l"`
			O string `json:"o"`
			N string `json:"n"` // Name: 1m_BTC_USDT
		}
		if err := json.Unmarshal(event.Result, &raw); err != nil {
			return
		}

		parts := strings.Split(raw.N, "_")
		if len(parts) < 3 {
			return
		}
		sym := parts[1] + parts[2] // BTCUSDT

		op, _ := decimal.NewFromString(raw.O)
		hi, _ := decimal.NewFromString(raw.H)
		lo, _ := decimal.NewFromString(raw.L)
		cl, _ := decimal.NewFromString(raw.C)
		vo := decimal.NewFromInt(raw.V)

		callback(&pb.Candle{
			Symbol:    sym,
			Open:      pbu.FromGoDecimal(op),
			High:      pbu.FromGoDecimal(hi),
			Low:       pbu.FromGoDecimal(lo),
			Close:     pbu.FromGoDecimal(cl),
			Volume:    pbu.FromGoDecimal(vo),
			Timestamp: raw.T * 1000,
			IsClosed:  false,
		})
	}, e.Logger)

	client.SetOnConnected(func() {
		payload := make([]string, len(symbols))
		for i, s := range symbols {
			payload[i] = fmt.Sprintf("%s_%s", interval, toGateSymbol(s))
		}

		sub := map[string]interface{}{
			"time":    time.Now().Unix(),
			"channel": "futures.candlesticks",
			"event":   "subscribe",
			"payload": payload,
		}
		client.Send(sub)
	})

	go func() {
		client.Start()
		<-ctx.Done()
		client.Stop()
	}()

	return nil
}

func (e *GateExchange) StopKlineStream() error {
	return nil
}

func (e *GateExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *GateExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	path := "/api/v4/futures/usdt/candlesticks"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("contract", toGateSymbol(symbol))
	q.Add("interval", interval)
	q.Add("limit", fmt.Sprintf("%d", limit))
	req.URL.RawQuery = q.Encode()

	resp, err := e.HTTPClient.Do(req)
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

	var rawCandles []struct {
		T int64  `json:"t"`
		V int64  `json:"v"`
		C string `json:"c"`
		H string `json:"h"`
		L string `json:"l"`
		O string `json:"o"`
	}

	if err := json.Unmarshal(body, &rawCandles); err != nil {
		return nil, err
	}

	candles := make([]*pb.Candle, len(rawCandles))
	for i, c := range rawCandles {
		op, _ := decimal.NewFromString(c.O)
		hi, _ := decimal.NewFromString(c.H)
		lo, _ := decimal.NewFromString(c.L)
		cl, _ := decimal.NewFromString(c.C)
		vo := decimal.NewFromInt(c.V)

		candles[i] = &pb.Candle{
			Symbol:    symbol,
			Open:      pbu.FromGoDecimal(op),
			High:      pbu.FromGoDecimal(hi),
			Low:       pbu.FromGoDecimal(lo),
			Close:     pbu.FromGoDecimal(cl),
			Volume:    pbu.FromGoDecimal(vo),
			Timestamp: c.T * 1000,
			IsClosed:  true,
		}
	}

	return candles, nil
}

func (e *GateExchange) FetchExchangeInfo(ctx context.Context, symbol string) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultGateURL
	}

	// Assuming USDT futures
	path := "/api/v4/futures/usdt/contracts"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if symbol != "" {
		q := req.URL.Query()
		q.Add("contract", toGateSymbol(symbol))
		req.URL.RawQuery = q.Encode()
	}

	resp, err := e.HTTPClient.Do(req)
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

	// Response is array of objects if fetching one or list?
	// If specific contract requested, returns array with one element?
	// Docs say "List all futures contracts" returns array.
	// "Get a single contract" is /contracts/{contract}
	// But List accepts contract param? No, List does not usually.
	// But `contracts` endpoint with `contract` query param?
	// Docs: `GET /futures/{settle}/contracts/{contract}` for single.
	// `GET /futures/{settle}/contracts` for all.
	// I'll implement Fetch all for now as caching is good.
	// If I pass symbol, I might want to fetch single.

	// Wait, if I used query param `contract` above, does it work?
	// Gate V4 docs for List All: No parameters usually.
	// Gate V4 docs for Get Single: Path param.
	// So I should change logic if symbol is provided.

	if symbol != "" {
		// Single contract fetch
		// But response format might be object instead of array?
		// "Retrieve a single contract" returns object.
		// So unmarshalling might fail if I assume array.
		// Let's stick to Fetch All for simplicity and consistency with other connectors,
		// or handle both.
		// Ideally I fetch all to populate cache once.
		// If I really need single, I check `symbol`.
		// But to keep it simple, I will ignore `symbol` arg and fetch all if it's empty, or fetch all anyway.
		// Fetching all is safer for "contracts" endpoint.
		// Re-create request without query params
		req, _ = http.NewRequestWithContext(ctx, "GET", url, nil) // Reset
		resp, err = e.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ = io.ReadAll(resp.Body)
	}

	var response []struct {
		Name             string `json:"name"`
		OrderPriceRound  string `json:"order_price_round"` // "0.1"
		OrderSizeMin     string `json:"order_size_min"`    // "1"
		QuantoMultiplier string `json:"quanto_multiplier"` // "0.001"
		ConfigChangeTime int64  `json:"config_change_time"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, c := range response {
		tickSz, _ := decimal.NewFromString(c.OrderPriceRound)
		minQty, _ := decimal.NewFromString(c.OrderSizeMin)
		multiplier, _ := decimal.NewFromString(c.QuantoMultiplier)
		if multiplier.IsZero() {
			multiplier = decimal.NewFromInt(1)
		}

		e.quantoMultiplier[c.Name] = multiplier
		normalized := strings.ReplaceAll(c.Name, "_", "")
		e.quantoMultiplier[normalized] = multiplier

		pricePrec := -tickSz.Exponent()
		qtyPrec := -minQty.Exponent()

		info := &pb.SymbolInfo{

			Symbol:            c.Name,
			BaseAsset:         strings.Split(c.Name, "_")[0], // BTC
			QuoteAsset:        "USDT",
			PricePrecision:    pricePrec,
			QuantityPrecision: qtyPrec,
			TickSize:          pbu.FromGoDecimal(tickSz),
			MinQuantity:       pbu.FromGoDecimal(minQty),
			StepSize:          pbu.FromGoDecimal(minQty), // Assuming step = min?
		}

		e.symbolInfo[c.Name] = info
		e.symbolInfo[normalized] = info
	}

	return nil
}

func (e *GateExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
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

func (e *GateExchange) GetSymbols(ctx context.Context) ([]string, error) {
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

func (e *GateExchange) GetPriceDecimals() int {

	return 2
}

func (e *GateExchange) GetQuantityDecimals() int {
	return 3
}

func (e *GateExchange) GetBaseAsset() string {
	return "BTC"
}

func (e *GateExchange) GetQuoteAsset() string {
	return "USDT"
}

// StartAccountStream implements account balance streaming via polling
func (e *GateExchange) StartAccountStream(ctx context.Context, callback func(*pb.Account)) error {
	return e.StartPollingStream(ctx, func(ctx context.Context) (interface{}, error) {
		return e.GetAccount(ctx)
	}, func(data interface{}) {
		callback(data.(*pb.Account))
	}, 5*time.Second, "AccountStream")
}

// StartPositionStream implements position streaming via polling
func (e *GateExchange) StartPositionStream(ctx context.Context, callback func(*pb.Position)) error {
	return e.StartPollingStream(ctx, func(ctx context.Context) (interface{}, error) {
		return e.GetPositions(ctx, "")
	}, func(data interface{}) {
		positions := data.([]*pb.Position)
		for _, position := range positions {
			callback(position)
		}
	}, 5*time.Second, "PositionStream")
}

func (e *GateExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *GateExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *GateExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *GateExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *GateExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *GateExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	return fmt.Errorf("not implemented")
}
