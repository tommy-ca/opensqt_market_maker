package position

import (
	"fmt"
	"market_maker/internal/core"
	pb "market_maker/internal/pb"
	"sync"
	"time"

	"context"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"market_maker/pkg/pbu"
)

type PositionServiceServer struct {
	pb.UnimplementedPositionServiceServer
	manager      core.IPositionManager
	exchangeName string

	// For streaming subscriptions
	subscribers   map[string]chan *pb.PositionUpdate
	subscribersMu sync.RWMutex
}

func NewPositionServiceServer(manager core.IPositionManager, exchangeName string) *PositionServiceServer {
	s := &PositionServiceServer{
		manager:      manager,
		exchangeName: exchangeName,
		subscribers:  make(map[string]chan *pb.PositionUpdate),
	}

	// Listen to position manager updates
	manager.OnUpdate(s.broadcastUpdate)

	return s
}

func (s *PositionServiceServer) GetPositions(ctx context.Context, req *pb.PositionServiceGetPositionsRequest) (*pb.PositionServiceGetPositionsResponse, error) {
	snapshot := s.manager.GetSnapshot()
	symbol := snapshot.Symbol

	if len(req.Symbols) > 0 && !contains(req.Symbols, symbol) {
		return &pb.PositionServiceGetPositionsResponse{}, nil
	}

	totalQty := decimal.Zero
	totalCost := decimal.Zero
	realizedPnl := decimal.Zero

	for _, slot := range snapshot.Slots {
		if slot.PositionStatus == pb.PositionStatus_POSITION_STATUS_FILLED {
			qty := pbu.ToGoDecimal(slot.PositionQty)
			price := pbu.ToGoDecimal(slot.Price)

			totalQty = totalQty.Add(qty)
			totalCost = totalCost.Add(qty.Mul(price))
		}
	}

	avgEntryPrice := decimal.Zero
	if !totalQty.IsZero() {
		avgEntryPrice = totalCost.Div(totalQty)
	}

	currentPrice := pbu.ToGoDecimal(snapshot.AnchorPrice)
	unrealizedPnl := totalQty.Mul(currentPrice.Sub(avgEntryPrice))

	posData := &pb.PositionData{
		Symbol:        symbol,
		Exchange:      s.exchangeName,
		Quantity:      pbu.FromGoDecimal(totalQty),
		EntryPrice:    pbu.FromGoDecimal(avgEntryPrice),
		CurrentPrice:  pbu.FromGoDecimal(currentPrice),
		UnrealizedPnl: pbu.FromGoDecimal(unrealizedPnl),
		RealizedPnl:   pbu.FromGoDecimal(realizedPnl),
		OpenedAt:      0,
		UpdatedAt:     snapshot.LastUpdateTime,
	}

	return &pb.PositionServiceGetPositionsResponse{Positions: []*pb.PositionData{posData}}, nil
}

func (s *PositionServiceServer) SubscribePositions(req *pb.PositionServiceSubscribePositionsRequest, stream pb.PositionService_SubscribePositionsServer) error {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	updates := make(chan *pb.PositionUpdate, 100)

	s.subscribersMu.Lock()
	s.subscribers[id] = updates
	s.subscribersMu.Unlock()

	defer func() {
		s.subscribersMu.Lock()
		delete(s.subscribers, id)
		s.subscribersMu.Unlock()
		close(updates)
	}()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case update := <-updates:
			if len(req.Symbols) > 0 && !contains(req.Symbols, update.Position.Symbol) {
				continue
			}

			if err := stream.Send(update); err != nil {
				return err
			}
		}
	}
}

func (s *PositionServiceServer) broadcastUpdate(update *pb.PositionUpdate) {
	s.subscribersMu.RLock()
	defer s.subscribersMu.RUnlock()

	for _, ch := range s.subscribers {
		select {
		case ch <- update:
		default:
		}
	}
}

func (s *PositionServiceServer) GetOpenOrders(ctx context.Context, req *pb.PositionServiceGetOpenOrdersRequest) (*pb.PositionServiceGetOpenOrdersResponse, error) {
	snapshot := s.manager.GetSnapshot()
	symbol := snapshot.Symbol

	if len(req.Symbols) > 0 && !contains(req.Symbols, symbol) {
		return &pb.PositionServiceGetOpenOrdersResponse{}, nil
	}

	var orders []*pb.Order
	for _, slot := range snapshot.Slots {
		if slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_LOCKED || slot.SlotStatus == pb.SlotStatus_SLOT_STATUS_PENDING {
			if slot.OrderId > 0 {
				orders = append(orders, &pb.Order{
					OrderId:       slot.OrderId,
					Symbol:        symbol,
					Side:          slot.OrderSide,
					Status:        slot.OrderStatus,
					Price:         slot.OrderPrice,
					ExecutedQty:   slot.OrderFilledQty,
					ClientOrderId: slot.ClientOid,
				})
			}
		}
	}

	return &pb.PositionServiceGetOpenOrdersResponse{Orders: orders}, nil
}

