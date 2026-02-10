// Package bitget provides Bitget exchange implementation
package bitget

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	defaultBitgetURL   = "https://api.bitget.com"
	defaultBitgetWS    = "wss://ws.bitget.com/mix/v1/stream"
	defaultProductType = "USDT-FUTURES"
	defaultMarginCoin  = "USDT"
)

// BitgetExchange implements IExchange for Bitget
type BitgetExchange struct {
	*base.BaseAdapter
	posMode     string
	productType string
	marginCoin  string
	symbolInfo  map[string]*pb.SymbolInfo
	mu          sync.RWMutex
}

// NewBitgetExchange creates a new Bitget exchange instance
func NewBitgetExchange(cfg *config.ExchangeConfig, logger core.ILogger) *BitgetExchange {
	b := base.NewBaseAdapter("bitget", cfg, logger)
	e := &BitgetExchange{
		BaseAdapter: b,
		posMode:     "hedge_mode",
		productType: defaultProductType,
		marginCoin:  defaultMarginCoin,
		symbolInfo:  make(map[string]*pb.SymbolInfo),
	}

	b.SetSignRequest(func(req *http.Request, body []byte) error {
		e.SignRequest(req, string(body))
		return nil
	})
	b.SetParseError(e.parseError)
	b.SetMapOrderStatus(e.mapOrderStatus)

	return e
}

// SignRequest adds authentication headers to the request
func (e *BitgetExchange) SignRequest(req *http.Request, body string) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	method := req.Method
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	payload := timestamp + method + path + body

	mac := hmac.New(sha256.New, []byte(e.Config.SecretKey))
	mac.Write([]byte(payload))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("ACCESS-KEY", string(e.Config.APIKey))
	req.Header.Set("ACCESS-SIGN", signature)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("ACCESS-PASSPHRASE", string(e.Config.Passphrase))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "en-US")
}

func (e *BitgetExchange) parseError(body []byte) error {
	var errResp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("bitget error (unmarshal failed): %s", string(body))
	}

	// Map Bitget error codes (strings)
	switch errResp.Code {
	case "00000":
		return nil
	case "40019", "45110": // 45110: less than min amount
		return apperrors.ErrInvalidOrderParameter
	case "40014": // 40014: Incorrect access key
		return apperrors.ErrAuthenticationFailed
	case "43009": // Insufficient balance
		return apperrors.ErrInsufficientFunds
	case "40029": // Order not found
		return apperrors.ErrOrderNotFound
	case "40009": // System error
		return apperrors.ErrSystemOverload
	case "40012": // API Key expired
		return apperrors.ErrAuthenticationFailed
	case "40003": // Request too frequent
		return apperrors.ErrRateLimitExceeded
	}

	return fmt.Errorf("bitget error: %s (%s)", errResp.Msg, errResp.Code)
}

