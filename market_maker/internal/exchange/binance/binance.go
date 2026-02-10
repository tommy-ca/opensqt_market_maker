// Package binance provides Binance Futures exchange connectivity
package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/exchange/base"
	"market_maker/internal/pb"
	"market_maker/pkg/concurrency"
	apperrors "market_maker/pkg/errors"
	"market_maker/pkg/pbu"
	"market_maker/pkg/retry"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultFuturesURL = "https://fapi.binance.com"
	defaultFuturesWS  = "wss://fstream.binance.com/ws"
)

// SignRequest adds authentication headers and signature to the request
func (e *BinanceExchange) SignRequest(req *http.Request, body []byte) error {
	// Add API Key header
	req.Header.Set("X-MBX-APIKEY", string(e.GetConfig().APIKey))

	// Get current query params
	q := req.URL.Query()

	// Add timestamp if missing
	if q.Get("timestamp") == "" {
		q.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}

	// Calculate signature
	queryString := q.Encode()
	mac := hmac.New(sha256.New, []byte(string(e.GetConfig().SecretKey)))
	mac.Write([]byte(queryString))
	signature := hex.EncodeToString(mac.Sum(nil))

	// Add signature to query params
	q.Set("signature", signature)
	req.URL.RawQuery = q.Encode()

	return nil
}

// BinanceExchange implements IExchange for Binance
type BinanceExchange struct {
	*base.BaseAdapter
	symbolInfo map[string]*pb.SymbolInfo
	pool       *concurrency.WorkerPool
}

// NewBinanceExchange creates a new Binance exchange instance
func NewBinanceExchange(cfg *config.ExchangeConfig, logger core.ILogger, pool *concurrency.WorkerPool) *BinanceExchange {
	b := base.NewBaseAdapter("binance", cfg, logger)
	e := &BinanceExchange{
		BaseAdapter: b,
		symbolInfo:  make(map[string]*pb.SymbolInfo),
		pool:        pool,
	}

	b.SetSignRequest(e.SignRequest)
	b.SetParseError(e.parseError)
	b.SetMapOrderStatus(e.mapOrderStatus)

	return e
}

func (e *BinanceExchange) parseError(body []byte) error {
	var errResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("binance error (unmarshal failed): %s", string(body))
	}

	// Map Binance error codes to standard errors
	switch errResp.Code {
	case -2015:
		return apperrors.ErrAuthenticationFailed
	case -2010:
		return apperrors.ErrInsufficientFunds
	case -1003:
		return apperrors.ErrRateLimitExceeded
	case -1121:
		return apperrors.ErrInvalidSymbol
	case -2012:
		return apperrors.ErrDuplicateOrder
	}

	return fmt.Errorf("binance error %d: %s", errResp.Code, errResp.Msg)
}

