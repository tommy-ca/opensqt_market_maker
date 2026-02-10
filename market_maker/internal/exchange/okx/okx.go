// Package okx provides OKX exchange implementation
package okx

import (
	"bytes"
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
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
	"market_maker/pkg/websocket"
)

const (
	defaultOKXURL = "https://www.okx.com"
	defaultOKXWS  = "wss://ws.okx.com:8443/ws/v5/public"  // Public for Tickers
	privateOKXWS  = "wss://ws.okx.com:8443/ws/v5/private" // Private for Orders
)

// OKXExchange implements IExchange for OKX
type OKXExchange struct {
	*base.BaseAdapter
	symbolInfo map[string]*pb.SymbolInfo
	mu         sync.RWMutex
}

// NewOKXExchange creates a new OKX exchange instance
func NewOKXExchange(cfg *config.ExchangeConfig, logger core.ILogger) (*OKXExchange, error) {
	if cfg.BaseURL != "" && !strings.HasPrefix(cfg.BaseURL, "https://") {
		// Allow http for local testing
		if !strings.Contains(cfg.BaseURL, "127.0.0.1") && !strings.Contains(cfg.BaseURL, "localhost") {
			return nil, fmt.Errorf("okx base URL must start with https://: %s", cfg.BaseURL)
		}
	}

	b := base.NewBaseAdapter("okx", cfg, logger)
	e := &OKXExchange{
		BaseAdapter: b,
		symbolInfo:  make(map[string]*pb.SymbolInfo),
	}

	b.SetSignRequest(func(req *http.Request, body []byte) error {
		return e.SignRequest(req, string(body))
	})
	b.SetParseError(e.parseError)
	b.SetMapOrderStatus(e.mapOrderStatus)

	return e, nil
}

// SignRequest adds authentication headers to the request
func (e *OKXExchange) SignRequest(req *http.Request, body string) error {
	// Timestamp: ISO 8601, e.g. 2020-12-08T09:08:57.715Z
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	method := req.Method
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	// message = timestamp + method + requestPath + body
	message := timestamp + method + path + body

	mac := hmac.New(sha256.New, []byte(e.Config.SecretKey))
	mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("OK-ACCESS-KEY", string(e.Config.APIKey))
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", string(e.Config.Passphrase))
	req.Header.Set("Content-Type", "application/json")

	return nil
}

func (e *OKXExchange) parseError(body []byte) error {
	var errResp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("okx error (unmarshal failed): %s", string(body))
	}

	// Map OKX error codes (strings)
	// https://www.okx.com/docs-v5/en/#error-code-details
	switch errResp.Code {
	case "0":
		return nil
	case "50004", "50011", "50027": // Params error, invalid param
		return apperrors.ErrInvalidOrderParameter
	case "50005", "50013": // Auth failed
		return apperrors.ErrAuthenticationFailed
	case "50014": // Rate limit
		return apperrors.ErrRateLimitExceeded
	case "51000": // Insufficient balance
		return apperrors.ErrInsufficientFunds
	case "51401": // Order doesn't exist
		return apperrors.ErrOrderNotFound
	case "51020": // PostOnly rule
		return apperrors.ErrOrderRejected
	case "50001": // Service temporarily unavailable
		return apperrors.ErrSystemOverload
	}

	return fmt.Errorf("okx error: %s (%s)", errResp.Msg, errResp.Code)
}

