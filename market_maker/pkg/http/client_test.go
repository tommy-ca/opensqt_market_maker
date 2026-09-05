package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHttpClient_Retry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second, nil)
	_, err := client.Get(context.Background(), "/", nil)
	if err != nil {
		t.Fatalf("Request failed after retries: %v", err)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestHttpClient_CircuitBreaker(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, 5*time.Second, nil)

	// We need to trigger the breaker.
	// Policy is 5 failures out of 10.
	// We'll do 6 requests.
	for i := 0; i < 6; i++ {
		_, _ = client.Get(context.Background(), "/", nil)
	}

	// The 7th request should fail immediately due to open circuit breaker
	// without reaching the server.
	startAttempts := attempts
	_, err := client.Get(context.Background(), "/", nil)
	if err == nil {
		t.Error("Expected error due to open circuit breaker, got nil")
	}

	if attempts != startAttempts {
		t.Errorf("Server was reached even though circuit should be open. Attempts: %d", attempts)
	}
}