func (s *PositionServiceServer) GetPosition(ctx context.Context, req *pb.PositionServiceGetPositionRequest) (*pb.PositionServiceGetPositionResponse, error) {
	resp, err := s.GetPositions(ctx, &pb.PositionServiceGetPositionsRequest{
		Symbols: []string{req.Symbol},
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Positions) == 0 {
		return nil, status.Errorf(codes.NotFound, "position not found for symbol %s", req.Symbol)
	}

	openOrdersResp, err := s.GetOpenOrders(ctx, &pb.PositionServiceGetOpenOrdersRequest{
		Symbols: []string{req.Symbol},
	})
	if err != nil {
		return nil, err
	}

	return &pb.PositionServiceGetPositionResponse{
		Position:   resp.Positions[0],
		OpenOrders: openOrdersResp.Orders,
	}, nil
}

func (s *PositionServiceServer) GetOrderHistory(ctx context.Context, req *pb.PositionServiceGetOrderHistoryRequest) (*pb.PositionServiceGetOrderHistoryResponse, error) {
	history := s.manager.GetOrderHistory()
	var filtered []*pb.Order
	for _, order := range history {
		if len(req.Symbols) > 0 && !contains(req.Symbols, order.Symbol) {
			continue
		}
		// In a real implementation we would check start_time and end_time
		filtered = append(filtered, order)
	}

	// Apply limit
	if req.Limit > 0 && int32(len(filtered)) > req.Limit {
		filtered = filtered[int32(len(filtered))-req.Limit:]
	}

	return &pb.PositionServiceGetOrderHistoryResponse{
		Orders: filtered,
	}, nil
}

func (s *PositionServiceServer) GetOrder(ctx context.Context, req *pb.PositionServiceGetOrderRequest) (*pb.PositionServiceGetOrderResponse, error) {
	history := s.manager.GetOrderHistory()
	var foundOrder *pb.Order
	for _, order := range history {
		if fmt.Sprintf("%d", order.OrderId) == req.OrderId || order.ClientOrderId == req.OrderId {
			foundOrder = order
			break
		}
	}

	if foundOrder == nil {
		return nil, status.Errorf(codes.NotFound, "order not found: %s", req.OrderId)
	}

	fills := s.manager.GetFills()
	var orderFills []*pb.Fill
	for _, fill := range fills {
		if fill.OrderId == fmt.Sprintf("%d", foundOrder.OrderId) {
			orderFills = append(orderFills, fill)
		}
	}

	return &pb.PositionServiceGetOrderResponse{
		Order: foundOrder,
		Fills: orderFills,
	}, nil
}

func (s *PositionServiceServer) GetFills(ctx context.Context, req *pb.PositionServiceGetFillsRequest) (*pb.PositionServiceGetFillsResponse, error) {
	fills := s.manager.GetFills()
	var filtered []*pb.Fill
	for _, fill := range fills {
		if len(req.Symbols) > 0 && !contains(req.Symbols, fill.Symbol) {
			continue
		}
		filtered = append(filtered, fill)
	}

	if req.Limit > 0 && int32(len(filtered)) > req.Limit {
		filtered = filtered[int32(len(filtered))-req.Limit:]
	}

	return &pb.PositionServiceGetFillsResponse{
		Fills: filtered,
	}, nil
}

func (s *PositionServiceServer) GetPositionHistory(ctx context.Context, req *pb.PositionServiceGetPositionHistoryRequest) (*pb.PositionServiceGetPositionHistoryResponse, error) {
	history := s.manager.GetPositionHistory()
	var filtered []*pb.PositionSnapshotData
	for _, snap := range history {
		if req.Symbol != "" && snap.Symbol != req.Symbol {
			continue
		}
		filtered = append(filtered, snap)
	}

	return &pb.PositionServiceGetPositionHistoryResponse{
		Snapshots: filtered,
	}, nil
}

func (s *PositionServiceServer) GetRealizedPnL(ctx context.Context, req *pb.PositionServiceGetRealizedPnLRequest) (*pb.PositionServiceGetRealizedPnLResponse, error) {
	pnl := s.manager.GetRealizedPnL()

	// Since we currently track total realized PnL in the manager, we return it here.
	// In a multi-symbol manager we would filter.
	return &pb.PositionServiceGetRealizedPnLResponse{
		TotalPnl: pbu.FromGoDecimal(pnl),
	}, nil
}

func (s *PositionServiceServer) GetUnrealizedPnL(ctx context.Context, req *pb.PositionServiceGetUnrealizedPnLRequest) (*pb.PositionServiceGetUnrealizedPnLResponse, error) {
	resp, err := s.GetPositions(ctx, &pb.PositionServiceGetPositionsRequest{
		Symbols: req.Symbols,
	})
	if err != nil {
		return nil, err
	}

	var totalPnL decimal.Decimal
	var pnlDetails []*pb.PositionPnL

	for _, pos := range resp.Positions {
		uPnL := pbu.ToGoDecimal(pos.UnrealizedPnl)
		totalPnL = totalPnL.Add(uPnL)

		pnlDetails = append(pnlDetails, &pb.PositionPnL{
			Symbol:        pos.Symbol,
			Quantity:      pos.Quantity,
			EntryPrice:    pos.EntryPrice,
			CurrentPrice:  pos.CurrentPrice,
			UnrealizedPnl: pos.UnrealizedPnl,
		})
	}

	return &pb.PositionServiceGetUnrealizedPnLResponse{
		TotalPnl:  pbu.FromGoDecimal(totalPnL),
		Positions: pnlDetails,
	}, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
