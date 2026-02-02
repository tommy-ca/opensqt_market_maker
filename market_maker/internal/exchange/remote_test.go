package exchange

import (
	"context"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"net"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"market_maker/pkg/telemetry"
)

func TestRemoteExchange_Integration(t *testing.T) {
	// Initialize telemetry to avoid panic
	_, err := telemetry.Setup("test")
	if err != nil {
		t.Fatalf("Failed to setup telemetry: %v", err)
	}

	// 1. Setup bufconn listener
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()

	mockExch := mock.NewMockExchange("mock-binance")
	logger, _ := logging.NewZapLogger("DEBUG")

	srv := NewExchangeServer(mockExch, logger)
	pb.RegisterExchangeServiceServer(s, srv)

	go func() {
		if err := s.Serve(lis); err != nil {
			return
		}
	}()
	defer s.Stop()

	// 2. Setup Client
	ctx := context.Background()
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := pb.NewExchangeServiceClient(conn)
	remote := &RemoteExchange{
		client: client,
		logger: logger,
		name:   "mock-binance",
	}

	// 3. Test GetName
	if remote.GetName() != "mock-binance" {
		t.Errorf("Expected mock-binance, got %s", remote.GetName())
	}

	// 4. Test PlaceOrder
	req := &pb.PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          pb.OrderSide_ORDER_SIDE_BUY,
		Type:          pb.OrderType_ORDER_TYPE_LIMIT,
		Quantity:      pbu.FromGoDecimal(decimal.NewFromInt(1)),
		Price:         pbu.FromGoDecimal(decimal.NewFromInt(45000)),
		ClientOrderId: "test-integration",
	}

	order, err := remote.PlaceOrder(ctx, req)
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	if order.OrderId == 0 {
		t.Error("Expected non-zero order ID")
	}
	if !pbu.ToGoDecimal(order.Price).Equal(pbu.ToGoDecimal(req.Price)) {
		t.Errorf("Expected price %v, got %v", req.Price, order.Price)
	}

	// 5. Test GetLatestPrice
	price, err := remote.GetLatestPrice(ctx, "BTCUSDT")
	if err != nil {
		t.Fatalf("GetLatestPrice failed: %v", err)
	}
	if price.IsZero() {
		t.Error("Expected non-zero price")
	}

	// 6. Test GetAccount
	acc, err := remote.GetAccount(ctx)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if pbu.ToGoDecimal(acc.TotalWalletBalance).IsZero() {
		t.Error("Expected non-zero balance")
	}

	// 7. Test Streams
	priceChan := make(chan *pb.PriceChange, 1)
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	err = remote.StartPriceStream(streamCtx, []string{"BTCUSDT"}, func(change *pb.PriceChange) {
		priceChan <- change
	})
	if err != nil {
		t.Fatalf("StartPriceStream failed: %v", err)
	}

	// Wait for stream establishment and mock price feed to tick
	time.Sleep(200 * time.Millisecond)

	select {
	case change := <-priceChan:
		if change.Symbol != "BTCUSDT" {
			t.Errorf("Expected BTCUSDT, got %s", change.Symbol)
		}
		if pbu.ToGoDecimal(change.Price).IsZero() {
			t.Error("Expected non-zero price update")
		}
	case <-time.After(2 * time.Second):
		t.Error("Timed out waiting for price update via stream")
	}
}
