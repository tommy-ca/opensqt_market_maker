package monitor

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

// FundingMonitor tracks funding rates across multiple exchanges
type FundingMonitor struct {
	exchanges map[string]core.IExchange
	logger    core.ILogger
	symbols   []string

	rates      map[string]map[string]*pb.FundingRate // exchange -> symbol -> rate
	lastUpdate map[string]map[string]time.Time       // exchange -> symbol -> last update time
	mu         sync.RWMutex

	subscribers []subscriber
	subMu       sync.RWMutex

	stopChan chan struct{}
}

type subscriber struct {
	ch       chan *pb.FundingUpdate
	exchange string
	symbol   string
}

// NewFundingMonitor creates a new FundingMonitor. If symbols is empty, default to BTCUSDT.
func NewFundingMonitor(exchanges map[string]core.IExchange, logger core.ILogger, symbols ...string) *FundingMonitor {
	if len(symbols) == 0 {
		symbols = []string{"BTCUSDT"}
	}
	return &FundingMonitor{
		exchanges:  exchanges,
		logger:     logger,
		symbols:    symbols,
		rates:      make(map[string]map[string]*pb.FundingRate),
		lastUpdate: make(map[string]map[string]time.Time),
		stopChan:   make(chan struct{}),
	}
}

// Start starts the monitor
func (m *FundingMonitor) Start(ctx context.Context) error {
	m.logger.Info("Starting FundingMonitor")

	g, ctx := errgroup.WithContext(ctx)

	for name, ex := range m.exchanges {
		name := name // Capture loop variable
		ex := ex     // Capture loop variable

		m.mu.Lock()
		if _, ok := m.rates[name]; !ok {
			m.rates[name] = make(map[string]*pb.FundingRate)
		}
		if _, ok := m.lastUpdate[name]; !ok {
			m.lastUpdate[name] = make(map[string]time.Time)
		}
		m.mu.Unlock()

		for _, symbol := range m.symbols {
			symbol := symbol // Capture loop variable
			g.Go(func() error {
				rate, err := ex.GetFundingRate(ctx, symbol)
				if err != nil {
					m.logger.Error("Failed to fetch initial funding rate", "exchange", name, "symbol", symbol, "error", err)
					return nil // Continue despite error
				}

				m.updateRate(name, symbol, rate)

				// Start stream per symbol
				err = ex.StartFundingRateStream(ctx, symbol, func(update *pb.FundingUpdate) {
					m.handleUpdate(update)
				})
				if err != nil {
					m.logger.Error("Failed to start funding stream", "exchange", name, "symbol", symbol, "error", err)
				}
				return nil
			})
		}
	}

	return g.Wait()
}

func (m *FundingMonitor) Stop() error {
	close(m.stopChan)
	return nil
}

func (m *FundingMonitor) GetRate(exchange, symbol string) (decimal.Decimal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exRates, ok := m.rates[exchange]
	if !ok {
		return decimal.Zero, fmt.Errorf("exchange not tracked: %s", exchange)
	}

	rate, ok := exRates[symbol]
	if !ok {
		return decimal.Zero, fmt.Errorf("symbol not tracked on %s: %s", exchange, symbol)
	}

	return pbu.ToGoDecimal(rate.Rate), nil
}

// IsStale returns true if the last update for (exchange,symbol) is older than ttl.
func (m *FundingMonitor) IsStale(exchange, symbol string, ttl time.Duration) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exTimes, ok := m.lastUpdate[exchange]
	if !ok {
		return true
	}

	last, ok := exTimes[symbol]
	if !ok {
		return true
	}

	return time.Since(last) > ttl
}

func (m *FundingMonitor) GetNextFundingTime(exchange, symbol string) (time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exRates, ok := m.rates[exchange]
	if !ok {
		return time.Time{}, fmt.Errorf("exchange not tracked: %s", exchange)
	}

	rate, ok := exRates[symbol]
	if !ok {
		return time.Time{}, fmt.Errorf("symbol not tracked on %s: %s", exchange, symbol)
	}

	return time.UnixMilli(rate.NextFundingTime), nil
}

func (m *FundingMonitor) Subscribe(exchange, symbol string) <-chan *pb.FundingUpdate {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	ch := make(chan *pb.FundingUpdate, 100)
	m.subscribers = append(m.subscribers, subscriber{ch: ch, exchange: exchange, symbol: symbol})
	return ch
}

func (m *FundingMonitor) updateRate(exchange, symbol string, rate *pb.FundingRate) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rates[exchange]; !ok {
		m.rates[exchange] = make(map[string]*pb.FundingRate)
	}
	if _, ok := m.lastUpdate[exchange]; !ok {
		m.lastUpdate[exchange] = make(map[string]time.Time)
	}
	m.rates[exchange][symbol] = rate
	m.lastUpdate[exchange][symbol] = time.Now()
}

func (m *FundingMonitor) handleUpdate(update *pb.FundingUpdate) {
	// Convert update to full Rate object or update in place
	rate := &pb.FundingRate{
		Exchange:        update.Exchange,
		Symbol:          update.Symbol,
		Rate:            update.Rate,
		PredictedRate:   update.PredictedRate,
		NextFundingTime: update.NextFundingTime,
		Timestamp:       update.Timestamp,
	}

	m.updateRate(update.Exchange, update.Symbol, rate)

	// Broadcast
	m.subMu.RLock()
	defer m.subMu.RUnlock()

	for _, sub := range m.subscribers {
		if sub.exchange != "" && sub.exchange != update.Exchange {
			continue
		}
		if sub.symbol != "" && sub.symbol != update.Symbol {
			continue
		}
		select {
		case sub.ch <- update:
		default:
			// Drop if full
		}
	}
}