func (e *BitgetExchange) mapOrderStatus(rawStatus string) pb.OrderStatus {
	switch rawStatus {
	case "new", "live":
		return pb.OrderStatus_ORDER_STATUS_NEW
	case "filled":
		return pb.OrderStatus_ORDER_STATUS_FILLED
	case "cancelled":
		return pb.OrderStatus_ORDER_STATUS_CANCELED
	case "partial-fill", "partially_filled":
		return pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	default:
		return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func (e *BitgetExchange) GetName() string {
	return "bitget"
}

func (e *BitgetExchange) IsUnifiedMargin() bool {
	// Bitget supports multi-asset margin which is a form of unified margin
	return true
}

func (e *BitgetExchange) CheckHealth(ctx context.Context) error {
	return nil
}

func (e *BitgetExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
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

func (e *BitgetExchange) isTransientError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, apperrors.ErrRateLimitExceeded) ||
		errors.Is(err, apperrors.ErrSystemOverload)
}

func (e *BitgetExchange) placeOrderInternal(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultBitgetURL
	}

	side := "buy"
	if req.Side == pb.OrderSide_ORDER_SIDE_SELL {
		side = "sell"
	}

	var tradeSide string
	reduceOnly := ""

	if e.posMode == "hedge_mode" {
		if req.ReduceOnly {
			tradeSide = "close"
		} else {
			tradeSide = "open"
		}
	} else {
		if req.ReduceOnly {
			reduceOnly = "YES"
		}
	}

	body := map[string]interface{}{
		"symbol":      req.Symbol,
		"productType": e.productType,
		"marginMode":  "crossed",
		"marginCoin":  e.marginCoin,
		"side":        side,
		"orderType":   "limit",
		"price":       pbu.ToGoDecimal(req.Price).String(),
		"size":        pbu.ToGoDecimal(req.Quantity).String(),
		"force":       "gtc",
	}

	if req.Type == pb.OrderType_ORDER_TYPE_MARKET {
		body["orderType"] = "market"
		delete(body, "price")
	}

	if req.ClientOrderId != "" {
		body["clientOID"] = req.ClientOrderId
	}

	if tradeSide != "" {
		body["tradeSide"] = tradeSide
	}
	if reduceOnly != "" {
		body["reduceOnly"] = reduceOnly
	}
	if req.PostOnly {
		body["force"] = "post_only"
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	path := "/api/v2/mix/order/place-order"
	url := baseURL + path

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}

	e.SignRequest(httpReq, string(jsonBody))

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OrderID       string `json:"orderId"`
			ClientOrderID string `json:"clientOID"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, err
	}

	if response.Code != "00000" {
		return nil, e.parseError(respBody)
	}

	orderID, _ := strconv.ParseInt(response.Data.OrderID, 10, 64)

	return &pb.Order{
		OrderId:       orderID,
		ClientOrderId: response.Data.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Status:        pb.OrderStatus_ORDER_STATUS_NEW,
		Price:         req.Price,
		Quantity:      req.Quantity,
		CreatedAt:     timestamppb.Now(),
	}, nil
}

func (e *BitgetExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	placedOrders := make([]*pb.Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := e.PlaceOrder(ctx, orderReq)
		if err != nil {
			e.Logger.Warn("Failed to place order in batch", "symbol", orderReq.Symbol, "error", err)
			if strings.Contains(err.Error(), "insufficient funds") || strings.Contains(err.Error(), "43009") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

func (e *BitgetExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultBitgetURL
	}

	body := map[string]interface{}{
		"symbol":      symbol,
		"productType": e.productType,
		"marginCoin":  e.marginCoin,
		"orderId":     fmt.Sprintf("%d", orderID),
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	path := "/api/v2/mix/order/cancel-order"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}

	e.SignRequest(req, string(jsonBody))

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return err
	}

	if response.Code != "00000" {
		if strings.Contains(response.Msg, "not exist") || response.Code == "40029" {
			return nil
		}
		return e.parseError(respBody)
	}

	return nil
}

func (e *BitgetExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	if len(orderIDs) == 0 {
		return nil
	}

	for i := 0; i < len(orderIDs); i += 20 {
		end := i + 20
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]
		orderIDStrs := make([]string, len(batch))
		for j, id := range batch {
			orderIDStrs[j] = fmt.Sprintf("%d", id)
		}

		body := map[string]interface{}{
			"symbol":      symbol,
			"productType": e.productType,
			"marginCoin":  e.marginCoin,
			"orderIdList": orderIDStrs,
		}

		jsonBody, _ := json.Marshal(body)
		path := "/api/v2/mix/order/batch-cancel-orders"
		url := e.Config.BaseURL + path
		if e.Config.BaseURL == "" {
			url = defaultBitgetURL + path
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
		if err != nil {
			continue
		}

		e.SignRequest(req, string(jsonBody))
		resp, err := e.HTTPClient.Do(req)
		if err != nil {
			e.Logger.Warn("Batch cancel request failed", "error", err)
			continue
		}
		resp.Body.Close()
	}

	return nil
}

func (e *BitgetExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	body := map[string]interface{}{
		"symbol":      symbol,
		"productType": e.productType,
		"marginCoin":  e.marginCoin,
	}

	jsonBody, _ := json.Marshal(body)
	path := "/api/v2/mix/order/cancel-all-orders"
	url := e.Config.BaseURL + path
	if e.Config.BaseURL == "" {
		url = defaultBitgetURL + path
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}

	e.SignRequest(req, string(jsonBody))
	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return e.parseError(respBody)
}

func (e *BitgetExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	path := fmt.Sprintf("/api/v2/mix/order/detail?symbol=%s&productType=%s", symbol, e.productType)
	if orderID != 0 {
		path += fmt.Sprintf("&orderId=%d", orderID)
	} else if clientOrderID != "" {
		path += fmt.Sprintf("&clientOID=%s", clientOrderID)
	}

	url := e.Config.BaseURL + path
	if e.Config.BaseURL == "" {
		url = defaultBitgetURL + path
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	e.SignRequest(req, "")
	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(body)
	}

	var response struct {
		Data struct {
			Symbol    string `json:"symbol"`
			Size      string `json:"size"`
			OrderID   string `json:"orderId"`
			ClientOID string `json:"clientOID"`
			FilledQty string `json:"filledQty"`
			Price     string `json:"price"`
			AvgPrice  string `json:"priceAvg"`
			Side      string `json:"side"`
			Status    string `json:"status"`
			UTime     string `json:"uTime"`
			CTime     string `json:"cTime"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	raw := response.Data
	price, _ := decimal.NewFromString(raw.Price)
	qty, _ := decimal.NewFromString(raw.Size)
	execQty, _ := decimal.NewFromString(raw.FilledQty)
	avgPrice, _ := decimal.NewFromString(raw.AvgPrice)
	uTime, _ := strconv.ParseInt(raw.UTime, 10, 64)
	cTime, _ := strconv.ParseInt(raw.CTime, 10, 64)

	var side pb.OrderSide
	if raw.Side == "buy" {
		side = pb.OrderSide_ORDER_SIDE_BUY
	} else {
		side = pb.OrderSide_ORDER_SIDE_SELL
	}

	var status pb.OrderStatus
	switch raw.Status {
	case "new":
		status = pb.OrderStatus_ORDER_STATUS_NEW
	case "filled":
		status = pb.OrderStatus_ORDER_STATUS_FILLED
	case "cancelled":
		status = pb.OrderStatus_ORDER_STATUS_CANCELED
	case "partial-fill":
		status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	}

	finalOrderID := orderID
	if raw.OrderID != "" {
		if parsed, err := strconv.ParseInt(raw.OrderID, 10, 64); err == nil {
			finalOrderID = parsed
		}
	}

	return &pb.Order{
		OrderId:       finalOrderID,
		ClientOrderId: raw.ClientOID,
		Symbol:        raw.Symbol,
		Side:          side,
		Type:          pb.OrderType_ORDER_TYPE_LIMIT,
		Status:        status,
		Price:         pbu.FromGoDecimal(price),
		Quantity:      pbu.FromGoDecimal(qty),
		ExecutedQty:   pbu.FromGoDecimal(execQty),
		AvgPrice:      pbu.FromGoDecimal(avgPrice),
		UpdateTime:    uTime,
		CreatedAt:     timestamppb.New(time.UnixMilli(cTime)),
	}, nil
}

