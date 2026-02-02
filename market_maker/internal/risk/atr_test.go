package risk

import (
	"market_maker/internal/pb"
	"market_maker/pkg/logging"
	"market_maker/pkg/pbu"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRiskMonitor_CalculateATR(t *testing.T) {
	logger := logging.NewLogger(logging.InfoLevel, nil)
	mockEx := &ServiceMockExchange{}
	mockEx.On("CancelOrder", mock.Anything, "BTCUSDT", mock.AnythingOfType("int64"), false).Return(nil)
	mockEx.On("BatchCancelOrders", mock.Anything, "BTCUSDT", mock.Anything, false).Return(nil)
	mockEx.On("GetOpenOrders", mock.Anything, "BTCUSDT", false).Return([]*pb.Order{}, nil)
	mockEx.On("GetHistoricalKlines", mock.Anything, "BTCUSDT", "1m", mock.Anything).Return([]*pb.Candle{}, nil)

	// Window 3 for easy calculation
	rm := NewRiskMonitor(mockEx, logger, []string{"BTCUSDT"}, "1m", 2.0, 3, 1, "All", nil)

	// Helper to create candle
	createCandle := func(h, l, c float64, closed bool) *pb.Candle {
		return &pb.Candle{
			Symbol:   "BTCUSDT",
			High:     pbu.FromGoDecimal(decimal.NewFromFloat(h)),
			Low:      pbu.FromGoDecimal(decimal.NewFromFloat(l)),
			Close:    pbu.FromGoDecimal(decimal.NewFromFloat(c)),
			Volume:   pbu.FromGoDecimal(decimal.NewFromFloat(100)),
			IsClosed: closed,
		}
	}

	// 1. Initial State
	assert.True(t, rm.GetATR("BTCUSDT").IsZero())

	// 2. Add candles
	// TR1: H=105, L=95, C=100. TR = 10.
	rm.HandleKlineUpdate(createCandle(105, 95, 100, true))
	assert.True(t, rm.GetATR("BTCUSDT").IsZero(), "ATR zero with insufficient data")

	// TR2: H=110, L=100, PrevC=100. H-L=10, |H-PC|=10, |L-PC|=0. TR=10.
	rm.HandleKlineUpdate(createCandle(110, 100, 105, true))

	// TR3: H=105, L=95, PrevC=105. H-L=10, |H-PC|=0, |L-PC|=10. TR=10.
	rm.HandleKlineUpdate(createCandle(105, 95, 100, true))

	// Now we have 3 candles. But calculation loop looks back from len-1 to >0.
	// Index: 0, 1, 2.
	// Loop: i=2. prev=1. TR based on candle 2 and 1.
	// i=1. prev=0. TR based on candle 1 and 0.
	// We need 1 more candle to have 3 periods of TR?
	// Window=3. count < 3.
	// i=2: uses 2 and 1. count=1.
	// i=1: uses 1 and 0. count=2.
	// i=0: loop stops (i>0 condition).
	// So we sum 2 TRs. ATR = Sum / 2?
	// If we want SMA of 3 periods, we need 4 candles (3 intervals).

	// Let's check logic:
	// count=0
	// i=2 (latest): TR(2) = TR(C2, C1). count=1.
	// i=1: TR(1) = TR(C1, C0). count=2.
	// i=0: Stop.
	// ATR = Sum(TR) / 2.

	// Add 4th candle.
	// TR4: H=120, L=80, PrevC=100. H-L=40, |H-PC|=20, |L-PC|=20. TR=40.
	rm.HandleKlineUpdate(createCandle(120, 80, 110, true))

	// i=3: TR(3,2) = 40. count=1.
	// i=2: TR(2,1) = 10. count=2.
	// i=1: TR(1,0) = 10. count=3.
	// Stop. Sum=60. ATR = 60/3 = 20.

	atr := rm.GetATR("BTCUSDT")
	assert.Equal(t, "20", atr.String())
}
