package monitor

import (
	"context"
	"market_maker/internal/core"
	"market_maker/internal/mock"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPriceMonitor_StartStop(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	pm := NewPriceMonitor(exchange, "BTCUSDT", logger)

	if pm.isRunningAtomic() {
		t.Error("Should not be running initially")
	}

	ctx := context.Background()
	err := pm.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if !pm.isRunningAtomic() {
		t.Error("Should be running")
	}

	err = pm.Stop()
	if err != nil {
		t.Fatalf("Failed to stop: %v", err)
	}
	if pm.isRunningAtomic() {
		t.Error("Should not be running")
	}
}

func TestPriceMonitor_PriceStorage(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	pm := NewPriceMonitor(exchange, "BTCUSDT", logger)

	_, err := pm.GetLatestPrice()
	if err == nil {
		t.Error("Expected error when no price available")
	}

	price := decimal.NewFromFloat(45000.50)
	pm.handlePriceUpdate(&pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(price),
		Timestamp: timestamppb.Now(),
	})

	stored, err := pm.GetLatestPrice()
	if err != nil {
		t.Fatalf("Failed to get price: %v", err)
	}
	if !stored.Equal(price) {
		t.Errorf("Expected %v, got %v", price, stored)
	}
}

func TestPriceMonitor_Subscription(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	pm := NewPriceMonitor(exchange, "BTCUSDT", logger)

	subscriber := pm.SubscribePriceChanges()
	ctx := context.Background()
	_ = pm.Start(ctx)
	defer func() {
		_ = pm.Stop()
	}()

	price := decimal.NewFromFloat(46000.25)
	pm.handlePriceUpdate(&pb.PriceChange{
		Symbol:    "BTCUSDT",
		Price:     pbu.FromGoDecimal(price),
		Timestamp: timestamppb.Now(),
	})

	select {
	case received := <-subscriber:
		if !pbu.ToGoDecimal(received.Price).Equal(price) {
			t.Errorf("Expected %v, got %v", price, received.Price)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for update")
	}
}

func TestPriceMonitor_ConcurrentAccess(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	pm := NewPriceMonitor(exchange, "BTCUSDT", logger)

	pm.handlePriceUpdate(&pb.PriceChange{
		Symbol: "BTCUSDT", Price: pbu.FromGoDecimal(decimal.NewFromFloat(45000.0)), Timestamp: timestamppb.Now(),
	})

	done := make(chan bool, 2)
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 50; i++ {
			pm.handlePriceUpdate(&pb.PriceChange{
				Symbol: "BTCUSDT", Price: pbu.FromGoDecimal(decimal.NewFromFloat(45000.0 + float64(i))), Timestamp: timestamppb.Now(),
			})
			time.Sleep(time.Millisecond)
		}
	}()

	go func() {
		defer func() { done <- true }()
		for i := 0; i < 50; i++ {
			p, err := pm.GetLatestPrice()
			if err == nil && p.LessThan(decimal.NewFromInt(45000)) {
				t.Errorf("Unexpected low price: %v", p)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	<-done
	<-done
}

func TestPriceMonitor_StartTwice(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	pm := NewPriceMonitor(exchange, "BTCUSDT", logger)
	ctx := context.Background()
	_ = pm.Start(ctx)
	defer func() {
		_ = pm.Stop()
	}()
	if pm.Start(ctx) == nil {
		t.Error("Should have failed")
	}
}

func TestPriceMonitor_StopNotRunning(t *testing.T) {
	exchange := mock.NewMockExchange("test_exchange")
	logger := &mockLogger{}
	pm := NewPriceMonitor(exchange, "BTCUSDT", logger)
	if err := pm.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, f ...interface{})               {}
func (m *mockLogger) Info(msg string, f ...interface{})                {}
func (m *mockLogger) Warn(msg string, f ...interface{})                {}
func (m *mockLogger) Error(msg string, f ...interface{})               {}
func (m *mockLogger) Fatal(msg string, f ...interface{})               {}
func (m *mockLogger) WithField(k string, v interface{}) core.ILogger   { return m }
func (m *mockLogger) WithFields(f map[string]interface{}) core.ILogger { return m }