func (e *BitgetExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultBitgetURL
	}

	path := "/api/v2/mix/order/orders-pending"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("productType", e.productType)
	if symbol != "" {
		q.Add("symbol", symbol)
	}
	req.URL.RawQuery = q.Encode()

	e.SignRequest(req, "")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			OrderID       string `json:"orderId"`
			ClientOrderID string `json:"clientOID"`
			Symbol        string `json:"symbol"`
			Price         string `json:"price"`
			Size          string `json:"size"`
			Side          string `json:"side"`
			Status        string `json:"status"`
			CTime         string `json:"cTime"`
			FilledSize    string `json:"filledSize"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Code != "00000" {
		return nil, e.parseError(body)
	}

	orders := make([]*pb.Order, len(response.Data))
	for i, raw := range response.Data {
		orderID, _ := strconv.ParseInt(raw.OrderID, 10, 64)
		price, _ := decimal.NewFromString(raw.Price)
		qty, _ := decimal.NewFromString(raw.Size)
		execQty, _ := decimal.NewFromString(raw.FilledSize)
		ts, _ := strconv.ParseInt(raw.CTime, 10, 64)

		var status pb.OrderStatus
		switch raw.Status {
		case "live", "new":
			status = pb.OrderStatus_ORDER_STATUS_NEW
		case "partially_filled":
			status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
		case "filled":
			status = pb.OrderStatus_ORDER_STATUS_FILLED
		case "cancelled":
			status = pb.OrderStatus_ORDER_STATUS_CANCELED
		default:
			status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
		}

		var side pb.OrderSide
		if strings.EqualFold(raw.Side, "buy") {
			side = pb.OrderSide_ORDER_SIDE_BUY
		} else {
			side = pb.OrderSide_ORDER_SIDE_SELL
		}

		orders[i] = &pb.Order{
			OrderId:       orderID,
			ClientOrderId: raw.ClientOrderID,
			Symbol:        raw.Symbol,
			Side:          side,
			Status:        status,
			Price:         pbu.FromGoDecimal(price),
			Quantity:      pbu.FromGoDecimal(qty),
			ExecutedQty:   pbu.FromGoDecimal(execQty),
			UpdateTime:    ts,
			CreatedAt:     timestamppb.New(time.UnixMilli(ts)),
		}
	}

	return orders, nil
}

func (e *BitgetExchange) GetAccount(ctx context.Context) (*pb.Account, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultBitgetURL
	}

	path := fmt.Sprintf("/api/v2/mix/account/account?productType=%s&marginCoin=%s", e.productType, e.marginCoin)
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	e.SignRequest(req, "")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Available             string `json:"available"`
			Equity                string `json:"accountEquity"`
			PosMode               string `json:"posMode"`
			CrossedMarginLeverage string `json:"crossedMarginLeverage"`
			CrossedRiskRate       string `json:"crossedRiskRate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Code != "00000" {
		return nil, e.parseError(body)
	}

	e.posMode = response.Data.PosMode

	available, _ := decimal.NewFromString(response.Data.Available)
	equity, _ := decimal.NewFromString(response.Data.Equity)
	leverage, _ := decimal.NewFromString(response.Data.CrossedMarginLeverage)
	riskRate, _ := decimal.NewFromString(response.Data.CrossedRiskRate)

	health := decimal.NewFromInt(1).Sub(riskRate)
	if health.IsNegative() {
		health = decimal.Zero
	}

	positions, _ := e.GetPositions(ctx, "")

	return &pb.Account{
		TotalWalletBalance: pbu.FromGoDecimal(equity),
		TotalMarginBalance: pbu.FromGoDecimal(equity),
		AvailableBalance:   pbu.FromGoDecimal(available),
		Positions:          positions,
		AccountLeverage:    int32(leverage.IntPart()),
		IsUnified:          true,
		HealthScore:        pbu.FromGoDecimal(health),
		MarginMode:         pb.MarginMode_MARGIN_MODE_UNIFIED,
		AdjustedEquity:     pbu.FromGoDecimal(equity),
	}, nil
}

