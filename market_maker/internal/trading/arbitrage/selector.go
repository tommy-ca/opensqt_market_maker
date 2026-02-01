package arbitrage

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

const (
	historyCacheTTL = 4 * time.Hour
)

// Opportunity represents an arbitrage opportunity
type Opportunity struct {
	Symbol        string
	Strategy      string // "SpotPerp", "CrossExchange"
	LongExchange  string
	ShortExchange string
	Spread        decimal.Decimal
	SpreadAPR     decimal.Decimal
	Basis         decimal.Decimal
	QualityScore  decimal.Decimal
	Metrics       FundingMetrics
	Timestamp     time.Time
}

// ScannerStrategyConfig defines a pair of exchanges to monitor
type ScannerStrategyConfig struct {
	Name           string
	LongCandidate  string // Exchange to prefer Long (e.g. Spot)
	ShortCandidate string // Exchange to prefer Short (e.g. Perp)
}

// UniverseSelector scans for arbitrage opportunities
type UniverseSelector struct {
	exchanges      map[string]core.IExchange
	strategies     []ScannerStrategyConfig
	minLiquidity24 decimal.Decimal
	analyzer       *FundingAnalyzer

	// Caching / State
	mu           sync.RWMutex
	historyCache map[string]*cachedMetrics

	// Worker Pool
	concurrency int
	taskChan    chan scanTask
}

type scanTask struct {
	ctx      context.Context
	strat    ScannerStrategyConfig
	exB      core.IExchange
	symbol   string
	rateA    *pb.FundingRate
	rateB    *pb.FundingRate
	tickA    *pb.Ticker
	tickB    *pb.Ticker
	resultCh chan<- Opportunity
	wg       *sync.WaitGroup
}

type cachedMetrics struct {
	Metrics   FundingMetrics
	UpdatedAt time.Time
}

func (s *UniverseSelector) getHistoryFromCache(exchange, symbol string) (FundingMetrics, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := exchange + ":" + symbol
	entry, ok := s.historyCache[key]
	if !ok {
		return FundingMetrics{}, false
	}

	if time.Since(entry.UpdatedAt) > historyCacheTTL {
		return FundingMetrics{}, false
	}

	return entry.Metrics, true
}

func (s *UniverseSelector) setHistoryToCache(exchange, symbol string, metrics FundingMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := exchange + ":" + symbol
	s.historyCache[key] = &cachedMetrics{
		Metrics:   metrics,
		UpdatedAt: time.Now(),
	}
}

// NewUniverseSelector creates a new UniverseSelector
func NewUniverseSelector(exchanges map[string]core.IExchange) *UniverseSelector {
	s := &UniverseSelector{
		exchanges:      exchanges,
		minLiquidity24: decimal.Zero,
		analyzer:       NewFundingAnalyzer(),
		historyCache:   make(map[string]*cachedMetrics),
		concurrency:    10,
		taskChan:       make(chan scanTask, 100),
	}
	s.startWorkers()
	return s
}

func (s *UniverseSelector) startWorkers() {
	for i := 0; i < s.concurrency; i++ {
		go func() {
			for task := range s.taskChan {
				opp, found := s.analyzeCandidate(task.ctx, task.strat, task.exB, task.symbol, task.rateA, task.rateB, task.tickA, task.tickB)
				if found {
					task.resultCh <- opp
				}
				task.wg.Done()
			}
		}()
	}
}

// SetMinLiquidity sets the minimum 24h quote volume (USDT) required for a symbol
func (s *UniverseSelector) SetMinLiquidity(threshold decimal.Decimal) {
	s.minLiquidity24 = threshold
}

// AddStrategy adds a scanning strategy (e.g. Spot vs Perp)
func (s *UniverseSelector) AddStrategy(name, legA, legB string) {
	s.strategies = append(s.strategies, ScannerStrategyConfig{
		Name:           name,
		LongCandidate:  legA,
		ShortCandidate: legB,
	})
}

// ClearCache clears the historical funding cache
func (s *UniverseSelector) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.historyCache = make(map[string]*cachedMetrics)
}

