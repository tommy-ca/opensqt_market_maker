package exchange

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"market_maker/internal/config"
	"market_maker/internal/mock"
	pb "market_maker/internal/pb"
	"market_maker/pkg/logging"
)

func TestUnifiedConnector_MultiExchange(t *testing.T) {
	ctx := context.Background()
	logger, _ := logging.NewZapLogger("DEBUG")

	// 1. Setup multi-exchange mock environment
	exchanges := []string{"binance", "okx", "bybit"}

	for _, exName := range exchanges {
		t.Run("Exchange_"+exName, func(t *testing.T) {
			lis := bufconn.Listen(1024 * 1024)
			s := grpc.NewServer()

			// Factory would normally be used here, but we mock the internal adapter
			mockExch := mock.NewMockExchange(exName)
			srv := NewExchangeServer(mockExch, logger)
			pb.RegisterExchangeServiceServer(s, srv)

			go func() { _ = s.Serve(lis) }()
			defer s.Stop()

			// Connect client
			conn, err := grpc.NewClient("passthrough://bufnet",
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
					return lis.Dial()
				}),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				t.Fatalf("Failed to dial: %v", err)
			}
			defer conn.Close()

			client := pb.NewExchangeServiceClient(conn)

			// Verify name
			resp, err := client.GetName(ctx, &pb.GetNameRequest{})
			if err != nil {
				t.Fatalf("GetName failed: %v", err)
			}
			if resp.Name != exName {
				t.Errorf("Expected %s, got %s", exName, resp.Name)
			}

			// Verify type
			typeResp, err := client.GetType(ctx, &pb.GetTypeRequest{})
			if err != nil {
				t.Fatalf("GetType failed: %v", err)
			}
			if typeResp.Type == pb.ExchangeType_EXCHANGE_TYPE_UNSPECIFIED {
				t.Error("Expected non-unspecified exchange type")
			}
		})
	}
}

func TestExchangeFactory(t *testing.T) {
	logger, _ := logging.NewZapLogger("INFO")
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {APIKey: "key", SecretKey: "secret"},
			"okx":     {APIKey: "key", SecretKey: "secret", Passphrase: "pass"},
		},
	}

	// Test valid creation
	exch, err := NewExchange("binance", cfg, logger, nil)
	if err != nil {
		t.Errorf("Failed to create binance exchange: %v", err)
	}
	if exch.GetName() != "binance" {
		t.Errorf("Expected binance, got %s", exch.GetName())
	}

	// Test invalid creation
	_, err = NewExchange("nonexistent", cfg, logger, nil)
	if err == nil {
		t.Error("Expected error for nonexistent exchange")
	}
}
