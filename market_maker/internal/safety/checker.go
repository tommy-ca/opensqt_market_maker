// Package safety provides safety checks and risk control mechanisms
package safety

import (
	"context"
	"fmt"
	"market_maker/pkg/pbu"

	"github.com/shopspring/decimal"
	"market_maker/internal/core"
)

// SafetyChecker implements safety validation checks
type SafetyChecker struct {
	logger core.ILogger
}

// NewSafetyChecker creates a new safety checker
func NewSafetyChecker(logger core.ILogger) *SafetyChecker {
	return &SafetyChecker{
		logger: logger,
	}
}

// CheckAccountSafety performs comprehensive safety checks before starting trading
func (s *SafetyChecker) CheckAccountSafety(
	ctx context.Context,
	exchange core.IExchange,
	symbol string,
	currentPrice decimal.Decimal,
	orderAmount decimal.Decimal,
	priceInterval decimal.Decimal,
	feeRate decimal.Decimal,
	requiredPositions int,
	priceDecimals int,
) error {

	s.logger.Info("Starting account safety check", "symbol", symbol, "price", currentPrice)

	// 1. Get current positions
	positions, err := exchange.GetPositions(ctx, symbol)
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	existingPositionSize := decimal.Zero
	for _, pos := range positions {
		if pos.Symbol == symbol {
			existingPositionSize = existingPositionSize.Add(pbu.ToGoDecimal(pos.Size).Abs())
		}
	}

	// 🔥 If current account has positions, skip safety check (matching legacy)
	if !existingPositionSize.IsZero() {
		s.logger.Info("Detected existing position, skipping safety check", "existing_size", existingPositionSize)
		return nil
	}

	// 2. Get account information
	account, err := exchange.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("failed to get account info: %w", err)
	}

	availBalance := pbu.ToGoDecimal(account.AvailableBalance)
	s.logger.Info("Account info retrieved",
		"total_balance", pbu.ToGoDecimal(account.TotalWalletBalance),
		"available_balance", availBalance,
		"leverage", account.AccountLeverage)

	// 3. Check balance sufficiency
	if availBalance.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("insufficient account balance: %s", availBalance)
	}

	// 4. Check leverage limits (max 10x as per original code)
	if account.AccountLeverage > 10 {
		return fmt.Errorf("account leverage too high: %d (max allowed: 10)", account.AccountLeverage)
	}

	// 5. Calculate maximum allowed positions
	maxAllowedPositions := s.calculateMaxPositions(
		availBalance,
		int(account.AccountLeverage),
		orderAmount,
		currentPrice,
	)

	if requiredPositions > maxAllowedPositions {
		return fmt.Errorf("required positions (%d) exceed maximum allowed (%d) based on account balance and leverage",
			requiredPositions, maxAllowedPositions)
	}

	// 6. Profitability Check (Matching Legacy)
	// Profit = PriceInterval
	// Total Fees = (Buy Price + Sell Price) * FeeRate
	buyPrice := currentPrice
	sellPrice := currentPrice.Add(priceInterval)
	totalFees := buyPrice.Add(sellPrice).Mul(feeRate)
	netProfit := priceInterval.Sub(totalFees)

	if netProfit.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("negative or zero net profit per trade: %s (Profit: %s, Fees: %s). Increase price interval or reduce fee rate",
			netProfit, priceInterval, totalFees)
	}

	s.logger.Info("Profitability check passed", "net_profit", netProfit, "fees", totalFees)

	// 7. Validate price precision
	if priceDecimals < 0 || priceDecimals > 8 {
		return fmt.Errorf("invalid price decimals: %d (must be 0-8)", priceDecimals)
	}

	s.logger.Info("Account safety check completed successfully",
		"max_allowed_positions", maxAllowedPositions,
		"net_profit_per_trade", netProfit)

	return nil
}

