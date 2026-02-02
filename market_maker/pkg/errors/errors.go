package apperrors

import "errors"

// Standardized Exchange Errors
var (
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrOrderRejected         = errors.New("order rejected")
	ErrRateLimitExceeded     = errors.New("rate limit exceeded")
	ErrNetwork               = errors.New("network error")
	ErrInvalidSymbol         = errors.New("invalid symbol")
	ErrAuthenticationFailed  = errors.New("authentication failed")
	ErrExchangeMaintenance   = errors.New("exchange maintenance")
	ErrOrderNotFound         = errors.New("order not found")
	ErrDuplicateOrder        = errors.New("duplicate order")
	ErrInvalidOrderParameter = errors.New("invalid order parameter")
	ErrSystemOverload        = errors.New("system overload")
	ErrTimestampOutOfBounds  = errors.New("timestamp out of bounds")
)
