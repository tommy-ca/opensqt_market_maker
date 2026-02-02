package tradingutils

import (
	"github.com/shopspring/decimal"
)

// RoundPrice rounds a price to the specified decimals
func RoundPrice(price decimal.Decimal, priceDecimals int) decimal.Decimal {
	return price.Round(int32(priceDecimals))
}

// RoundQuantity rounds a quantity to the specified decimals
func RoundQuantity(qty decimal.Decimal, qtyDecimals int) decimal.Decimal {
	return qty.Round(int32(qtyDecimals))
}

// CalculatePriceLevels generates a sequence of price levels starting from an anchor
func CalculatePriceLevels(anchorPrice, interval decimal.Decimal, count int) []decimal.Decimal {
	prices := make([]decimal.Decimal, 0, count)
	for i := 1; i <= count; i++ {
		prices = append(prices, anchorPrice.Add(interval.Mul(decimal.NewFromInt(int64(i)))))
	}
	return prices
}

// FindNearestGridPrice aligns a price to the nearest grid level based on an anchor and interval
func FindNearestGridPrice(currentPrice, anchorPrice, interval decimal.Decimal) decimal.Decimal {
	if interval.IsZero() {
		return currentPrice
	}
	offset := currentPrice.Sub(anchorPrice)
	intervals := offset.Div(interval).Round(0)
	return anchorPrice.Add(intervals.Mul(interval))
}

// CalculateNetProfit computes profit after trading fees
func CalculateNetProfit(buyPrice, sellPrice, buyFeeRate, sellFeeRate decimal.Decimal) decimal.Decimal {
	grossProfit := sellPrice.Sub(buyPrice)
	buyFee := buyPrice.Mul(buyFeeRate)
	sellFee := sellPrice.Mul(sellFeeRate)
	return grossProfit.Sub(buyFee).Sub(sellFee)
}

// CalculateSkewedPrice adjusts a base price based on inventory and a skew factor
func CalculateSkewedPrice(basePrice decimal.Decimal, inventory decimal.Decimal, targetInventory decimal.Decimal, skewFactor decimal.Decimal) decimal.Decimal {
	diff := inventory.Sub(targetInventory)
	// Price = BasePrice * (1 - diff * skewFactor)
	// If inventory > target (long), diff is positive, price moves down (to discourage buying/encourage selling)
	adjustment := decimal.NewFromInt(1).Sub(diff.Mul(skewFactor))
	return basePrice.Mul(adjustment)
}