func (e *BitgetExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultBitgetURL
	}

	path := fmt.Sprintf("/api/v2/mix/position/single-position?symbol=%s&productType=%s&marginCoin=%s", symbol, e.productType, e.marginCoin)
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	e.SignRequest(req, "")
	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol           string `json:"symbol"`
			HoldSide         string `json:"holdSide"`
			Total            string `json:"total"`
			AverageOpenPrice string `json:"averageOpenPrice"`
			MarkPrice        string `json:"markPrice"`
			UnrealizedPL     string `json:"unrealizedPL"`
			Leverage         string `json:"leverage"`
			MarginMode       string `json:"marginMode"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Code != "00000" {
		return nil, e.parseError(body)
	}

	positions := make([]*pb.Position, 0, len(response.Data))
	for _, item := range response.Data {
		size, _ := decimal.NewFromString(item.Total)
		if size.IsZero() {
			continue
		}

		if item.HoldSide == "short" {
			size = size.Neg()
		}

		entryPrice, _ := decimal.NewFromString(item.AverageOpenPrice)
		markPrice, _ := decimal.NewFromString(item.MarkPrice)
		upl, _ := decimal.NewFromString(item.UnrealizedPL)
		leverage, _ := strconv.Atoi(item.Leverage)

		positions = append(positions, &pb.Position{
			Symbol:        item.Symbol,
			Size:          pbu.FromGoDecimal(size),
			EntryPrice:    pbu.FromGoDecimal(entryPrice),
			MarkPrice:     pbu.FromGoDecimal(markPrice),
			UnrealizedPnl: pbu.FromGoDecimal(upl),
			Leverage:      int32(leverage),
			MarginType:    item.MarginMode,
		})
	}

	return positions, nil
}

func (e *BitgetExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *BitgetExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultBitgetWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		if string(message) == "pong" {
			return
		}

		var event struct {
			Action string `json:"action"`
			Arg    struct {
				Channel string `json:"channel"`
			} `json:"arg"`
			Data []struct {
				InstID        string `json:"instId"`
				OrderID       string `json:"ordId"`
				ClientOrderID string `json:"clOrdID"`
				Price         string `json:"px"`
				Size          string `json:"sz"`
				Status        string `json:"status"`
				Side          string `json:"side"`
				CTime         int64  `json:"cTime,string"`
				AccFillSz     string `json:"accFillSz"`
				AvgPx         string `json:"avgPx"`
			} `json:"data"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal ticker message", "error", err)
			return
		}

		if event.Action != "snapshot" && event.Action != "update" {
			return
		}
		if event.Arg.Channel != "orders" {
			return
		}

		for _, data := range event.Data {
			orderID, _ := strconv.ParseInt(data.OrderID, 10, 64)
			price, _ := decimal.NewFromString(data.Price)
			execQty, _ := decimal.NewFromString(data.AccFillSz)
			avgPrice, _ := decimal.NewFromString(data.AvgPx)

			var status pb.OrderStatus
			switch data.Status {
			case "new", "live":
				status = pb.OrderStatus_ORDER_STATUS_NEW
			case "partially_filled":
				status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
			case "filled":
				status = pb.OrderStatus_ORDER_STATUS_FILLED
			case "cancelled":
				status = pb.OrderStatus_ORDER_STATUS_CANCELED
			default:
				status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
			}

			update := pb.OrderUpdate{
				OrderId:       orderID,
				ClientOrderId: data.ClientOrderID,
				Symbol:        data.InstID,
				Status:        status,
				ExecutedQty:   pbu.FromGoDecimal(execQty),
				Price:         pbu.FromGoDecimal(price),
				AvgPrice:      pbu.FromGoDecimal(avgPrice),
				UpdateTime:    data.CTime,
			}

			callback(&update)
		}
	}, e.Logger)

	client.SetOnConnected(func() {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		sign := e.generateWSSign(timestamp, "GET", "/user/verify")
		loginMsg := map[string]interface{}{
			"op": "login",
			"args": []map[string]interface{}{
				{
					"apiKey":     string(e.Config.APIKey),
					"passphrase": string(e.Config.Passphrase),
					"timestamp":  timestamp,
					"sign":       sign,
				},
			},
		}
		if err := client.Send(loginMsg); err != nil {
			e.Logger.Error("Failed to send login message", "error", err)
		}

		go func() {
			time.Sleep(100 * time.Millisecond)
			subMsg := map[string]interface{}{
				"op": "subscribe",
				"args": []map[string]string{
					{
						"instType": "UMCBL",
						"channel":  "orders",
						"instId":   "default",
					},
				},
			}
			if err := client.Send(subMsg); err != nil {
				e.Logger.Error("Failed to send orders subscription message", "error", err)
			}
		}()
	})

	go func() {
		client.Start()
		<-ctx.Done()
		client.Stop()
	}()

	return nil
}