func (e *OKXExchange) mapOrderStatus(rawStatus string) pb.OrderStatus {
	switch rawStatus {
	case "live":
		return pb.OrderStatus_ORDER_STATUS_NEW
	case "partially_filled":
		return pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case "filled":
		return pb.OrderStatus_ORDER_STATUS_FILLED
	case "canceled":
		return pb.OrderStatus_ORDER_STATUS_CANCELED
	default:
		return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func (e *OKXExchange) GetName() string {
	return "okx"
}

func (e *OKXExchange) IsUnifiedMargin() bool {
	// OKX accounts are unified by default in V5
	return true
}

func (e *OKXExchange) CheckHealth(ctx context.Context) error {
	return nil
}

func (e *OKXExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
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

func (e *OKXExchange) isTransientError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, apperrors.ErrRateLimitExceeded) ||
		errors.Is(err, apperrors.ErrSystemOverload)
}

func (e *OKXExchange) placeOrderInternal(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	// OKX: buy/sell
	side := "buy"
	if req.Side == pb.OrderSide_ORDER_SIDE_SELL {
		side = "sell"
	}

	body := map[string]interface{}{
		"instId":  req.Symbol,
		"tdMode":  "cross", // Default to cross margin
		"side":    side,
		"ordType": "limit",
		"px":      pbu.ToGoDecimal(req.Price).String(),
		"sz":      pbu.ToGoDecimal(req.Quantity).String(),
	}

	if req.Type == pb.OrderType_ORDER_TYPE_MARKET {
		body["ordType"] = "market"
		delete(body, "px")
	}

	if req.ClientOrderId != "" {
		body["clOrdID"] = req.ClientOrderId
	}

	// OKX uses "posSide" for long/short in long/short mode, but default is net mode for single-currency margin?
	// For Swap/Futures in Net Mode (One-Way):
	// Buy -> Open Long (or Close Short if reduceOnly)
	// Sell -> Open Short (or Close Long if reduceOnly)
	// We assume Net Mode for simplicity or Cross Mode?
	// `tdMode`="cross" means Cross Margin.
	// If `posSide` is not sent, it's Net Mode.

	if req.ReduceOnly {
		body["reduceOnly"] = true
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	path := "/api/v5/trade/order"
	url := baseURL + path

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}

	if err := e.SignRequest(httpReq, string(jsonBody)); err != nil {
		return nil, err
	}

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
		Data []struct {
			OrdID   string `json:"ordId"`
			ClOrdID string `json:"clOrdID"`
			SCode   string `json:"sCode"`
			SMsg    string `json:"sMsg"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, err
	}

	if response.Code != "0" {
		return nil, fmt.Errorf("okx error: %s (%s)", response.Msg, response.Code)
	}
	if len(response.Data) == 0 {
		return nil, fmt.Errorf("okx error: no data returned")
	}

	data := response.Data[0]
	if data.SCode != "0" {
		// We can try to parse SCode too using parseError if we formatted it correctly,
		// but parseError expects standard {"code":...} structure.
		// We'll create a synthetic error body or just map manually here.
		// For consistency, let's map manually or reuse logic.
		// Example: "51020"
		errJSON := fmt.Sprintf(`{"code":"%s","msg":"%s"}`, data.SCode, data.SMsg)
		return nil, e.parseError([]byte(errJSON))
	}

	orderID, _ := strconv.ParseInt(data.OrdID, 10, 64)

	return &pb.Order{
		OrderId:       orderID,
		ClientOrderId: data.ClOrdID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Status:        pb.OrderStatus_ORDER_STATUS_NEW, // OKX response doesn't confirm status immediately, assume New
		Price:         req.Price,
		Quantity:      req.Quantity,
		CreatedAt:     timestamppb.Now(),
	}, nil
}

func (e *OKXExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	if len(orders) == 0 {
		return nil, true
	}

	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	limit := 20
	var allPlaced []*pb.Order
	allSuccess := true

	for i := 0; i < len(orders); i += limit {
		end := i + limit
		if end > len(orders) {
			end = len(orders)
		}
		chunk := orders[i:end]

		placed, success := e.batchPlaceOrdersInternal(ctx, baseURL, chunk)
		allPlaced = append(allPlaced, placed...)
		if !success {
			allSuccess = false
		}
	}

	return allPlaced, allSuccess
}

func (e *OKXExchange) batchPlaceOrdersInternal(ctx context.Context, baseURL string, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	reqs := make([]map[string]interface{}, len(orders))
	for i, req := range orders {
		side := "buy"
		if req.Side == pb.OrderSide_ORDER_SIDE_SELL {
			side = "sell"
		}

		otype := "limit"
		if req.Type == pb.OrderType_ORDER_TYPE_MARKET {
			otype = "market"
		}

		body := map[string]interface{}{
			"instId":  req.Symbol,
			"tdMode":  "cross",
			"side":    side,
			"ordType": otype,
			"sz":      pbu.ToGoDecimal(req.Quantity).String(),
		}

		if otype == "limit" {
			body["px"] = pbu.ToGoDecimal(req.Price).String()
		}

		if req.ClientOrderId != "" {
			body["clOrdID"] = req.ClientOrderId
		}

		if req.ReduceOnly {
			body["reduceOnly"] = true
		}

		reqs[i] = body
	}

	jsonBody, err := json.Marshal(reqs)
	if err != nil {
		return nil, false
	}

	path := "/api/v5/trade/batch-orders"
	url := baseURL + path

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, false
	}

	if err := e.SignRequest(httpReq, string(jsonBody)); err != nil {
		return nil, false
	}

	resp, err := e.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			OrdID   string `json:"ordId"`
			ClOrdID string `json:"clOrdID"`
			SCode   string `json:"sCode"`
			SMsg    string `json:"sMsg"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, false
	}

	if response.Code != "0" {
		e.Logger.Error("Batch order request failed", "code", response.Code, "msg", response.Msg)
		return nil, false
	}

	var placed []*pb.Order
	allSuccess := true

	for i, data := range response.Data {
		if data.SCode != "0" {
			e.Logger.Warn("Batch order item failed", "symbol", orders[i].Symbol, "code", data.SCode, "msg", data.SMsg)
			allSuccess = false
			continue
		}

		orderID, _ := strconv.ParseInt(data.OrdID, 10, 64)

		placed = append(placed, &pb.Order{
			OrderId:       orderID,
			ClientOrderId: data.ClOrdID,
			Symbol:        orders[i].Symbol,
			Side:          orders[i].Side,
			Type:          orders[i].Type,
			Status:        pb.OrderStatus_ORDER_STATUS_NEW,
			Price:         orders[i].Price,
			Quantity:      orders[i].Quantity,
			CreatedAt:     timestamppb.Now(),
		})
	}

	return placed, allSuccess
}

func (e *OKXExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	body := map[string]interface{}{
		"instId": symbol,
		"ordId":  fmt.Sprintf("%d", orderID),
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	path := "/api/v5/trade/cancel-order"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}

	if err := e.SignRequest(req, string(jsonBody)); err != nil {
		return err
	}

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
		Data []struct {
			OrdID string `json:"ordId"`
			SCode string `json:"sCode"`
			SMsg  string `json:"sMsg"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return err
	}

	if response.Code != "0" {
		return e.parseError(respBody)
	}

	if len(response.Data) > 0 {
		data := response.Data[0]
		if data.SCode != "0" {
			// Check if already cancelled?
			// OKX error 51401: Order doesn't exist
			if data.SCode == "51401" {
				return nil
			}
			errJSON := fmt.Sprintf(`{"code":"%s","msg":"%s"}`, data.SCode, data.SMsg)
			return e.parseError([]byte(errJSON))
		}
	}

	return nil
}

func (e *OKXExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIds []int64, useMargin bool) error {
	if len(orderIds) == 0 {
		return nil
	}

	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	limit := 20
	for i := 0; i < len(orderIds); i += limit {
		end := i + limit
		if end > len(orderIds) {
			end = len(orderIds)
		}
		chunk := orderIds[i:end]

		var requests []map[string]interface{}
		for _, id := range chunk {
			requests = append(requests, map[string]interface{}{
				"instId": symbol,
				"ordId":  fmt.Sprintf("%d", id),
			})
		}

		body, _ := json.Marshal(requests)
		url := baseURL + "/api/v5/trade/cancel-batch-orders"
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
		if err != nil {
			return err
		}

		if err := e.SignRequest(req, string(body)); err != nil {
			return err
		}

		resp, err := e.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return e.parseError(respBody)
		}

		// Also check individual results
		var response struct {
			Code string `json:"code"`
			Data []struct {
				SCode string `json:"sCode"`
				SMsg  string `json:"sMsg"`
			} `json:"data"`
		}
		if err := json.Unmarshal(respBody, &response); err == nil && response.Code == "0" {
			for _, data := range response.Data {
				if data.SCode != "0" && data.SCode != "51401" { // 51401 is "Order doesn't exist" (already cancelled)
					e.Logger.Warn("Batch cancel item failed", "symbol", symbol, "code", data.SCode, "msg", data.SMsg)
				}
			}
		}
	}

	return nil
}

func (e *OKXExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	orders, err := e.GetOpenOrders(ctx, symbol, false)
	if err != nil {
		return err
	}

	if len(orders) == 0 {
		return nil
	}

	ids := make([]int64, len(orders))
	for i, o := range orders {
		ids[i] = o.OrderId
	}

	return e.BatchCancelOrders(ctx, symbol, ids, useMargin)
}

func (e *OKXExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	path := fmt.Sprintf("/api/v5/trade/order?instId=%s", symbol)
	if orderID != 0 {
		path += fmt.Sprintf("&ordId=%d", orderID)
	} else if clientOrderID != "" {
		path += fmt.Sprintf("&clOrdID=%s", clientOrderID)
	}
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if err := e.SignRequest(req, ""); err != nil {
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

	if resp.StatusCode != http.StatusOK {
		return nil, e.parseError(body)
	}

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID    string `json:"instId"`
			OrdID     string `json:"ordId"`
			ClOrdID   string `json:"clOrdID"`
			Px        string `json:"px"`
			Sz        string `json:"sz"`
			Side      string `json:"side"`
			OrdType   string `json:"ordType"`
			State     string `json:"state"`
			AccFillSz string `json:"accFillSz"`
			AvgPx     string `json:"avgPx"`
			CTime     string `json:"cTime"`
			UTime     string `json:"uTime"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Code != "0" {
		return nil, e.parseError(body)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("order not found")
	}

	raw := response.Data[0]
	id, _ := strconv.ParseInt(raw.OrdID, 10, 64)
	p, _ := decimal.NewFromString(raw.Px)
	q, _ := decimal.NewFromString(raw.Sz)
	eq, _ := decimal.NewFromString(raw.AccFillSz)
	ap, _ := decimal.NewFromString(raw.AvgPx)
	ts, _ := strconv.ParseInt(raw.CTime, 10, 64)
	uts, _ := strconv.ParseInt(raw.UTime, 10, 64)

	var side pb.OrderSide
	if raw.Side == "buy" {
		side = pb.OrderSide_ORDER_SIDE_BUY
	} else if raw.Side == "sell" {
		side = pb.OrderSide_ORDER_SIDE_SELL
	}

	var otype pb.OrderType
	if raw.OrdType == "limit" {
		otype = pb.OrderType_ORDER_TYPE_LIMIT
	} else if raw.OrdType == "market" {
		otype = pb.OrderType_ORDER_TYPE_MARKET
	}

	var status pb.OrderStatus
	switch raw.State {
	case "live":
		status = pb.OrderStatus_ORDER_STATUS_NEW
	case "partially_filled":
		status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case "filled":
		status = pb.OrderStatus_ORDER_STATUS_FILLED
	case "canceled":
		status = pb.OrderStatus_ORDER_STATUS_CANCELED
	}

	return &pb.Order{
		OrderId:       id,
		ClientOrderId: raw.ClOrdID,
		Symbol:        raw.InstID,
		Side:          side,
		Type:          otype,
		Status:        status,
		Price:         pbu.FromGoDecimal(p),
		Quantity:      pbu.FromGoDecimal(q),
		ExecutedQty:   pbu.FromGoDecimal(eq),
		AvgPrice:      pbu.FromGoDecimal(ap),
		UpdateTime:    uts,
		CreatedAt:     timestamppb.New(time.UnixMilli(ts)),
	}, nil
}

func (e *OKXExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	path := "/api/v5/trade/orders-pending"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("instType", "SWAP") // Assume Swap
	if symbol != "" {
		q.Add("instId", symbol)
	}
	req.URL.RawQuery = q.Encode()

	if err := e.SignRequest(req, ""); err != nil {
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
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID    string `json:"instId"`
			OrdID     string `json:"ordId"`
			ClOrdID   string `json:"clOrdID"`
			Px        string `json:"px"`
			Sz        string `json:"sz"`
			Side      string `json:"side"`
			State     string `json:"state"`
			AccFillSz string `json:"accFillSz"`
			CTime     string `json:"cTime"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Code != "0" {
		return nil, e.parseError(body)
	}

	orders := make([]*pb.Order, len(response.Data))
	for i, raw := range response.Data {
		orderID, _ := strconv.ParseInt(raw.OrdID, 10, 64)
		price, _ := decimal.NewFromString(raw.Px)
		qty, _ := decimal.NewFromString(raw.Sz)
		execQty, _ := decimal.NewFromString(raw.AccFillSz)
		ts, _ := strconv.ParseInt(raw.CTime, 10, 64)

		var status pb.OrderStatus
		switch raw.State {
		case "live":
			status = pb.OrderStatus_ORDER_STATUS_NEW
		case "partially_filled":
			status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
		default:
			status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
		}

		var side pb.OrderSide
		if raw.Side == "buy" {
			side = pb.OrderSide_ORDER_SIDE_BUY
		} else {
			side = pb.OrderSide_ORDER_SIDE_SELL
		}

		orders[i] = &pb.Order{
			OrderId:       orderID,
			ClientOrderId: raw.ClOrdID,
			Symbol:        raw.InstID,
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

func (e *OKXExchange) GetAccount(ctx context.Context) (*pb.Account, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	// OKX uses "ccy" query param for single currency, or none for all
	// We assume USDT-margined usually
	path := "/api/v5/account/balance?ccy=USDT"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if err := e.SignRequest(req, ""); err != nil {
		return nil, err
	}

	resp, err := e.HTTPClient.Do(req)
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
		Data []struct {
			TotalEq  string `json:"totalEq"`  // Total Equity (USD)
			AdjEq    string `json:"adjEq"`    // Adjusted Equity (USD)
			MgnRatio string `json:"mgnRatio"` // Margin Ratio
			Details  []struct {
				Ccy     string `json:"ccy"`
				Eq      string `json:"eq"`      // Equity
				AvailEq string `json:"availEq"` // Available Equity
			} `json:"details"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, err
	}

	if response.Code != "0" {
		return nil, e.parseError(respBody)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("okx error: no account data")
	}

	data := response.Data[0]

	var avail, total decimal.Decimal
	for _, detail := range data.Details {
		if detail.Ccy == "USDT" {
			avail, _ = decimal.NewFromString(detail.AvailEq)
			total, _ = decimal.NewFromString(detail.Eq)
			break
		}
	}

	if total.IsZero() {
		total, _ = decimal.NewFromString(data.TotalEq)
		avail = total
	}

	adjEq, _ := decimal.NewFromString(data.AdjEq)
	mgnRatio, _ := decimal.NewFromString(data.MgnRatio)

	// OKX: Higher is safer. 1.0 (100%) is liquidation.
	// health_score = 1.0 - (1.0 / mgnRatio)
	health := decimal.Zero
	if mgnRatio.GreaterThan(decimal.NewFromFloat(1.001)) {
		health = decimal.NewFromInt(1).Sub(decimal.NewFromInt(1).Div(mgnRatio))
	}
	if health.IsNegative() {
		health = decimal.Zero
	}

	return &pb.Account{
		TotalWalletBalance: pbu.FromGoDecimal(total),
		AvailableBalance:   pbu.FromGoDecimal(avail),
		IsUnified:          true,
		HealthScore:        pbu.FromGoDecimal(health),
		MarginMode:         pb.MarginMode_MARGIN_MODE_UNIFIED,
		AdjustedEquity:     pbu.FromGoDecimal(adjEq),
	}, nil
}

func (e *OKXExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	path := "/api/v5/account/positions"
	if symbol != "" {
		path += "?instId=" + symbol
	}
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if err := e.SignRequest(req, ""); err != nil {
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
		Code string `json:"code"`
		Data []struct {
			InstID  string `json:"instId"`
			Pos     string `json:"pos"`
			AvgPx   string `json:"avgPx"`
			MarkPx  string `json:"markPx"`
			Upl     string `json:"upl"`
			Lever   string `json:"lever"`
			MgnMode string `json:"mgnMode"`
			LiqPx   string `json:"liqPx"`
			Margin  string `json:"margin"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Code != "0" {
		return nil, e.parseError(body)
	}

	positions := make([]*pb.Position, 0, len(response.Data))
	for _, raw := range response.Data {
		size, _ := decimal.NewFromString(raw.Pos)
		if size.IsZero() {
			continue
		}

		entry, _ := decimal.NewFromString(raw.AvgPx)
		mark, _ := decimal.NewFromString(raw.MarkPx)
		upnl, _ := decimal.NewFromString(raw.Upl)
		lev, _ := strconv.Atoi(raw.Lever)
		liq, _ := decimal.NewFromString(raw.LiqPx)
		margin, _ := decimal.NewFromString(raw.Margin)

		positions = append(positions, &pb.Position{
			Symbol:           raw.InstID,
			Size:             pbu.FromGoDecimal(size),
			EntryPrice:       pbu.FromGoDecimal(entry),
			MarkPrice:        pbu.FromGoDecimal(mark),
			UnrealizedPnl:    pbu.FromGoDecimal(upnl),
			Leverage:         int32(lev),
			MarginType:       raw.MgnMode,
			LiquidationPrice: pbu.FromGoDecimal(liq),
			IsolatedMargin:   pbu.FromGoDecimal(margin),
		})
	}

	return positions, nil
}

func (e *OKXExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *OKXExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	baseURL := e.Config.BaseURL
	wsURL := privateOKXWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		var event struct {
			Arg struct {
				Channel string `json:"channel"`
			} `json:"arg"`
			Data []struct {
				InstID    string `json:"instId"`
				OrdID     string `json:"ordId"`
				ClOrdID   string `json:"clOrdID"`
				State     string `json:"state"`
				FillSz    string `json:"fillSz"`
				FillPx    string `json:"fillPx"`
				AccFillSz string `json:"accFillSz"`
				AvgPx     string `json:"avgPx"`
				UTime     string `json:"uTime"`
			} `json:"data"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal order message", "error", err)
			return
		}

		if event.Arg.Channel != "orders" {
			return
		}

		for _, data := range event.Data {
			var status pb.OrderStatus
			switch data.State {
			case "live":
				status = pb.OrderStatus_ORDER_STATUS_NEW
			case "partially_filled":
				status = pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
			case "filled":
				status = pb.OrderStatus_ORDER_STATUS_FILLED
			case "canceled":
				status = pb.OrderStatus_ORDER_STATUS_CANCELED
			default:
				status = pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
			}

			// OKX gives cumulative fill size in accFillSz
			execQty, _ := decimal.NewFromString(data.AccFillSz)
			avgPrice, _ := decimal.NewFromString(data.AvgPx)
			// Price? We might not get order price in update, usually fill price or avg price matters.
			// pb.OrderUpdate expects Price. We can use AvgPx if filled, or just send 0 if unknown.
			// Or we track it.
			price, _ := decimal.NewFromString(data.FillPx) // Last fill price?

			orderID, _ := strconv.ParseInt(data.OrdID, 10, 64)
			ts, _ := strconv.ParseInt(data.UTime, 10, 64)

			update := pb.OrderUpdate{
				OrderId:       orderID,
				ClientOrderId: data.ClOrdID,
				Symbol:        data.InstID,
				Status:        status,
				ExecutedQty:   pbu.FromGoDecimal(execQty),
				Price:         pbu.FromGoDecimal(price),
				AvgPrice:      pbu.FromGoDecimal(avgPrice),
				UpdateTime:    ts,
			}

			callback(&update)
		}
	}, e.Logger)

	client.SetOnConnected(func() {
		// Login
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		// Signature: base64(hmac(timestamp + "GET" + "/users/self/verify", secret))
		message := timestamp + "GET" + "/users/self/verify"
		mac := hmac.New(sha256.New, []byte(e.Config.SecretKey))
		mac.Write([]byte(message))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		loginMsg := map[string]interface{}{
			"op": "login",
			"args": []map[string]string{
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

		// Subscribe orders
		go func() {
			time.Sleep(100 * time.Millisecond)
			subMsg := map[string]interface{}{
				"op": "subscribe",
				"args": []map[string]string{
					{
						"channel":  "orders",
						"instType": "ANY",
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

func (e *OKXExchange) StopOrderStream() error {
	return nil
}

func (e *OKXExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	baseURL := e.Config.BaseURL
	wsURL := defaultOKXWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		var event struct {
			Arg struct {
				Channel string `json:"channel"`
				InstID  string `json:"instId"`
			} `json:"arg"`
			Data []struct {
				InstID string `json:"instId"`
				Last   string `json:"last"`
				TS     string `json:"ts"`
			} `json:"data"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal ticker message", "error", err)
			return
		}

		if event.Arg.Channel != "tickers" {
			return
		}

		for _, data := range event.Data {
			price, _ := decimal.NewFromString(data.Last)
			ts, _ := strconv.ParseInt(data.TS, 10, 64)

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
		for i, s := range symbols {
			args[i] = map[string]string{
				"channel": "tickers",
				"instId":  s,
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

func (e *OKXExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	return fmt.Errorf("not implemented")
}

func (e *OKXExchange) StopKlineStream() error {
	return nil
}

func (e *OKXExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *OKXExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *OKXExchange) FetchExchangeInfo(ctx context.Context, symbol string) error {
	baseURL := e.Config.BaseURL
	if baseURL == "" {
		baseURL = defaultOKXURL
	}

	// OKX fetches all instruments for a type usually
	// We assume SWAP for futures connector
	path := "/api/v5/public/instruments?instType=SWAP"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
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

	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID   string `json:"instId"`
			BaseCcy  string `json:"baseCcy"`  // e.g. BTC
			QuoteCcy string `json:"quoteCcy"` // e.g. USDT
			TickSz   string `json:"tickSz"`
			LotSz    string `json:"lotSz"`
			MinSz    string `json:"minSz"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}

	if response.Code != "0" {
		return e.parseError(body)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, inst := range response.Data {
		tickSz, _ := decimal.NewFromString(inst.TickSz)
		lotSz, _ := decimal.NewFromString(inst.LotSz)
		minSz, _ := decimal.NewFromString(inst.MinSz)

		// Calculate precision
		// 0.01 -> 2
		// 0.0001 -> 4
		pricePrec := -tickSz.Exponent()
		qtyPrec := -lotSz.Exponent()

		info := &pb.SymbolInfo{
			Symbol:            inst.InstID,
			BaseAsset:         inst.BaseCcy,  // For SWAP usually base currency
			QuoteAsset:        inst.QuoteCcy, // For SWAP usually quote currency
			PricePrecision:    pricePrec,
			QuantityPrecision: qtyPrec,
			TickSize:          pbu.FromGoDecimal(tickSz),
			StepSize:          pbu.FromGoDecimal(lotSz),
			MinQuantity:       pbu.FromGoDecimal(minSz),
		}

		// Some OKX swaps don't have BaseCcy/QuoteCcy populated directly?
		// "baseCcy" is "Settlement currency" in some contexts?
		// For BTC-USDT-SWAP: baseCcy=BTC, quoteCcy=USDT usually.

		e.symbolInfo[inst.InstID] = info
	}

	return nil
}

func (e *OKXExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
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

func (e *OKXExchange) GetSymbols(ctx context.Context) ([]string, error) {
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

func (e *OKXExchange) GetPriceDecimals() int {

	return 2
}

func (e *OKXExchange) GetQuantityDecimals() int {
	return 3
}

func (e *OKXExchange) GetBaseAsset() string {
	return "BTC"
}

func (e *OKXExchange) GetQuoteAsset() string {
	return "USDT"
}

func (e *OKXExchange) StartAccountStream(ctx context.Context, callback func(*pb.Account)) error {
	baseURL := e.Config.BaseURL
	wsURL := privateOKXWS
	if baseURL != "" {
		if strings.HasPrefix(baseURL, "http") {
			wsURL = strings.Replace(baseURL, "http", "ws", 1)
		} else if strings.HasPrefix(baseURL, "ws") {
			wsURL = baseURL
		}
	}

	client := websocket.NewClient(wsURL, func(message []byte) {
		var event struct {
			Arg struct {
				Channel string `json:"channel"`
			} `json:"arg"`
			Data []struct {
				TotalEq  string `json:"totalEq"`
				AdjEq    string `json:"adjEq"`
				MgnRatio string `json:"mgnRatio"`
				Details  []struct {
					Ccy     string `json:"ccy"`
					Eq      string `json:"eq"`
					AvailEq string `json:"availEq"`
				} `json:"details"`
			} `json:"data"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			e.Logger.Error("Failed to unmarshal account message", "error", err)
			return
		}

		if event.Arg.Channel != "account" {
			return
		}

		for _, data := range event.Data {
			var avail, total decimal.Decimal
			for _, detail := range data.Details {
				if detail.Ccy == "USDT" {
					avail, _ = decimal.NewFromString(detail.AvailEq)
					total, _ = decimal.NewFromString(detail.Eq)
					break
				}
			}

			if total.IsZero() {
				total, _ = decimal.NewFromString(data.TotalEq)
				avail = total
			}

			adjEq, _ := decimal.NewFromString(data.AdjEq)
			mgnRatio, _ := decimal.NewFromString(data.MgnRatio)

			health := decimal.Zero
			if mgnRatio.GreaterThan(decimal.NewFromFloat(1.001)) {
				health = decimal.NewFromInt(1).Sub(decimal.NewFromInt(1).Div(mgnRatio))
			}
			if health.IsNegative() {
				health = decimal.Zero
			}

			callback(&pb.Account{
				TotalWalletBalance: pbu.FromGoDecimal(total),
				AvailableBalance:   pbu.FromGoDecimal(avail),
				IsUnified:          true,
				HealthScore:        pbu.FromGoDecimal(health),
				MarginMode:         pb.MarginMode_MARGIN_MODE_UNIFIED,
				AdjustedEquity:     pbu.FromGoDecimal(adjEq),
			})
		}
	}, e.Logger)

	client.SetOnConnected(func() {
		// Login
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		message := timestamp + "GET" + "/users/self/verify"
		mac := hmac.New(sha256.New, []byte(e.Config.SecretKey))
		mac.Write([]byte(message))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		loginMsg := map[string]interface{}{
			"op": "login",
			"args": []map[string]string{
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

		// Subscribe
		go func() {
			time.Sleep(100 * time.Millisecond)
			subMsg := map[string]interface{}{
				"op": "subscribe",
				"args": []map[string]string{
					{"channel": "account"},
				},
			}
			if err := client.Send(subMsg); err != nil {
				e.Logger.Error("Failed to send account subscription message", "error", err)
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

// StartPositionStream implements position streaming via polling
func (e *OKXExchange) StartPositionStream(ctx context.Context, callback func(*pb.Position)) error {
	return e.StartPollingStream(ctx, func(ctx context.Context) (interface{}, error) {
		return e.GetPositions(ctx, "")
	}, func(data interface{}) {
		positions := data.([]*pb.Position)
		for _, position := range positions {
			callback(position)
		}
	}, 5*time.Second, "PositionStream")
}

func (e *OKXExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *OKXExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *OKXExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *OKXExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.Zero, fmt.Errorf("not implemented")
}

func (e *OKXExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	return nil, fmt.Errorf("not implemented")
}

func (e *OKXExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	return fmt.Errorf("not implemented")
}
