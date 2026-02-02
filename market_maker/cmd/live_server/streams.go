package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"market_maker/internal/infrastructure/grpc/client"
	"market_maker/internal/pb"
	"market_maker/pkg/exchange"
	"market_maker/pkg/liveserver"
)

// StreamHandlers manages all exchange data streams
type StreamHandlers struct {
	exch     exchange.Exchange
	mmClient *client.MarketMakerClient
	hub      *liveserver.Hub
	config   *Config
	logger   liveserver.Logger
}

// NewStreamHandlers creates a new stream handlers manager
func NewStreamHandlers(exch exchange.Exchange, mmClient *client.MarketMakerClient, hub *liveserver.Hub, config *Config, logger liveserver.Logger) *StreamHandlers {
	return &StreamHandlers{
		exch:     exch,
		mmClient: mmClient,
		hub:      hub,
		config:   config,
		logger:   logger,
	}
}

// StartAll starts all applicable stream handlers
func (s *StreamHandlers) StartAll(ctx context.Context) error {
	// Start k-line stream (always)
	go s.streamKlines(ctx)

	// Start order stream (always)
	go s.streamOrders(ctx)

	// Start account stream (always)
	go s.streamAccount(ctx)

	// Start position stream if futures trading
	if s.config.IsFutures() {
		go s.streamPositions(ctx)
	}

	// Send historical data to clients on connect
	go s.sendHistoricalData(ctx)

	return nil
}

// streamKlines streams k-line (candlestick) data from exchange to WebSocket clients
func (s *StreamHandlers) streamKlines(ctx context.Context) {
	symbol := s.config.Trading.Symbol
	interval := s.config.Trading.Interval

	if s.logger != nil {
		s.logger.Info("Starting k-line stream", "symbol", symbol, "interval", interval)
	}

	err := s.exch.StartKlineStream(ctx, []string{symbol}, interval, func(candle *pb.Candle) {
		// Convert protobuf Candle to WebSocket message
		msg := liveserver.NewKlineMessage(map[string]interface{}{
			"time":   candle.Timestamp,
			"open":   candle.Open.String(),
			"high":   candle.High.String(),
			"low":    candle.Low.String(),
			"close":  candle.Close.String(),
			"volume": candle.Volume.String(),
		})

		// Broadcast to all connected clients
		s.hub.Broadcast(msg)
	})

	if err != nil && s.logger != nil {
		s.logger.Warn("K-line stream error", "error", err)
	}
}

// streamOrders streams order updates from market_maker to WebSocket clients
func (s *StreamHandlers) streamOrders(ctx context.Context) {
	if s.logger != nil {
		s.logger.Info("Starting order stream from market_maker")
	}

	// Pivot: Subscribe to market_maker gRPC instead of direct exchange
	s.mmClient.SubscribePositions(ctx, []string{s.config.Trading.Symbol}, func(update *pb.PositionUpdate) {
		// Extract trigger order if available
		if update.TriggerOrder == nil {
			return
		}

		order := update.TriggerOrder

		// Broadcast order update
		orderMsg := liveserver.NewOrdersMessage(map[string]interface{}{
			"id":     order.OrderId,
			"price":  order.Price.String(),
			"side":   order.Side.String(),
			"status": order.Status.String(),
			"type":   order.Type.String(),
			"symbol": order.Symbol,
		})
		s.hub.Broadcast(orderMsg)

		// If order filled, also send trade event
		if order.Status == pb.OrderStatus_ORDER_STATUS_FILLED || order.Status == pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED {
			tradeMsg := liveserver.NewTradeEventMessage(map[string]interface{}{
				"side":   strings.ToLower(order.Side.String()),
				"price":  order.Price.String(),
				"amount": order.Quantity.String(),
				"symbol": order.Symbol,
				"time":   time.Now().Unix(),
			})
			s.hub.Broadcast(tradeMsg)
		}
	})
}