// Scan scans all configured strategies for opportunities in parallel using a worker pool
func (s *UniverseSelector) Scan(ctx context.Context) ([]Opportunity, error) {
	var opportunities []Opportunity

	for _, strat := range s.strategies {
		exA, okA := s.exchanges[strat.LongCandidate]
		exB, okB := s.exchanges[strat.ShortCandidate]

		if !okA || !okB {
			continue
		}

		// 1. Identify Liquid Universe
		liquidUniverse, err := s.getLiquidUniverse(ctx, exA, exB)
		if err != nil {
			continue
		}

		if len(liquidUniverse) == 0 {
			continue
		}

		// 2. Fetch Bulk Data (Current Rates and Tickers)
		ratesA, errA := exA.GetFundingRates(ctx)
		ratesB, errB := exB.GetFundingRates(ctx)
		if errA != nil || errB != nil {
			continue
		}

		tickersA, _ := exA.GetTickers(ctx)
		tickersB, _ := exB.GetTickers(ctx)

		// Indexing for fast lookup
		mapA := make(map[string]*pb.FundingRate)
		for _, r := range ratesA {
			mapA[r.Symbol] = r
		}
		mapTickA := make(map[string]*pb.Ticker)
		for _, t := range tickersA {
			mapTickA[t.Symbol] = t
		}
		mapTickB := make(map[string]*pb.Ticker)
		for _, t := range tickersB {
			mapTickB[t.Symbol] = t
		}

		// 3. Parallel Historical Analysis using Worker Pool
		resultsCh := make(chan Opportunity, len(ratesB))
		var wg sync.WaitGroup

		for _, rB := range ratesB {
			symbol := rB.Symbol
			if !liquidUniverse[symbol] {
				continue
			}
			rA, ok := mapA[symbol]
			if !ok {
				continue
			}

			wg.Add(1)
			s.taskChan <- scanTask{
				ctx:      ctx,
				strat:    strat,
				exB:      exB,
				symbol:   symbol,
				rateA:    rA,
				rateB:    rB,
				tickA:    mapTickA[symbol],
				tickB:    mapTickB[symbol],
				resultCh: resultsCh,
				wg:       &wg,
			}
		}

		// Wait for all tasks for this strategy to finish
		wg.Wait()
		close(resultsCh)

		// Collect results
		for opp := range resultsCh {
			opportunities = append(opportunities, opp)
		}
	}

	// Sort by Quality Score (Descending)
	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].QualityScore.GreaterThan(opportunities[j].QualityScore)
	})

	return opportunities, nil
}

func (s *UniverseSelector) analyzeCandidate(
	ctx context.Context,
	strat ScannerStrategyConfig,
	exB core.IExchange,
	symbol string,
	rateA, rateB *pb.FundingRate,
	tickA, tickB *pb.Ticker,
) (Opportunity, bool) {
	valA := pbu.ToGoDecimal(rateA.Rate)
	valB := pbu.ToGoDecimal(rateB.Rate)

	// Current Spread
	spread := valB.Sub(valA)
	apr := spread.Mul(decimal.NewFromInt(3)).Mul(decimal.NewFromInt(365))

	// Filter out low yield immediately
	if apr.LessThan(decimal.NewFromFloat(0.01)) {
		return Opportunity{}, false
	}

	// 4. Advanced Analysis (Historical)
	metrics, cached := s.getHistoryFromCache(exB.GetName(), symbol)
	if !cached {
		// Fetch last 30 days of funding (90 intervals)
		histB, err := exB.GetHistoricalFundingRates(ctx, symbol, 90)
		if err != nil {
			return Opportunity{}, false
		}

		// Fetch last 30 days of candles (daily) for volatility
		candlesB, err := exB.GetHistoricalKlines(ctx, symbol, "1d", 30)
		if err != nil {
			return Opportunity{}, false
		}

		metrics = s.analyzer.Analyze(histB, candlesB)
		s.setHistoryToCache(exB.GetName(), symbol, metrics)
	}

	// Fetch Open Interest
	oi, _ := exB.GetOpenInterest(ctx, symbol)
	oiF, _ := oi.Float64()

	vol24F := 0.0
	priceB := 0.0
	priceA := 0.0

	if tickB != nil {
		vol24F, _ = pbu.ToGoDecimal(tickB.QuoteVolume).Float64()
		priceB, _ = pbu.ToGoDecimal(tickB.LastPrice).Float64()
	}
	if tickA != nil {
		priceA, _ = pbu.ToGoDecimal(tickA.LastPrice).Float64()
	}

	if vol24F > 0 {
		metrics.OIFactor = decimal.NewFromFloat(oiF / vol24F)
	}

	basis := 0.0
	if priceA > 0 && priceB > 0 {
		basis = (priceB - priceA) / priceA
	}

	// 5. Scoring (v3 Pillars)
	score := s.calculateQualityScore(metrics, rateB, basis)

	return Opportunity{
		Symbol:        symbol,
		Strategy:      strat.Name,
		LongExchange:  strat.LongCandidate,
		ShortExchange: strat.ShortCandidate,
		Spread:        spread,
		SpreadAPR:     apr,
		Basis:         decimal.NewFromFloat(basis),
		QualityScore:  score,
		Metrics:       metrics,
		Timestamp:     time.Now(),
	}, true
}