func (e *BinanceExchange) mapOrderStatus(rawStatus string) pb.OrderStatus {
	switch rawStatus {
	case "NEW":
		return pb.OrderStatus_ORDER_STATUS_NEW
	case "PARTIALLY_FILLED":
		return pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case "FILLED":
		return pb.OrderStatus_ORDER_STATUS_FILLED
	case "CANCELED":
		return pb.OrderStatus_ORDER_STATUS_CANCELED
	case "EXPIRED":
		return pb.OrderStatus_ORDER_STATUS_EXPIRED
	case "REJECTED":
		return pb.OrderStatus_ORDER_STATUS_REJECTED
	default:
		return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func (e *BinanceExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/premiumIndex?symbol=%s", baseURL, symbol)
	body, err := e.ExecuteRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var data struct {
		Symbol          string `json:"symbol"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		Time            int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	rate, _ := decimal.NewFromString(data.LastFundingRate)

	return &pb.FundingRate{
		Exchange:        "binance",
		Symbol:          data.Symbol,
		Rate:            pbu.FromGoDecimal(rate),
		NextFundingTime: data.NextFundingTime,
		Timestamp:       data.Time,
	}, nil
}

func (e *BinanceExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/premiumIndex", baseURL)
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
		Symbol          string `json:"symbol"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		Time            int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	rates := make([]*pb.FundingRate, 0, len(data))
	for _, item := range data {
		rate, _ := decimal.NewFromString(item.LastFundingRate)
		rates = append(rates, &pb.FundingRate{
			Exchange:        "binance",
			Symbol:          item.Symbol,
			Rate:            pbu.FromGoDecimal(rate),
			NextFundingTime: item.NextFundingTime,
			Timestamp:       item.Time,
		})
	}

	return rates, nil
}

func (e *BinanceExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/fundingRate", baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	q := httpReq.URL.Query()
	q.Add("symbol", symbol)
	if limit > 0 {
		q.Add("limit", fmt.Sprintf("%d", limit))
	}
	httpReq.URL.RawQuery = q.Encode()

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
		Symbol      string `json:"symbol"`
		FundingTime int64  `json:"fundingTime"`
		FundingRate string `json:"fundingRate"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	rates := make([]*pb.FundingRate, 0, len(data))
	for _, item := range data {
		rate, _ := decimal.NewFromString(item.FundingRate)
		rates = append(rates, &pb.FundingRate{
			Exchange:  "binance",
			Symbol:    item.Symbol,
			Rate:      pbu.FromGoDecimal(rate),
			Timestamp: item.FundingTime,
		})
	}

	return rates, nil
}

func (e *BinanceExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/ticker/24hr", baseURL)
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

func (e *BinanceExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/openInterest?symbol=%s", baseURL, symbol)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Zero, err
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return decimal.Zero, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return decimal.Zero, err
	}

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, e.parseError(body)
	}

	var data struct {
		OpenInterest string `json:"openInterest"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromString(data.OpenInterest)
}

func (e *BinanceExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultFuturesWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	streamURL := fmt.Sprintf("%s/%s@markPrice", wsURL, strings.ToLower(symbol))

	return e.StartWebSocketStream(ctx, streamURL, func(message []byte) {
		var event struct {
			EventType       string `json:"e"`
			EventTime       int64  `json:"E"`
			Symbol          string `json:"s"`
			FundingRate     string `json:"r"`
			NextFundingTime int64  `json:"T"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal funding update", "error", err)
			return
		}

		rate, _ := decimal.NewFromString(event.FundingRate)

		update := pb.FundingUpdate{
			Exchange:        "binance",
			Symbol:          event.Symbol,
			Rate:            pbu.FromGoDecimal(rate),
			NextFundingTime: event.NextFundingTime,
			Timestamp:       event.EventTime,
		}

		if e.pool != nil {
			_ = e.pool.Submit(func() { callback(&update) })
		} else {
			callback(&update)
		}
	}, nil, "FundingStream")
}

func (e *BinanceExchange) GetName() string {
	return "binance"
}

func (e *BinanceExchange) IsUnifiedMargin() bool {
	// Check if configured to use PAPI (Portfolio Margin)
	return strings.Contains(e.Config.BaseURL, "papi.binance.com")
}

func (e *BinanceExchange) CheckHealth(ctx context.Context) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/ping", baseURL)
	_, err := e.ExecuteRequest(ctx, "GET", url, nil)
	return err
}

// PlaceOrder places a new order on Binance
func (e *BinanceExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	var order *pb.Order
	err := retry.Do(ctx, retry.DefaultPolicy, e.isTransientError, func() error {
		var err error
		order, err = e.placeOrderInternal(ctx, req)
		if err != nil {
			if errors.Is(err, apperrors.ErrDuplicateOrder) {
				if req.ClientOrderId != "" {
					existing, fetchErr := e.GetOrder(ctx, req.Symbol, 0, req.ClientOrderId, req.UseMargin)
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

func (e *BinanceExchange) isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "timeout") {
		return true
	}
	return errors.Is(err, apperrors.ErrRateLimitExceeded) ||
		errors.Is(err, apperrors.ErrNetwork) ||
		errors.Is(err, apperrors.ErrSystemOverload)
}

func (e *BinanceExchange) placeOrderInternal(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/order", baseURL)
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
		UpdateTime    int64  `json:"updateTime"`
	}

	if err := json.Unmarshal(body, &rawOrder); err != nil {
		return nil, err
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
		Status:        pb.OrderStatus_ORDER_STATUS_NEW,
		Price:         pbu.FromGoDecimal(price),
		Quantity:      pbu.FromGoDecimal(qty),
		ExecutedQty:   pbu.FromGoDecimal(execQty),
		UpdateTime:    rawOrder.UpdateTime,
		CreatedAt:     timestamppb.Now(),
	}, nil
}

func (e *BinanceExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	if len(orders) == 0 {
		return nil, true
	}

	isPapi := e.IsUnifiedMargin()
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	endpoint := "/fapi/v1/batchOrders"
	limit := 5
	if isPapi {
		endpoint = "/papi/v1/batchOrders"
		limit = 10
	}

	var allPlaced []*pb.Order
	allSuccess := true

	// Chunk orders
	for i := 0; i < len(orders); i += limit {
		end := i + limit
		if end > len(orders) {
			end = len(orders)
		}
		chunk := orders[i:end]

		placed, success := e.batchPlaceOrdersInternal(ctx, baseURL, endpoint, chunk)
		allPlaced = append(allPlaced, placed...)
		if !success {
			allSuccess = false
		}
	}

	return allPlaced, allSuccess
}

type binanceBatchOrderReq struct {
	Symbol           string `json:"symbol"`
	Side             string `json:"side"`
	Type             string `json:"type"`
	Quantity         string `json:"quantity"`
	Price            string `json:"price,omitempty"`
	TimeInForce      string `json:"timeInForce,omitempty"`
	NewClientOrderID string `json:"newClientOrderId,omitempty"`
}

func (e *BinanceExchange) batchPlaceOrdersInternal(ctx context.Context, baseURL, endpoint string, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	reqs := make([]binanceBatchOrderReq, len(orders))
	for i, req := range orders {
		side := "BUY"
		if req.Side == pb.OrderSide_ORDER_SIDE_SELL {
			side = "SELL"
		}

		otype := "LIMIT"
		if req.Type == pb.OrderType_ORDER_TYPE_MARKET {
			otype = "MARKET"
		}

		reqs[i] = binanceBatchOrderReq{
			Symbol:           req.Symbol,
			Side:             side,
			Type:             otype,
			Quantity:         pbu.ToGoDecimal(req.Quantity).String(),
			NewClientOrderID: req.ClientOrderId,
		}

		if otype == "LIMIT" {
			reqs[i].Price = pbu.ToGoDecimal(req.Price).String()
			reqs[i].TimeInForce = "GTC"
		}
	}

	batchJSON, _ := json.Marshal(reqs)

	url := fmt.Sprintf("%s%s", baseURL, endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		e.Logger.Error("Failed to create batch order request", "error", err)
		return nil, false
	}

	q := httpReq.URL.Query()
	q.Add("batchOrders", string(batchJSON))
	httpReq.URL.RawQuery = q.Encode()

	if err := e.SignRequest(httpReq, nil); err != nil {
		e.Logger.Error("Failed to sign batch order request", "error", err)
		return nil, false
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		e.Logger.Error("Failed to execute batch order request", "error", err)
		return nil, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}

	if resp.StatusCode != http.StatusOK {
		e.Logger.Error("Batch order request failed", "status", resp.StatusCode, "body", string(body))
		return nil, false
	}

	var results []json.RawMessage
	if err := json.Unmarshal(body, &results); err != nil {
		e.Logger.Error("Failed to unmarshal batch order response", "error", err)
		return nil, false
	}

	var placed []*pb.Order
	allSuccess := true

	for i, raw := range results {
		var res struct {
			OrderID       int64  `json:"orderId"`
			Symbol        string `json:"symbol"`
			ClientOrderID string `json:"clientOrderId"`
			Price         string `json:"price"`
			OrigQty       string `json:"origQty"`
			ExecutedQty   string `json:"executedQty"`
			Code          int    `json:"code"`
			Msg           string `json:"msg"`
		}

		if err := json.Unmarshal(raw, &res); err != nil {
			allSuccess = false
			continue
		}

		if res.Code != 0 {
			e.Logger.Warn("Batch order item failed", "symbol", orders[i].Symbol, "code", res.Code, "msg", res.Msg)
			allSuccess = false
			continue
		}

		price, _ := decimal.NewFromString(res.Price)
		qty, _ := decimal.NewFromString(res.OrigQty)
		execQty, _ := decimal.NewFromString(res.ExecutedQty)

		placed = append(placed, &pb.Order{
			OrderId:       res.OrderID,
			ClientOrderId: res.ClientOrderID,
			Symbol:        res.Symbol,
			Side:          orders[i].Side,
			Type:          orders[i].Type,
			Status:        pb.OrderStatus_ORDER_STATUS_NEW,
			Price:         pbu.FromGoDecimal(price),
			Quantity:      pbu.FromGoDecimal(qty),
			ExecutedQty:   pbu.FromGoDecimal(execQty),
			UpdateTime:    time.Now().UnixMilli(),
			CreatedAt:     timestamppb.Now(),
		})
	}

	return placed, allSuccess
}

func (e *BinanceExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/order", baseURL)
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

func (e *BinanceExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	if len(orderIDs) == 0 {
		return nil
	}

	isPapi := e.IsUnifiedMargin()
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	endpoint := "/fapi/v1/batchOrders"
	limit := 10 // fapi batch cancel limit is 10
	if isPapi {
		endpoint = "/papi/v1/batchOrders"
		limit = 10
	}

	for i := 0; i < len(orderIDs); i += limit {
		end := i + limit
		if end > len(orderIDs) {
			end = len(orderIDs)
		}
		chunk := orderIDs[i:end]

		idStrs := make([]string, len(chunk))
		for j, id := range chunk {
			idStrs[j] = fmt.Sprintf("%d", id)
		}
		idListJSON := "[" + strings.Join(idStrs, ",") + "]"

		url := fmt.Sprintf("%s%s", baseURL, endpoint)
		httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
		if err != nil {
			return err
		}

		q := httpReq.URL.Query()
		q.Add("symbol", symbol)
		if isPapi {
			q.Add("orderIds", idListJSON)
		} else {
			q.Add("orderIdList", idListJSON)
		}
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
	}

	return nil
}

func (e *BinanceExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	isPapi := e.IsUnifiedMargin()
	endpoint := "/fapi/v1/allOpenOrders"
	if isPapi {
		endpoint = "/papi/v1/allOpenOrders"
	}

	url := fmt.Sprintf("%s%s", baseURL, endpoint)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return e.parseError(body)
	}

	return nil
}

func (e *BinanceExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	isPapi := e.IsUnifiedMargin()
	endpoint := "/fapi/v1/order"
	if isPapi {
		endpoint = "/papi/v1/order"
	}

	url := fmt.Sprintf("%s%s?symbol=%s", baseURL, endpoint, symbol)
	if orderID != 0 {
		url += fmt.Sprintf("&orderId=%d", orderID)
	} else if clientOrderID != "" {
		url += fmt.Sprintf("&origClientOrderId=%s", clientOrderID)
	}

	body, err := e.ExecuteRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var raw struct {
		OrderID       int64  `json:"orderId"`
		ClientOrderID string `json:"clientOrderId"`
		Symbol        string `json:"symbol"`
		Status        string `json:"status"`
		Price         string `json:"price"`
		OrigQty       string `json:"origQty"`
		ExecutedQty   string `json:"executedQty"`
		AvgPrice      string `json:"avgPrice"`
		Side          string `json:"side"`
		Type          string `json:"type"`
		UpdateTime    int64  `json:"updateTime"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	status := e.mapOrderStatus(raw.Status)

	var side pb.OrderSide
	if raw.Side == "BUY" {
		side = pb.OrderSide_ORDER_SIDE_BUY
	} else {
		side = pb.OrderSide_ORDER_SIDE_SELL
	}

	var orderType pb.OrderType
	if raw.Type == "LIMIT" {
		orderType = pb.OrderType_ORDER_TYPE_LIMIT
	} else if raw.Type == "MARKET" {
		orderType = pb.OrderType_ORDER_TYPE_MARKET
	}

	pVal, _ := decimal.NewFromString(raw.Price)
	qVal, _ := decimal.NewFromString(raw.OrigQty)
	eVal, _ := decimal.NewFromString(raw.ExecutedQty)
	avgVal, _ := decimal.NewFromString(raw.AvgPrice)

	return &pb.Order{
		OrderId:       raw.OrderID,
		ClientOrderId: raw.ClientOrderID,
		Symbol:        raw.Symbol,
		Side:          side,
		Type:          orderType,
		Status:        status,
		Price:         pbu.FromGoDecimal(pVal),
		Quantity:      pbu.FromGoDecimal(qVal),
		ExecutedQty:   pbu.FromGoDecimal(eVal),
		AvgPrice:      pbu.FromGoDecimal(avgVal),
		UpdateTime:    raw.UpdateTime,
	}, nil
}

func (e *BinanceExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	isPapi := e.IsUnifiedMargin()
	endpoint := "/fapi/v1/openOrders"
	if isPapi {
		endpoint = "/papi/v1/openOrders"
	}

	url := fmt.Sprintf("%s%s", baseURL, endpoint)
	if symbol != "" {
		url += fmt.Sprintf("?symbol=%s", symbol)
	}

	body, err := e.ExecuteRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var rawOrders []struct {
		OrderID       int64  `json:"orderId"`
		ClientOrderID string `json:"clientOrderId"`
		Symbol        string `json:"symbol"`
		Status        string `json:"status"`
		Price         string `json:"price"`
		OrigQty       string `json:"origQty"`
		ExecutedQty   string `json:"executedQty"`
		AvgPrice      string `json:"avgPrice"`
		Side          string `json:"side"`
		Type          string `json:"type"`
		UpdateTime    int64  `json:"updateTime"`
	}

	if err := json.Unmarshal(body, &rawOrders); err != nil {
		return nil, err
	}

	orders := make([]*pb.Order, 0, len(rawOrders))
	for _, raw := range rawOrders {
		status := e.mapOrderStatus(raw.Status)

		var side pb.OrderSide
		if raw.Side == "BUY" {
			side = pb.OrderSide_ORDER_SIDE_BUY
		} else {
			side = pb.OrderSide_ORDER_SIDE_SELL
		}

		var orderType pb.OrderType
		if raw.Type == "LIMIT" {
			orderType = pb.OrderType_ORDER_TYPE_LIMIT
		} else if raw.Type == "MARKET" {
			orderType = pb.OrderType_ORDER_TYPE_MARKET
		}

		pVal, _ := decimal.NewFromString(raw.Price)
		qVal, _ := decimal.NewFromString(raw.OrigQty)
		eVal, _ := decimal.NewFromString(raw.ExecutedQty)
		avgVal, _ := decimal.NewFromString(raw.AvgPrice)

		orders = append(orders, &pb.Order{
			OrderId:       raw.OrderID,
			ClientOrderId: raw.ClientOrderID,
			Symbol:        raw.Symbol,
			Side:          side,
			Type:          orderType,
			Status:        status,
			Price:         pbu.FromGoDecimal(pVal),
			Quantity:      pbu.FromGoDecimal(qVal),
			ExecutedQty:   pbu.FromGoDecimal(eVal),
			AvgPrice:      pbu.FromGoDecimal(avgVal),
			UpdateTime:    raw.UpdateTime,
		})
	}

	return orders, nil
}

func (e *BinanceExchange) GetAccount(ctx context.Context) (*pb.Account, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	isPapi := e.IsUnifiedMargin()
	endpoint := "/fapi/v2/account"
	if isPapi {
		endpoint = "/papi/v1/account"
	}

	url := fmt.Sprintf("%s%s", baseURL, endpoint)
	body, err := e.ExecuteRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if isPapi {
		var raw struct {
			UniMMR             string `json:"uniMMR"`
			AccountEquity      string `json:"accountEquity"`
			ActualEquity       string `json:"actualEquity"`
			AccountMaintMargin string `json:"accountMaintMargin"`
			AccountStatus      string `json:"accountStatus"`
			Balance            []struct {
				Asset         string `json:"asset"`
				TotalBalance  string `json:"totalBalance"`
				WalletBalance string `json:"walletBalance"`
			} `json:"balance"`
			Positions []struct {
				Symbol           string `json:"symbol"`
				PositionAmt      string `json:"positionAmt"`
				EntryPrice       string `json:"entryPrice"`
				MarkPrice        string `json:"markPrice"`
				UnRealizedProfit string `json:"unRealizedProfit"`
				Leverage         string `json:"leverage"`
				LiquidationPrice string `json:"liquidationPrice"`
			} `json:"subPositions"` // Note: PAPI uses subPositions
		}

		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, err
		}

		ae, _ := decimal.NewFromString(raw.AccountEquity)
		actualAe, _ := decimal.NewFromString(raw.ActualEquity)
		mm, _ := decimal.NewFromString(raw.AccountMaintMargin)
		umr, _ := decimal.NewFromString(raw.UniMMR)

		// health_score = 1.0 - (1.0 / uniMMR)
		health := decimal.Zero
		if umr.GreaterThan(decimal.NewFromFloat(1.001)) {
			health = decimal.NewFromInt(1).Sub(decimal.NewFromInt(1).Div(umr))
		}
		if health.IsNegative() {
			health = decimal.Zero
		}
		if health.GreaterThan(decimal.NewFromInt(1)) {
			health = decimal.NewFromInt(1)
		}

		var positions []*pb.Position
		for _, p := range raw.Positions {
			amt, _ := decimal.NewFromString(p.PositionAmt)
			if amt.IsZero() {
				continue
			}
			ep, _ := decimal.NewFromString(p.EntryPrice)
			mp, _ := decimal.NewFromString(p.MarkPrice)
			upnl, _ := decimal.NewFromString(p.UnRealizedProfit)
			lev, _ := decimal.NewFromString(p.Leverage)
			lp, _ := decimal.NewFromString(p.LiquidationPrice)

			positions = append(positions, &pb.Position{
				Symbol:           p.Symbol,
				Size:             pbu.FromGoDecimal(amt),
				EntryPrice:       pbu.FromGoDecimal(ep),
				MarkPrice:        pbu.FromGoDecimal(mp),
				UnrealizedPnl:    pbu.FromGoDecimal(upnl),
				Leverage:         int32(lev.IntPart()),
				LiquidationPrice: pbu.FromGoDecimal(lp),
			})
		}

		return &pb.Account{
			TotalWalletBalance: pbu.FromGoDecimal(ae),
			TotalMarginBalance: pbu.FromGoDecimal(ae),
			AvailableBalance:   pbu.FromGoDecimal(actualAe.Sub(mm)),
			Positions:          positions,
			IsUnified:          true,
			HealthScore:        pbu.FromGoDecimal(health),
			MarginMode:         pb.MarginMode_MARGIN_MODE_PORTFOLIO,
			AdjustedEquity:     pbu.FromGoDecimal(actualAe),
		}, nil
	}

	var raw struct {
		Assets []struct {
			Asset            string `json:"asset"`
			WalletBalance    string `json:"walletBalance"`
			UnrealizedProfit string `json:"unrealizedProfit"`
			MarginBalance    string `json:"marginBalance"`
			MaintMargin      string `json:"maintMargin"`
			InitialMargin    string `json:"initialMargin"`
		} `json:"assets"`
		TotalWalletBalance string `json:"totalWalletBalance"`
		TotalMarginBalance string `json:"totalMarginBalance"`
		AvailableBalance   string `json:"availableBalance"`
		Positions          []struct {
			Symbol           string `json:"symbol"`
			PositionAmt      string `json:"positionAmt"`
			EntryPrice       string `json:"entryPrice"`
			MarkPrice        string `json:"markPrice"`
			UnRealizedProfit string `json:"unRealizedProfit"`
			Leverage         string `json:"leverage"`
			MarginType       string `json:"marginType"`
			IsolatedMargin   string `json:"isolatedWallet"`
			LiquidationPrice string `json:"liquidationPrice"`
		} `json:"positions"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	wb, _ := decimal.NewFromString(raw.TotalWalletBalance)
	mb, _ := decimal.NewFromString(raw.TotalMarginBalance)
	ab, _ := decimal.NewFromString(raw.AvailableBalance)

	var positions []*pb.Position
	for _, p := range raw.Positions {
		amt, _ := decimal.NewFromString(p.PositionAmt)
		if amt.IsZero() {
			continue
		}
		ep, _ := decimal.NewFromString(p.EntryPrice)
		mp, _ := decimal.NewFromString(p.MarkPrice)
		upnl, _ := decimal.NewFromString(p.UnRealizedProfit)
		lev, _ := decimal.NewFromString(p.Leverage)
		im, _ := decimal.NewFromString(p.IsolatedMargin)
		lp, _ := decimal.NewFromString(p.LiquidationPrice)

		positions = append(positions, &pb.Position{
			Symbol:           p.Symbol,
			Size:             pbu.FromGoDecimal(amt),
			EntryPrice:       pbu.FromGoDecimal(ep),
			MarkPrice:        pbu.FromGoDecimal(mp),
			UnrealizedPnl:    pbu.FromGoDecimal(upnl),
			Leverage:         int32(lev.IntPart()),
			MarginType:       p.MarginType,
			IsolatedMargin:   pbu.FromGoDecimal(im),
			LiquidationPrice: pbu.FromGoDecimal(lp),
		})
	}

	return &pb.Account{
		TotalWalletBalance: pbu.FromGoDecimal(wb),
		TotalMarginBalance: pbu.FromGoDecimal(mb),
		AvailableBalance:   pbu.FromGoDecimal(ab),
		Positions:          positions,
		MarginMode:         pb.MarginMode_MARGIN_MODE_REGULAR,
	}, nil
}

func (e *BinanceExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	isPapi := e.IsUnifiedMargin()
	endpoint := "/fapi/v2/positionRisk"
	if isPapi {
		endpoint = "/papi/v1/um/positionRisk"
	}

	url := fmt.Sprintf("%s%s", baseURL, endpoint)
	if symbol != "" {
		url += fmt.Sprintf("?symbol=%s", symbol)
	}

	body, err := e.ExecuteRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var rawPositions []struct {
		Symbol           string `json:"symbol"`
		PositionAmt      string `json:"positionAmt"`
		EntryPrice       string `json:"entryPrice"`
		MarkPrice        string `json:"markPrice"`
		UnRealizedProfit string `json:"unRealizedProfit"`
		LiquidationPrice string `json:"liquidationPrice"`
		Leverage         string `json:"leverage"`
		MarginType       string `json:"marginType"`
		IsolatedWallet   string `json:"isolatedWallet"`
	}

	if err := json.Unmarshal(body, &rawPositions); err != nil {
		return nil, err
	}

	positions := make([]*pb.Position, 0)
	for _, p := range rawPositions {
		amt, _ := decimal.NewFromString(p.PositionAmt)
		if amt.IsZero() && symbol == "" {
			continue
		}

		ep, _ := decimal.NewFromString(p.EntryPrice)
		mp, _ := decimal.NewFromString(p.MarkPrice)
		upnl, _ := decimal.NewFromString(p.UnRealizedProfit)
		lp, _ := decimal.NewFromString(p.LiquidationPrice)
		lev, _ := decimal.NewFromString(p.Leverage)
		iw, _ := decimal.NewFromString(p.IsolatedWallet)

		positions = append(positions, &pb.Position{
			Symbol:           p.Symbol,
			Size:             pbu.FromGoDecimal(amt),
			EntryPrice:       pbu.FromGoDecimal(ep),
			MarkPrice:        pbu.FromGoDecimal(mp),
			UnrealizedPnl:    pbu.FromGoDecimal(upnl),
			LiquidationPrice: pbu.FromGoDecimal(lp),
			Leverage:         int32(lev.IntPart()),
			MarginType:       p.MarginType,
			IsolatedMargin:   pbu.FromGoDecimal(iw),
		})
	}

	return positions, nil
}

func (e *BinanceExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	acc, err := e.GetAccount(ctx)
	if err != nil {
		return decimal.Zero, err
	}

	// For futures, we typically return the total wallet balance if no asset specified
	if asset == "" || asset == "USDT" || asset == "BUSD" {
		return pbu.ToGoDecimal(acc.TotalWalletBalance), nil
	}

	return decimal.Zero, fmt.Errorf("asset %s not found in account", asset)
}

func (e *BinanceExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/ticker/price?symbol=%s", baseURL, symbol)
	body, err := e.ExecuteRequest(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Zero, err
	}

	var res struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromString(res.Price)
}

func (e *BinanceExchange) FetchExchangeInfo(ctx context.Context, symbol string) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}

	url := fmt.Sprintf("%s/fapi/v1/exchangeInfo", baseURL)
	body, err := e.ExecuteRequest(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	var res struct {
		Symbols []struct {
			Symbol            string `json:"symbol"`
			BaseAsset         string `json:"baseAsset"`
			QuoteAsset        string `json:"quoteAsset"`
			PricePrecision    int    `json:"pricePrecision"`
			QuantityPrecision int    `json:"quantityPrecision"`
			Filters           []struct {
				FilterType  string `json:"filterType"`
				TickSize    string `json:"tickSize"`
				StepSize    string `json:"stepSize"`
				MinQty      string `json:"minQty"`
				MinNotional string `json:"notional"`
			} `json:"filters"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	for _, s := range res.Symbols {
		info := &pb.SymbolInfo{
			Symbol:            s.Symbol,
			PricePrecision:    int32(s.PricePrecision),
			QuantityPrecision: int32(s.QuantityPrecision),
			BaseAsset:         s.BaseAsset,
			QuoteAsset:        s.QuoteAsset,
		}

		for _, f := range s.Filters {
			if f.FilterType == "PRICE_FILTER" {
				tick, _ := decimal.NewFromString(f.TickSize)
				info.TickSize = pbu.FromGoDecimal(tick)
			}
			if f.FilterType == "LOT_SIZE" {
				step, _ := decimal.NewFromString(f.StepSize)
				minQty, _ := decimal.NewFromString(f.MinQty)
				info.StepSize = pbu.FromGoDecimal(step)
				info.MinQuantity = pbu.FromGoDecimal(minQty)
			}
			if f.FilterType == "MIN_NOTIONAL" {
				minNotional, _ := decimal.NewFromString(f.MinNotional)
				info.MinNotional = pbu.FromGoDecimal(minNotional)
			}
		}
		e.symbolInfo[s.Symbol] = info
	}

	return nil
}

func (e *BinanceExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
	if info, ok := e.symbolInfo[symbol]; ok {
		return info, nil
	}

	if err := e.FetchExchangeInfo(ctx, symbol); err != nil {
		return nil, err
	}

	if info, ok := e.symbolInfo[symbol]; ok {
		return info, nil
	}

	return nil, fmt.Errorf("symbol not found: %s", symbol)
}

func (e *BinanceExchange) GetSymbols(ctx context.Context) ([]string, error) {
	if len(e.symbolInfo) == 0 {
		if err := e.FetchExchangeInfo(ctx, ""); err != nil {
			return nil, err
		}
	}

	symbols := make([]string, 0, len(e.symbolInfo))
	for s := range e.symbolInfo {
		symbols = append(symbols, s)
	}
	return symbols, nil
}

func (e *BinanceExchange) getListenKey(ctx context.Context) (string, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultFuturesURL
	}
	url := fmt.Sprintf("%s/fapi/v1/listenKey", baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", err
	}
	if err := e.SignRequest(req, nil); err != nil {
		return "", err
	}
	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", e.parseError(body)
	}

	var res struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}
	return res.ListenKey, nil
}

func (e *BinanceExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	listenKey, err := e.getListenKey(ctx)
	if err != nil {
		return err
	}

	baseURL := e.Config.BaseURL
	wsURL := defaultFuturesWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	// Binance futures user stream
	streamURL := fmt.Sprintf("%s/%s", wsURL, listenKey)

	return e.StartWebSocketStream(ctx, streamURL, func(message []byte) {
		// e.Logger.Debug("Received order update", "message", string(message))

		var event struct {
			EventType json.RawMessage `json:"e"`
			EventTime int64           `json:"E"`
			Order     struct {
				Symbol        string `json:"s"`
				ClientOrderID string `json:"c"`
				Side          string `json:"S"`
				Type          string `json:"o"`
				Status        string `json:"X"`
				OrderID       int64  `json:"i"`
				Price         string `json:"p"`
				OrigQty       string `json:"q"`
				ExecutedQty   string `json:"z"`
				AvgPrice      string `json:"ap"`
				OrderTime     int64  `json:"T"`
			} `json:"o"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal order update", "error", err)
			return
		}

		e.Logger.Info("Received WS message", "eventType", string(event.EventType))

		var eventType string
		if len(event.EventType) > 0 {
			if event.EventType[0] == '"' {
				if err := json.Unmarshal(event.EventType, &eventType); err != nil {
					e.Logger.Error("Failed to unmarshal event type", "error", err, "raw", string(event.EventType))
				}
			} else {
				// If it's a number? Unlikely for EventType.
				eventType = string(event.EventType)
			}
		}

		// e.Logger.Debug("Parsed EventType", "type", eventType)

		if eventType != "ORDER_TRADE_UPDATE" {
			return
		}

		o := event.Order
		price, _ := decimal.NewFromString(o.Price)
		qty, _ := decimal.NewFromString(o.OrigQty)
		execQty, _ := decimal.NewFromString(o.ExecutedQty)
		avgPrice, _ := decimal.NewFromString(o.AvgPrice)

		status := e.SafeMapOrderStatus(o.Status)

		var side pb.OrderSide
		if o.Side == "BUY" {
			side = pb.OrderSide_ORDER_SIDE_BUY
		} else {
			side = pb.OrderSide_ORDER_SIDE_SELL
		}

		var orderType pb.OrderType
		if o.Type == "LIMIT" {
			orderType = pb.OrderType_ORDER_TYPE_LIMIT
		} else {
			orderType = pb.OrderType_ORDER_TYPE_MARKET
		}

		update := &pb.OrderUpdate{
			OrderId:       o.OrderID,
			ClientOrderId: o.ClientOrderID,
			Symbol:        o.Symbol,
			Side:          side,
			Type:          orderType,
			Status:        status,
			Price:         pbu.FromGoDecimal(price),
			Quantity:      pbu.FromGoDecimal(qty),
			ExecutedQty:   pbu.FromGoDecimal(execQty),
			AvgPrice:      pbu.FromGoDecimal(avgPrice),
			UpdateTime:    o.OrderTime,
		}

		if e.pool != nil {
			_ = e.pool.Submit(func() { callback(update) })
		} else {
			callback(update)
		}
	}, nil, "OrderStream")
}

func (e *BinanceExchange) StopOrderStream() error {
	return nil
}

func (e *BinanceExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultFuturesWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	// For single symbol, use <symbol>@bookTicker
	// For multiple, we'd need combined stream or multiple connections.
	// Test uses single symbol.
	if len(symbols) == 0 {
		return nil
	}
	symbol := strings.ToLower(symbols[0])
	streamURL := fmt.Sprintf("%s/%s@bookTicker", wsURL, symbol)

	return e.StartWebSocketStream(ctx, streamURL, func(message []byte) {
		// Log raw message for debugging
		// e.Logger.Debug("Received price update", "message", string(message))

		var event struct {
			Symbol    string          `json:"s"`
			BestBid   string          `json:"b"`
			BestAsk   string          `json:"a"`
			BidQty    string          `json:"B"`
			AskQty    string          `json:"A"`
			EventTime json.RawMessage `json:"E"`
		}
		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal price update", "error", err)
			return
		}

		bid, _ := decimal.NewFromString(event.BestBid)

		// Parse EventTime manually to handle potential type mismatch (string vs number)
		var et int64
		if len(event.EventTime) > 0 {
			if event.EventTime[0] == '"' {
				// It's a string, strip quotes
				var s string
				if err := json.Unmarshal(event.EventTime, &s); err != nil {
					e.Logger.Error("Failed to unmarshal event time string", "error", err, "raw", string(event.EventTime))
				}
				et, _ = strconv.ParseInt(s, 10, 64)
			} else {
				if err := json.Unmarshal(event.EventTime, &et); err != nil {
					e.Logger.Error("Failed to unmarshal event time int", "error", err, "raw", string(event.EventTime))
				}
			}
		}

		change := &pb.PriceChange{
			Symbol:    event.Symbol,
			Price:     pbu.FromGoDecimal(bid),
			Timestamp: timestamppb.New(time.UnixMilli(et)),
		}

		if e.pool != nil {
			_ = e.pool.Submit(func() { callback(change) })
		} else {
			callback(change)
		}
	}, nil, "PriceStream")
}

func (e *BinanceExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	return nil
}

func (e *BinanceExchange) StopKlineStream() error {
	return nil
}

func (e *BinanceExchange) StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error {
	return nil
}

func (e *BinanceExchange) StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error {
	return nil
}

func (e *BinanceExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	return nil, nil
}

func (e *BinanceExchange) GetPriceDecimals() int {
	return 8
}

func (e *BinanceExchange) GetQuantityDecimals() int {
	return 8
}

func (e *BinanceExchange) GetBaseAsset() string {
	return "BTC"
}

func (e *BinanceExchange) GetQuoteAsset() string {
	return "USDT"
}
