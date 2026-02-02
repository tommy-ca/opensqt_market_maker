package main

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// TestHealthCheck_ServiceNotRegistered verifies that health check should fail
// when the health service is not yet registered (this test validates RED phase).
// After implementing health service registration, this test will pass.
func TestHealthCheck_ServiceNotRegistered(t *testing.T) {
	t.Skip("Integration test - requires running exchange_connector instance")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient("localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Skipf("Skipping: exchange_connector not running (%v)", err)
	}
	defer conn.Close()

	healthClient := grpc_health_v1.NewHealthClient(conn)

	// Test overall health
	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("Expected SERVING, got %v", resp.Status)
	}

	// Test specific service health
	resp, err = healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "opensqt.market_maker.v1.ExchangeService",
	})
	if err != nil {
		t.Fatalf("Service health check failed: %v", err)
	}

	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("Expected SERVING for ExchangeService, got %v", resp.Status)
	}
}

// TestHealthCheck_Watch verifies that health watch stream works correctly
func TestHealthCheck_Watch(t *testing.T) {
	t.Skip("Integration test - requires running exchange_connector instance")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient("localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Skipf("Skipping: exchange_connector not running (%v)", err)
	}
	defer conn.Close()

	healthClient := grpc_health_v1.NewHealthClient(conn)

	// Start watching health status
	stream, err := healthClient.Watch(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Failed to start health watch: %v", err)
	}

	// Should immediately receive SERVING status
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive health status: %v", err)
	}

	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("Expected initial status SERVING, got %v", resp.Status)
	}
}
