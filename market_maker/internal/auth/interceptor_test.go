package auth

import (
	"context"
	"market_maker/internal/logging"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAPIKeyValidator_ValidateAPIKey(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	apiKeys := []string{"valid-key-1", "valid-key-2"}
	validator := NewAPIKeyValidator(apiKeys, 100, logger)

	tests := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{
			name:   "valid key 1",
			apiKey: "valid-key-1",
			want:   true,
		},
		{
			name:   "valid key 2",
			apiKey: "valid-key-2",
			want:   true,
		},
		{
			name:   "invalid key",
			apiKey: "invalid-key",
			want:   false,
		},
		{
			name:   "empty key",
			apiKey: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validator.ValidateAPIKey(tt.apiKey); got != tt.want {
				t.Errorf("ValidateAPIKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIKeyValidator_AddRemoveAPIKey(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	validator := NewAPIKeyValidator([]string{"initial-key"}, 100, logger)

	// Test initial key
	if !validator.ValidateAPIKey("initial-key") {
		t.Error("Initial key should be valid")
	}

	// Add new key
	validator.AddAPIKey("new-key")
	if !validator.ValidateAPIKey("new-key") {
		t.Error("New key should be valid after adding")
	}

	// Remove key
	validator.RemoveAPIKey("new-key")
	if validator.ValidateAPIKey("new-key") {
		t.Error("Key should be invalid after removal")
	}

	// Initial key should still be valid
	if !validator.ValidateAPIKey("initial-key") {
		t.Error("Initial key should still be valid")
	}
}

func TestAPIKeyValidator_RateLimit(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	rateLimit := 5 // 5 requests per second
	validator := NewAPIKeyValidator([]string{"test-key"}, rateLimit, logger)

	// First N requests should succeed
	for i := 0; i < rateLimit; i++ {
		if !validator.CheckRateLimit("test-key") {
			t.Errorf("Request %d should be allowed (rate limit: %d)", i+1, rateLimit)
		}
	}

	// Next request should fail (rate limit exceeded)
	if validator.CheckRateLimit("test-key") {
		t.Error("Request should be rate limited")
	}

	// Wait for token refill (1 second + buffer)
	time.Sleep(1100 * time.Millisecond)

	// Should be able to make requests again
	if !validator.CheckRateLimit("test-key") {
		t.Error("Request should succeed after rate limit refill")
	}
}

func TestUnaryServerInterceptor_MissingMetadata(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	validator := NewAPIKeyValidator([]string{"valid-key"}, 100, logger)
	interceptor := validator.UnaryServerInterceptor()

	// Create context without metadata
	ctx := context.Background()

	// Call interceptor
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "success", nil
		})

	// Should fail with Unauthenticated error
	if err == nil {
		t.Fatal("Expected error for missing metadata, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got %v", st.Code())
	}
}

func TestUnaryServerInterceptor_MissingAPIKey(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	validator := NewAPIKeyValidator([]string{"valid-key"}, 100, logger)
	interceptor := validator.UnaryServerInterceptor()

	// Create context with metadata but no API key
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("other-key", "value"))

	// Call interceptor
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "success", nil
		})

	// Should fail with Unauthenticated error
	if err == nil {
		t.Fatal("Expected error for missing API key, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got %v", st.Code())
	}
}

func TestUnaryServerInterceptor_InvalidAPIKey(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	validator := NewAPIKeyValidator([]string{"valid-key"}, 100, logger)
	interceptor := validator.UnaryServerInterceptor()

	// Create context with invalid API key
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKeyAPIKey, "invalid-key"))

	// Call interceptor
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "success", nil
		})

	// Should fail with Unauthenticated error
	if err == nil {
		t.Fatal("Expected error for invalid API key, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got %v", st.Code())
	}
}

func TestUnaryServerInterceptor_ValidAPIKey(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	validator := NewAPIKeyValidator([]string{"valid-key"}, 100, logger)
	interceptor := validator.UnaryServerInterceptor()

	// Create context with valid API key
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKeyAPIKey, "valid-key"))

	// Call interceptor
	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "success", nil
		})

	// Should succeed
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if resp != "success" {
		t.Errorf("Expected response 'success', got %v", resp)
	}
}

func TestUnaryServerInterceptor_RateLimitExceeded(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	rateLimit := 2
	validator := NewAPIKeyValidator([]string{"valid-key"}, rateLimit, logger)
	interceptor := validator.UnaryServerInterceptor()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKeyAPIKey, "valid-key"))

	// First N requests should succeed
	for i := 0; i < rateLimit; i++ {
		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
			func(ctx context.Context, req interface{}) (interface{}, error) {
				return "success", nil
			})
		if err != nil {
			t.Fatalf("Request %d should succeed, got error: %v", i+1, err)
		}
	}

	// Next request should fail with ResourceExhausted
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "success", nil
		})

	if err == nil {
		t.Fatal("Expected rate limit error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.ResourceExhausted {
		t.Errorf("Expected ResourceExhausted code, got %v", st.Code())
	}
}
