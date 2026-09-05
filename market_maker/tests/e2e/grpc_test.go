package e2e

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/exchange"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"net"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestE2E_gRPCLoopback(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")
	mockExch := mock.NewMockExchange("loopback_mock")

	// 1. Start gRPC Server
	server := exchange.NewExchangeServer(mockExch, logger)
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	pb.RegisterExchangeServiceServer(grpcServer, server)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			t.Logf("Server exited: %v", err)
		}
	}()
	defer grpcServer.Stop()

	// 2. Connect Remote Client
	remoteAddr := fmt.Sprintf("localhost:%d", port)

	// Wait for server to start
	var remoteExch core.IExchange
	assert.Eventually(t, func() bool {
		var err error
		remoteExch, err = exchange.NewRemoteExchange(remoteAddr, logger)
		return err == nil
	}, 2*time.Second, 50*time.Millisecond, "Failed to connect to gRPC server")

	// 3. Verify Operations through gRPC
	ctx := context.Background()

	// Identity
	if remoteExch.GetName() != "loopback_mock" {
		t.Errorf("Expected loopback_mock, got %s", remoteExch.GetName())
	}

	// Market Data
	price, err := remoteExch.GetLatestPrice(ctx, "BTCUSDT")
	if err != nil {
		t.Fatalf("GetLatestPrice failed: %v", err)
	}
	if !price.Equal(decimal.NewFromInt(45000)) {
		t.Errorf("Expected 45000, got %s", price)
	}

	// Order Operations
	req := &pb.PlaceOrderRequest{
		Symbol:   "BTCUSDT",
		Side:     pb.OrderSide_ORDER_SIDE_BUY,
		Type:     pb.OrderType_ORDER_TYPE_LIMIT,
		Price:    pbu.FromGoDecimal(decimal.NewFromInt(44000)),
		Quantity: pbu.FromGoDecimal(decimal.NewFromInt(1)),
	}
	order, err := remoteExch.PlaceOrder(ctx, req)
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}
	if order.OrderId == 0 {
		t.Error("OrderId should not be 0")
	}

	// Verify on mock directly
	openOrders, _ := mockExch.GetOpenOrders(ctx, "BTCUSDT", false)
	if len(openOrders) != 1 {
		t.Errorf("Expected 1 open order on mock, got %d", len(openOrders))
	}
}