// streamAccount streams account balance updates from exchange to WebSocket clients
func (s *StreamHandlers) streamAccount(ctx context.Context) {
	if s.logger != nil {
		s.logger.Info("Starting account stream")
	}

	err := s.exch.StartAccountStream(ctx, func(account *pb.Account) {
		// Use account-level balances
		balanceData := map[string]interface{}{
			"asset":         "USDT",
			"free":          account.AvailableBalance.String(),
			"balance":       account.TotalWalletBalance.String(),
			"marginBalance": account.TotalMarginBalance.String(),
			"symbol":        s.config.Trading.Symbol,
		}

		msg := liveserver.NewAccountMessage(balanceData)
		s.hub.Broadcast(msg)

		if s.logger != nil {
			s.logger.Info("Account update broadcasted", "balance", balanceData["balance"])
		}
	})

	if err != nil && s.logger != nil {
		s.logger.Warn("Account stream error", "error", err)
	}
}

// streamPositions streams position updates from market_maker to WebSocket clients
func (s *StreamHandlers) streamPositions(ctx context.Context) {
	if s.logger != nil {
		s.logger.Info("Starting position stream from market_maker")
	}

	// Pivot: Subscribe to market_maker gRPC instead of direct exchange
	s.mmClient.SubscribePositions(ctx, []string{s.config.Trading.Symbol}, func(update *pb.PositionUpdate) {
		pos := update.Position
		posData := map[string]interface{}{
			"symbol":        pos.Symbol,
			"amount":        pos.Quantity.Value,
			"entryPrice":    pos.EntryPrice.Value,
			"unrealizedPnL": pos.UnrealizedPnl.Value,
			"realizedPnL":   pos.RealizedPnl.Value,
			"currentPrice":  pos.CurrentPrice.Value,
			"updateType":    update.UpdateType,
		}

		msg := liveserver.NewPositionMessage(posData)
		s.hub.Broadcast(msg)

		// Rationale: We also broadcast Slot metadata if available in the update.
		// Note: pb.PositionUpdate doesn't contain slots currently.
		// We might need to fetch them or add them to the update proto.
	})

	// Add Risk Status subscription
	s.mmClient.SubscribeRiskAlerts(ctx, func(alert *pb.RiskAlert) {
		msg := liveserver.NewRiskStatusMessage(map[string]interface{}{
			"symbol":    alert.Symbol,
			"type":      alert.AlertType,
			"severity":  alert.Severity,
			"message":   alert.Message,
			"timestamp": alert.Timestamp,
		})
		s.hub.Broadcast(msg)
	})
}

// sendHistoricalData fetches and broadcasts historical k-line data
func (s *StreamHandlers) sendHistoricalData(ctx context.Context) {
	// Wait a bit for exchange connection to be ready
	time.Sleep(2 * time.Second)

	symbol := s.config.Trading.Symbol
	interval := s.config.Trading.Interval
	limit := s.config.Trading.HistoricalLimit

	if s.logger != nil {
		s.logger.Info("Fetching historical k-lines", "symbol", symbol, "limit", limit)
	}

	candles, err := s.exch.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to fetch historical k-lines", "error", err)
		}
		return
	}

	// Convert to history message format
	historyData := make([]map[string]interface{}, 0, len(candles))
	for _, candle := range candles {
		historyData = append(historyData, map[string]interface{}{
			"time":   candle.Timestamp,
			"open":   candle.Open.String(),
			"high":   candle.High.String(),
			"low":    candle.Low.String(),
			"close":  candle.Close.String(),
			"volume": candle.Volume.String(),
		})
	}

	// Broadcast history message
	historyMsg := liveserver.NewHistoryMessage(historyData)
	s.hub.Broadcast(historyMsg)

	if s.logger != nil {
		s.logger.Info("Sent historical k-lines", "count", len(candles))
	}
}

// GetHistoricalData fetches and returns historical k-line data
func (s *StreamHandlers) GetHistoricalData(ctx context.Context) ([]map[string]interface{}, error) {
	symbol := s.config.Trading.Symbol
	interval := s.config.Trading.Interval
	limit := s.config.Trading.HistoricalLimit

	candles, err := s.exch.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch historical k-lines: %w", err)
	}

	historyData := make([]map[string]interface{}, 0, len(candles))
	for _, candle := range candles {
		historyData = append(historyData, map[string]interface{}{
			"time":   candle.Timestamp,
			"open":   candle.Open.String(),
			"high":   candle.High.String(),
			"low":    candle.Low.String(),
			"close":  candle.Close.String(),
			"volume": candle.Volume.String(),
		})
	}

	return historyData, nil
}
