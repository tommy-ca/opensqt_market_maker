package auth

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	// MetadataKeyAPIKey is the metadata key for API key authentication
	MetadataKeyAPIKey = "x-api-key"

	// DefaultRateLimitPerKey is the default number of requests per second allowed per API key
	DefaultRateLimitPerKey = 100
)

// APIKeyValidator validates API keys and manages rate limiting
type APIKeyValidator struct {
	validKeys     map[string]bool
	rateLimiters  map[string]*rateLimiter
	rateLimit     int
	logger        core.ILogger
	mu            sync.RWMutex
	failureLogger core.ILogger // Separate logger for authentication failures
}

// rateLimiter implements a simple token bucket rate limiter
type rateLimiter struct {
	tokens     int
	maxTokens  int
	lastRefill time.Time
	mu         sync.Mutex
}

// NewAPIKeyValidator creates a new API key validator with rate limiting
func NewAPIKeyValidator(apiKeys []string, rateLimit int, logger core.ILogger) *APIKeyValidator {
	validKeys := make(map[string]bool)
	for _, key := range apiKeys {
		validKeys[key] = true
	}

	if rateLimit <= 0 {
		rateLimit = DefaultRateLimitPerKey
	}

	return &APIKeyValidator{
		validKeys:     validKeys,
		rateLimiters:  make(map[string]*rateLimiter),
		rateLimit:     rateLimit,
		logger:        logger.WithField("component", "auth"),
		failureLogger: logger.WithField("component", "auth_failure"),
	}
}

// AddAPIKey adds a new API key to the validator (for key rotation)
func (v *APIKeyValidator) AddAPIKey(apiKey string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.validKeys[apiKey] = true
	v.logger.Info("API key added")
}

// RemoveAPIKey removes an API key from the validator (for key rotation)
func (v *APIKeyValidator) RemoveAPIKey(apiKey string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.validKeys, apiKey)
	delete(v.rateLimiters, apiKey)
	v.logger.Info("API key removed")
}

// ValidateAPIKey checks if the API key is valid
func (v *APIKeyValidator) ValidateAPIKey(apiKey string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.validKeys[apiKey]
}

// CheckRateLimit checks if the request is within rate limit for the API key
func (v *APIKeyValidator) CheckRateLimit(apiKey string) bool {
	v.mu.Lock()
	limiter, exists := v.rateLimiters[apiKey]
	if !exists {
		limiter = &rateLimiter{
			tokens:     v.rateLimit,
			maxTokens:  v.rateLimit,
			lastRefill: time.Now(),
		}
		v.rateLimiters[apiKey] = limiter
	}
	v.mu.Unlock()

	return limiter.allowRequest()
}

// allowRequest checks if a request is allowed under the rate limit
func (r *rateLimiter) allowRequest() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastRefill)

	// Refill tokens based on elapsed time (1 token per 1/maxTokens second)
	tokensToAdd := int(elapsed.Seconds() * float64(r.maxTokens))
	if tokensToAdd > 0 {
		r.tokens = min(r.maxTokens, r.tokens+tokensToAdd)
		r.lastRefill = now
	}

	if r.tokens > 0 {
		r.tokens--
		return true
	}

	return false
}

type requestIDKey struct{}

// withRequestID adds a request ID to the context
func withRequestID(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestIDKey{}, uuid.New().String())
}

// getRequestID extracts the request ID from the context
func getRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return "unknown"
}

// getClientIP extracts the client IP address from the context
func getClientIP(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok {
		return p.Addr.String()
	}
	return "unknown"
}

// UnaryServerInterceptor returns a gRPC unary interceptor for API key authentication
func (v *APIKeyValidator) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Add request ID to context
		ctx = withRequestID(ctx)
		requestID := getRequestID(ctx)
		clientIP := getClientIP(ctx)

		// Extract metadata from context
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			v.failureLogger.Warn("Authentication failed: missing metadata",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		// Get API key from metadata
		keys := md.Get(MetadataKeyAPIKey)
		if len(keys) == 0 {
			v.failureLogger.Warn("Authentication failed: missing API key",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return nil, status.Error(codes.Unauthenticated, "missing API key")
		}

		apiKey := keys[0]

		// Validate API key
		if !v.ValidateAPIKey(apiKey) {
			v.failureLogger.Warn("Authentication failed: invalid API key",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return nil, status.Error(codes.Unauthenticated, "invalid API key")
		}

		// Check rate limit
		if !v.CheckRateLimit(apiKey) {
			v.failureLogger.Warn("Rate limit exceeded",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded for API key")
		}

		// Call the handler
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a gRPC stream interceptor for API key authentication
func (v *APIKeyValidator) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Add request ID to context
		ctx := withRequestID(ss.Context())
		requestID := getRequestID(ctx)
		clientIP := getClientIP(ctx)

		// Extract metadata from context
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			v.failureLogger.Warn("Authentication failed: missing metadata",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		// Get API key from metadata
		keys := md.Get(MetadataKeyAPIKey)
		if len(keys) == 0 {
			v.failureLogger.Warn("Authentication failed: missing API key",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return status.Error(codes.Unauthenticated, "missing API key")
		}

		apiKey := keys[0]

		// Validate API key
		if !v.ValidateAPIKey(apiKey) {
			v.failureLogger.Warn("Authentication failed: invalid API key",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return status.Error(codes.Unauthenticated, "invalid API key")
		}

		// Check rate limit (for stream initiation)
		if !v.CheckRateLimit(apiKey) {
			v.failureLogger.Warn("Rate limit exceeded",
				"method", info.FullMethod,
				"request_id", requestID,
				"client_ip", clientIP)
			return status.Error(codes.ResourceExhausted, "rate limit exceeded for API key")
		}

		// Create a wrapper for ServerStream to use the new context
		wrappedStream := &wrappedServerStream{
			ServerStream: ss,
			ctx:          ctx,
		}

		// Call the handler
		return handler(srv, wrappedStream)
	}
}

// wrappedServerStream wraps grpc.ServerStream to allow context replacement
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the wrapped context
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LoadAPIKeysFromEnv loads API keys from environment variables
// Expected format: GRPC_API_KEYS="key1,key2,key3"
func LoadAPIKeysFromEnv() ([]string, error) {
	// This will be implemented in the config package
	// Placeholder for now
	return nil, fmt.Errorf("not implemented: use config package to load API keys")
}