func (s *UniverseSelector) calculateQualityScore(m FundingMetrics, currentRateB *pb.FundingRate, basis float64) decimal.Decimal {
	// PILLAR 1: YIELD (Reward)
	// Weighted average of historical SMAs.
	yield := m.SMA1d.Mul(decimal.NewFromFloat(0.5)).
		Add(m.SMA7d.Mul(decimal.NewFromFloat(0.3))).
		Add(m.SMA30d.Mul(decimal.NewFromFloat(0.2)))

	if yield.IsNegative() || yield.IsZero() {
		return decimal.Zero // Strategy requires positive funding
	}

	// PILLAR 2: RISK (Safety)
	// Consolidates Stability, Volatility, and Sign Flips.
	stability := m.StabilityScore.Div(decimal.NewFromInt(10))
	if stability.GreaterThan(decimal.NewFromInt(1)) {
		stability = decimal.NewFromInt(1)
	}

	// Use Decimal for arithmetic to maintain precision
	volatilityPenalty := decimal.NewFromInt(1).Div(decimal.NewFromInt(1).Add(m.VolatilityScore.Mul(decimal.NewFromInt(100))))
	flipPenalty := decimal.NewFromInt(1).Div(decimal.NewFromInt(int64(1 + m.NumSignFlips)))
	positiveRatio := m.PositiveRatio

	riskPillar := stability.Mul(volatilityPenalty).Mul(flipPenalty).Mul(positiveRatio)

	// PILLAR 3: MATURITY (Momentum & Basis)
	// Consolidates Regime Duration, Open Interest, Momentum, and Basis.
	durationFactor := decimal.NewFromFloat(1.0 + math.Log10(float64(m.CurrentDuration+1)))

	oiFactor := m.OIFactor
	if oiFactor.GreaterThan(decimal.NewFromInt(2)) {
		oiFactor = decimal.NewFromInt(2)
	}

	momentumFactor := decimal.NewFromInt(1).Add(m.Momentum.Mul(decimal.NewFromInt(1000)))
	if momentumFactor.LessThan(decimal.NewFromFloat(0.5)) {
		momentumFactor = decimal.NewFromFloat(0.5)
	} else if momentumFactor.GreaterThan(decimal.NewFromFloat(1.5)) {
		momentumFactor = decimal.NewFromFloat(1.5)
	}

	// Basis & Predicted convergence
	basisDec := decimal.NewFromFloat(basis)
	basisFactor := decimal.NewFromInt(1).Add(basisDec.Mul(decimal.NewFromInt(100)))
	if basisFactor.LessThan(decimal.NewFromFloat(0.8)) {
		basisFactor = decimal.NewFromFloat(0.8)
	} else if basisFactor.GreaterThan(decimal.NewFromFloat(1.2)) {
		basisFactor = decimal.NewFromFloat(1.2)
	}

	predictedFactor := decimal.NewFromInt(1)
	if currentRateB.PredictedRate != nil {
		curr := pbu.ToGoDecimal(currentRateB.Rate)
		pred := pbu.ToGoDecimal(currentRateB.PredictedRate)
		if curr.IsPositive() {
			predictedFactor = pred.Div(curr)
			if predictedFactor.LessThan(decimal.NewFromFloat(0.5)) {
				predictedFactor = decimal.NewFromFloat(0.5)
			}
		}
	}

	maturityPillar := durationFactor.Mul(decimal.NewFromInt(1).Add(oiFactor)).Mul(momentumFactor).Mul(basisFactor).Mul(predictedFactor)

	// Final Score: Product of the Three Pillars
	return yield.Mul(riskPillar).Mul(maturityPillar)
}

// getLiquidUniverse returns a set of symbols that meet the liquidity threshold on BOTH exchanges
func (s *UniverseSelector) getLiquidUniverse(ctx context.Context, exA, exB core.IExchange) (map[string]bool, error) {
	if s.minLiquidity24.IsZero() {
		symsA, _ := exA.GetSymbols(ctx)
		symsB, _ := exB.GetSymbols(ctx)
		set := make(map[string]bool)
		mapA := make(map[string]bool)
		for _, s := range symsA {
			mapA[s] = true
		}
		for _, s := range symsB {
			if mapA[s] {
				set[s] = true
			}
		}
		return set, nil
	}

	tickersA, errA := exA.GetTickers(ctx)
	tickersB, errB := exB.GetTickers(ctx)
	if errA != nil || errB != nil {
		return nil, fmt.Errorf("failed to fetch tickers")
	}

	liquidA := make(map[string]bool)
	for _, t := range tickersA {
		if pbu.ToGoDecimal(t.QuoteVolume).GreaterThanOrEqual(s.minLiquidity24) {
			liquidA[t.Symbol] = true
		}
	}

	intersection := make(map[string]bool)
	for _, t := range tickersB {
		if liquidA[t.Symbol] && pbu.ToGoDecimal(t.QuoteVolume).GreaterThanOrEqual(s.minLiquidity24) {
			intersection[t.Symbol] = true
		}
	}

	return intersection, nil
}
