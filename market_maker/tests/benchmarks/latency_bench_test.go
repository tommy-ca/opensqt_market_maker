package benchmarks

import (
	"context"
	"market_maker/internal/exchange"
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"testing"
)

// Benchmark 5.1.1: GetAccount Latency
func BenchmarkGetAccount_Latency(b *testing.B) {
	logger, _ := logging.NewZapLogger("WARN")
	remote, err := exchange.NewRemoteExchange("localhost:50051", logger)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := remote.GetAccount(ctx)
		if err != nil {
			b.Errorf("Request failed: %v", err)
		}
	}
}

// Benchmark 5.2.1: Order Placement Throughput
func BenchmarkOrderPlacement_Throughput(b *testing.B) {
	logger, _ := logging.NewZapLogger("WARN")
	remote, err := exchange.NewRemoteExchange("localhost:50051", logger)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}

	ctx := context.Background()
	// Create a dummy order request
	req := &pb.PlaceOrderRequest{
		Symbol: "BTCUSDT",
		Side:   pb.OrderSide_ORDER_SIDE_BUY,
		Type:   pb.OrderType_ORDER_TYPE_LIMIT,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = remote.PlaceOrder(ctx, req)
		}
	})
}