func (e *BitgetExchange) generateWSSign(timestamp, method, requestPath string) string {
	message := timestamp + method + requestPath
	mac := hmac.New(sha256.New, []byte(e.Config.SecretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (e *BitgetExchange) StopOrderStream() error {
	return nil
}

func (e *BitgetExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultBitgetWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		if string(message) == "pong" {
			return
		}

		var event struct {
			Action string `json:"action"`
			Arg    struct {
				Channel string `json:"channel"`
				InstID  string `json:"instId"`
			} `json:"arg"`
			Data []struct {
				InstID  string      `json:"instId"`
				Last    string      `json:"last"`
				BestBid string      `json:"bestBid"`
				TS      interface{} `json:"ts"`
			} `json:"data"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal ticker message", "error", err)
			return
		}

		if event.Action != "snapshot" && event.Action != "update" {
			return
		}
		if event.Arg.Channel != "ticker" {
			return
		}

		for _, data := range event.Data {
			price, _ := decimal.NewFromString(data.Last)
			var ts int64
			switch v := data.TS.(type) {
			case float64:
				ts = int64(v)
			case string:
				tsVal, _ := strconv.ParseInt(v, 10, 64)
				ts = tsVal
			}

			change := pb.PriceChange{
				Symbol:    data.InstID,
				Price:     pbu.FromGoDecimal(price),
				Timestamp: timestamppb.New(time.UnixMilli(ts)),
			}

			callback(&change)
		}
	}, e.Logger)

	client.SetOnConnected(func() {
		args := make([]map[string]string, len(symbols))
		for i, symbol := range symbols {
			args[i] = map[string]string{
				"instType": "UMCBL",
				"channel":  "ticker",
				"instId":   symbol,
			}
		}

		sub := map[string]interface{}{
			"op":   "subscribe",
			"args": args,
		}
		if err := client.Send(sub); err != nil {
			e.Logger.Error("Failed to send tickers subscription", "error", err)
		}
	})

	go func() {
		client.Start()
		<-ctx.Done()
		client.Stop()
	}()

	return nil
}

func (e *BitgetExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultBitgetWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		var event struct {
			Action string `json:"action"`
			Arg    struct {
				Channel string `json:"channel"`
				InstID  string `json:"instId"`
			} `json:"arg"`
			Data [][]string `json:"data"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal kline message", "error", err)
			return
		}

		if event.Arg.Channel != "candle"+interval {
			return
		}

		for _, item := range event.Data {
			if len(item) < 6 {
				continue
			}
			ts, _ := strconv.ParseInt(item[0], 10, 64)
			o, _ := decimal.NewFromString(item[1])
			h, _ := decimal.NewFromString(item[2])
			l, _ := decimal.NewFromString(item[3])
			c, _ := decimal.NewFromString(item[4])
			v, _ := decimal.NewFromString(item[5])

			callback(&pb.Candle{
				Symbol:    event.Arg.InstID,
				Open:      pbu.FromGoDecimal(o),
				High:      pbu.FromGoDecimal(h),
				Low:       pbu.FromGoDecimal(l),
				Close:     pbu.FromGoDecimal(c),
				Volume:    pbu.FromGoDecimal(v),
				Timestamp: ts,
				IsClosed:  false,
			})
		}
	}, e.Logger)

	client.SetOnConnected(func() {
		args := make([]map[string]string, len(symbols))
		for i, s := range symbols {
			args[i] = map[string]string{
				"instType": "UMCBL",
				"channel":  "candle" + interval,
				"instId":   s,
			}
		}

		sub := map[string]interface{}{
			"op":   "subscribe",
			"args": args,
		}
		if err := client.Send(sub); err != nil {
			e.Logger.Error("Failed to send kline subscription", "error", err)
		}
	})

	go func() {
		client.Start()
		<-ctx.Done()
		client.Stop()
	}()

	return nil
}

func (e *BitgetExchange) StopKlineStream() error {
	return nil
}

func (e *BitgetExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *BitgetExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultBitgetURL
	}

	gran := interval
	if interval == "1H" {
		gran = "1h"
	}

	path := fmt.Sprintf("/api/v2/mix/market/history-candles?symbol=%s&productType=%s&granularity=%s&limit=%d",
		symbol, e.productType, gran, limit)
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Code string     `json:"code"`
		Msg  string     `json:"msg"`
		Data [][]string `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Code != "00000" {
		return nil, e.parseError(body)
	}

	candles := make([]*pb.Candle, 0, len(response.Data))
	for _, item := range response.Data {
		if len(item) < 6 {
			continue
		}
		ts, _ := strconv.ParseInt(item[0], 10, 64)
		o, _ := decimal.NewFromString(item[1])
		h, _ := decimal.NewFromString(item[2])
		l, _ := decimal.NewFromString(item[3])
		c, _ := decimal.NewFromString(item[4])
		v, _ := decimal.NewFromString(item[5])

		candles = append(candles, &pb.Candle{
			Symbol:    symbol,
			Open:      pbu.FromGoDecimal(o),
			High:      pbu.FromGoDecimal(h),
			Low:       pbu.FromGoDecimal(l),
			Close:     pbu.FromGoDecimal(c),
			Volume:    pbu.FromGoDecimal(v),
			Timestamp: ts,
			IsClosed:  true,
		})
	}

	return candles, nil
}

func (e *BitgetExchange) FetchExchangeInfo(ctx context.Context, symbol string) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultBitgetURL
	}

	path := "/api/v2/mix/market/contracts"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	q := req.URL.Query()
	q.Add("productType", e.productType)
	req.URL.RawQuery = q.Encode()

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol         string `json:"symbol"`
			BaseCoin       string `json:"baseCoin"`
			QuoteCoin      string `json:"quoteCoin"`
			PricePlace     string `json:"pricePlace"`
			VolumePlace    string `json:"volumePlace"`
			PriceEndStep   string `json:"priceEndStep"`
			MinTradeNum    string `json:"minTradeNum"`
			SizeMultiplier string `json:"sizeMultiplier"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}

	if response.Code != "00000" {
		return e.parseError(body)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, s := range response.Data {
		pricePrec, _ := strconv.Atoi(s.PricePlace)
		qtyPrec, _ := strconv.Atoi(s.VolumePlace)
		tickSize, _ := decimal.NewFromString(s.PriceEndStep)
		minQty, _ := decimal.NewFromString(s.MinTradeNum)

		info := &pb.SymbolInfo{
			Symbol:            s.Symbol,
			BaseAsset:         s.BaseCoin,
			QuoteAsset:        s.QuoteCoin,
			PricePrecision:    int32(pricePrec),
			QuantityPrecision: int32(qtyPrec),
			TickSize:          pbu.FromGoDecimal(tickSize),
			MinQuantity:       pbu.FromGoDecimal(minQty),
			StepSize:          pbu.FromGoDecimal(minQty),
		}

		e.symbolInfo[s.Symbol] = info
	}

	return nil
}