// calculateMaxPositions calculates the maximum number of positions allowed
func (s *SafetyChecker) calculateMaxPositions(
	availableBalance decimal.Decimal,
	leverage int,
	orderAmount decimal.Decimal,
	currentPrice decimal.Decimal,
) int {

	// Maximum usable margin = available balance * leverage
	maxMargin := availableBalance.Mul(decimal.NewFromInt(int64(leverage)))

	// Cost per position = order amount (in USDT)
	costPerPosition := orderAmount

	if costPerPosition.IsZero() {
		return 1
	}

	// Maximum positions = max margin / cost per position
	maxPositions := int(maxMargin.Div(costPerPosition).IntPart())

	// Apply safety buffer (80% of calculated maximum)
	safeMaxPositions := int(float64(maxPositions) * 0.8)

	// Ensure minimum of 1 position
	if safeMaxPositions < 1 {
		safeMaxPositions = 1
	}

	// Cap at reasonable maximum (1000 positions)
	if safeMaxPositions > 1000 {
		safeMaxPositions = 1000
	}

	return safeMaxPositions
}

// estimateTradingFees estimates total trading fees for a trading session
func (s *SafetyChecker) estimateTradingFees(orderAmount decimal.Decimal, numPositions int, feeRate float64) decimal.Decimal {
	// Rough estimate: 2 trades per position (buy + sell) * fee rate * order amount
	tradesPerPosition := 2.0
	totalOrderValue := orderAmount.Mul(decimal.NewFromInt(int64(numPositions))).Mul(decimal.NewFromFloat(tradesPerPosition))
	return totalOrderValue.Mul(decimal.NewFromFloat(feeRate))
}

// ValidateTradingParameters validates trading parameters for safety
func (s *SafetyChecker) ValidateTradingParameters(
	symbol string,
	priceInterval decimal.Decimal,
	orderQuantity decimal.Decimal,
	minOrderValue decimal.Decimal,
	buyWindowSize int,
	sellWindowSize int,
) error {

	// Validate symbol
	if symbol == "" {
		return fmt.Errorf("trading symbol cannot be empty")
	}

	// Validate price interval
	if priceInterval.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("price interval must be positive: %s", priceInterval)
	}

	// Validate order quantity
	if orderQuantity.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("order quantity must be positive: %s", orderQuantity)
	}

	// Validate minimum order value
	if minOrderValue.LessThan(decimal.Zero) {
		return fmt.Errorf("minimum order value cannot be negative: %s", minOrderValue)
	}

	// Validate window sizes
	if buyWindowSize <= 0 || buyWindowSize > 200 {
		return fmt.Errorf("buy window size must be between 1 and 200: %d", buyWindowSize)
	}

	if sellWindowSize <= 0 || sellWindowSize > 200 {
		return fmt.Errorf("sell window size must be between 1 and 200: %d", sellWindowSize)
	}

	// Check for reasonable limits
	totalWindowSize := buyWindowSize + sellWindowSize
	if totalWindowSize > 300 {
		s.logger.Warn("Large total window size may impact performance",
			"total_windows", totalWindowSize,
			"recommended_max", 300)
	}

	return nil
}

// CheckExchangeConnectivity performs basic connectivity checks
func (s *SafetyChecker) CheckExchangeConnectivity(ctx context.Context, exchange core.IExchange, symbol string) error {
	s.logger.Info("Checking exchange connectivity", "exchange", exchange.GetName())

	// Test account access
	_, err := exchange.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("account access failed: %w", err)
	}

	// Test price access
	price, err := exchange.GetLatestPrice(ctx, symbol)
	if err != nil {
		return fmt.Errorf("price access failed: %w", err)
	}

	if price.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("invalid price received: %s", price)
	}

	// Test order book access (if available)
	_, err = exchange.GetOpenOrders(ctx, symbol, false)
	if err != nil {
		s.logger.Warn("Open orders access failed (may be normal)", "error", err.Error())
	}

	s.logger.Info("Exchange connectivity check passed",
		"exchange", exchange.GetName(),
		"price", price)

	return nil
}