func (e *BitgetExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
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

func (e *BitgetExchange) GetSymbols(ctx context.Context) ([]string, error) {
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

func (e *BitgetExchange) GetPriceDecimals() int {
	return 2
}

func (e *BitgetExchange) GetQuantityDecimals() int {
	return 3
}

func (e *BitgetExchange) GetBaseAsset() string {
	return "BTC"
}

func (e *BitgetExchange) GetQuoteAsset() string {
	return "USDT"
}

// StartAccountStream implements account balance streaming via polling
func (e *BitgetExchange) StartAccountStream(ctx context.Context, callback func(*pb.Account)) error {
	return e.StartPollingStream(ctx, func(ctx context.Context) (interface{}, error) {
		return e.GetAccount(ctx)
	}, func(data interface{}) {
		callback(data.(*pb.Account))
	}, 5*time.Second, "AccountStream")
}

// StartPositionStream implements position streaming via polling
func (e *BitgetExchange) StartPositionStream(ctx context.Context, callback func(*pb.Position)) error {
	return e.StartPollingStream(ctx, func(ctx context.Context) (interface{}, error) {
		return e.GetPositions(ctx, "")
	}, func(data interface{}) {
		positions := data.([]*pb.Position)
		for _, position := range positions {
			callback(position)
		}
	}, 5*time.Second, "PositionStream")
}

func (e *BitgetExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *BitgetExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *BitgetExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *BitgetExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *BitgetExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *BitgetExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	return fmt.Errorf("not implemented")
}
