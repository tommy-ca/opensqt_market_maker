// Package config handles configuration management with validation
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the complete configuration structure
type Config struct {
	App         AppConfig                 `yaml:"app"`
	Exchanges   map[string]ExchangeConfig `yaml:"exchanges"`
	Trading     TradingConfig             `yaml:"trading"`
	System      SystemConfig              `yaml:"system"`
	RiskControl RiskControlConfig         `yaml:"risk_control"`
	Timing      TimingConfig              `yaml:"timing"`
	Concurrency ConcurrencyConfig         `yaml:"concurrency"`
	Telemetry   TelemetryConfig           `yaml:"telemetry"`
}

// TelemetryConfig contains telemetry settings
type TelemetryConfig struct {
	MetricsPort   int  `yaml:"metrics_port"`
	EnableMetrics bool `yaml:"enable_metrics"`
}

// AppConfig contains application-level settings
type AppConfig struct {
	CurrentExchange string   `yaml:"current_exchange"` // Legacy: primary exchange
	ActiveExchanges []string `yaml:"active_exchanges"` // List of active exchanges
	EngineType      string   `yaml:"engine_type" validate:"required,oneof=simple dbos"`
	DatabaseURL     string   `yaml:"database_url"` // Required for DBOS
}

// ExchangeConfig contains exchange-specific configuration
type ExchangeConfig struct {
	APIKey        string  `yaml:"api_key" validate:"required"`
	SecretKey     string  `yaml:"secret_key" validate:"required"`
	Passphrase    string  `yaml:"passphrase"` // Required for some exchanges
	BaseURL       string  `yaml:"base_url"`   // Optional override for API URL
	FeeRate       float64 `yaml:"fee_rate" validate:"required,min=0,max=1"`
	TLSCertFile   string  `yaml:"tls_cert_file"`   // TLS certificate file for gRPC (remote only)
	TLSKeyFile    string  `yaml:"tls_key_file"`    // TLS key file for gRPC server (remote only)
	TLSServerName string  `yaml:"tls_server_name"` // TLS server name for verification (remote only)
	GRPCAPIKeys   string  `yaml:"grpc_api_keys"`   // Comma-separated API keys for gRPC authentication (server only)
	GRPCAPIKey    string  `yaml:"grpc_api_key"`    // Single API key for gRPC client authentication
	GRPCRateLimit int     `yaml:"grpc_rate_limit"` // Rate limit per API key (requests per second)
}

// TradingConfig contains trading parameters
type TradingConfig struct {
	StrategyType              string  `yaml:"strategy_type" validate:"oneof=grid arbitrage"`
	Symbol                    string  `yaml:"symbol" validate:"required"`
	PriceInterval             float64 `yaml:"price_interval" validate:"required_if=StrategyType grid,min=0"`
	OrderQuantity             float64 `yaml:"order_quantity" validate:"required,min=0.00001"`
	MinOrderValue             float64 `yaml:"min_order_value" validate:"required,min=0"`
	BuyWindowSize             int     `yaml:"buy_window_size" validate:"required_if=StrategyType grid,min=1,max=200"`
	SellWindowSize            int     `yaml:"sell_window_size" validate:"required_if=StrategyType grid,min=1,max=200"`
	ReconcileInterval         int     `yaml:"reconcile_interval" validate:"required,min=1,max=3600"`
	OrderCleanupThreshold     int     `yaml:"order_cleanup_threshold" validate:"required,min=1,max=1000"`
	CleanupBatchSize          int     `yaml:"cleanup_batch_size" validate:"required,min=1,max=100"`
	MarginLockDurationSeconds int     `yaml:"margin_lock_duration_seconds" validate:"required,min=1,max=300"`
	PositionSafetyCheck       int     `yaml:"position_safety_check" validate:"required,min=1,max=1000"`
	GridMode                  string  `yaml:"grid_mode" validate:"oneof=long neutral"`
	DynamicInterval           bool    `yaml:"dynamic_interval"`
	VolatilityScale           float64 `yaml:"volatility_scale" validate:"min=0,max=100"`
	InventorySkewFactor       float64 `yaml:"inventory_skew_factor" validate:"min=0,max=1"`

	// Arbitrage Specific
	ArbitrageSpotExchange string  `yaml:"arbitrage_spot_exchange"`
	ArbitragePerpExchange string  `yaml:"arbitrage_perp_exchange"`
	MinSpreadAPR          float64 `yaml:"min_spread_apr"`
	ExitSpreadAPR         float64 `yaml:"exit_spread_apr"`
	LiquidationThreshold  float64 `yaml:"liquidation_threshold"`
}

// SystemConfig contains system settings
type SystemConfig struct {
	LogLevel      string `yaml:"log_level" validate:"required,oneof=DEBUG INFO WARN ERROR FATAL"`
	CancelOnExit  bool   `yaml:"cancel_on_exit"`
	AgentGRPCPort string `yaml:"agent_grpc_port"` // Port for agent observability API (default: 50052)
}

// RiskControlConfig contains risk control settings
type RiskControlConfig struct {
	Enabled           bool     `yaml:"enabled"`
	MonitorSymbols    []string `yaml:"monitor_symbols" validate:"required,min=1,max=10"`
	Interval          string   `yaml:"interval" validate:"required,oneof=1m 3m 5m"`
	VolumeMultiplier  float64  `yaml:"volume_multiplier" validate:"required,min=1,max=10"`
	AverageWindow     int      `yaml:"average_window" validate:"required,min=5,max=100"`
	RecoveryThreshold int      `yaml:"recovery_threshold" validate:"required,min=1,max=10"`
	GlobalStrategy    string   `yaml:"global_strategy" validate:"oneof=Any All"`
}

// TimingConfig contains timing-related settings
type TimingConfig struct {
	WebsocketReconnectDelay    int `yaml:"websocket_reconnect_delay" validate:"min=1,max=300"`
	WebsocketWriteWait         int `yaml:"websocket_write_wait" validate:"min=1,max=300"`
	WebsocketPongWait          int `yaml:"websocket_pong_wait" validate:"min=1,max=300"`
	WebsocketPingInterval      int `yaml:"websocket_ping_interval" validate:"min=1,max=300"`
	ListenKeyKeepaliveInterval int `yaml:"listen_key_keepalive_interval" validate:"min=1,max=3600"`
	PriceSendInterval          int `yaml:"price_send_interval" validate:"min=1,max=1000"`
	RateLimitRetryDelay        int `yaml:"rate_limit_retry_delay" validate:"min=1,max=300"`
	OrderRetryDelay            int `yaml:"order_retry_delay" validate:"min=1,max=10000"`
	PricePollInterval          int `yaml:"price_poll_interval" validate:"min=1,max=10000"`
	StatusPrintInterval        int `yaml:"status_print_interval" validate:"min=1,max=60"`
	OrderCleanupInterval       int `yaml:"order_cleanup_interval" validate:"min=1,max=300"`
}

// ConcurrencyConfig contains worker pool settings
type ConcurrencyConfig struct {
	RiskPoolSize        int `yaml:"risk_pool_size" validate:"min=1,max=100"`
	RiskPoolBuffer      int `yaml:"risk_pool_buffer" validate:"min=1,max=10000"`
	BroadcastPoolSize   int `yaml:"broadcast_pool_size" validate:"min=1,max=100"`
	BroadcastPoolBuffer int `yaml:"broadcast_pool_buffer" validate:"min=1,max=10000"`
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s' (value: %v): %s", e.Field, e.Value, e.Message)
}

// LoadConfig loads configuration from a YAML file with environment variable expansion
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in the YAML content
	expandedData := expandEnvVars(string(data))

	var config Config
	if err := yaml.Unmarshal([]byte(expandedData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// Validate performs comprehensive validation of the configuration
func (c *Config) Validate() error {
	var errors []string

	// Validate app config
	if err := c.validateAppConfig(); err != nil {
		errors = append(errors, err.Error())
	}

	// Validate exchanges
	if err := c.validateExchanges(); err != nil {
		errors = append(errors, err.Error())
	}

	// Validate trading config
	if err := c.validateTradingConfig(); err != nil {
		errors = append(errors, err.Error())
	}

	// Validate system config
	if err := c.validateSystemConfig(); err != nil {
		errors = append(errors, err.Error())
	}

	// Validate risk control config
	if err := c.validateRiskControlConfig(); err != nil {
		errors = append(errors, err.Error())
	}

	// Validate timing config
	if err := c.validateTimingConfig(); err != nil {
		errors = append(errors, err.Error())
	}

	// Validate concurrency config
	if err := c.validateConcurrencyConfig(); err != nil {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

func (c *Config) validateAppConfig() error {
	validExchanges := []string{"binance", "bitget", "gate", "okx", "bybit", "mock", "remote", "binance_spot"}

	// Fallback logic: If ActiveExchanges is empty, use CurrentExchange
	if len(c.App.ActiveExchanges) == 0 {
		if c.App.CurrentExchange != "" {
			c.App.ActiveExchanges = []string{c.App.CurrentExchange}
		} else {
			return ValidationError{
				Field:   "app.active_exchanges",
				Message: "at least one exchange must be active",
			}
		}
	}

	for _, ex := range c.App.ActiveExchanges {
		if !contains(validExchanges, ex) {
			return ValidationError{
				Field:   "app.active_exchanges",
				Value:   ex,
				Message: fmt.Sprintf("must be one of: %s", strings.Join(validExchanges, ", ")),
			}
		}

		if ex == "mock" || ex == "remote" {
			continue
		}

		if _, exists := c.Exchanges[ex]; !exists {
			return ValidationError{
				Field:   "app.active_exchanges",
				Value:   ex,
				Message: "exchange configuration not found in exchanges section",
			}
		}
	}

	return nil
}

func (c *Config) validateExchanges() error {
	if len(c.Exchanges) == 0 {
		return ValidationError{
			Field:   "exchanges",
			Message: "at least one exchange must be configured",
		}
	}

	for name, exchange := range c.Exchanges {
		// Skip validation for remote exchange (no API keys needed)
		if name == "remote" {
			continue
		}

		if exchange.APIKey == "" {
			return ValidationError{
				Field:   fmt.Sprintf("exchanges.%s.api_key", name),
				Message: "API key is required",
			}
		}
		if exchange.SecretKey == "" {
			return ValidationError{
				Field:   fmt.Sprintf("exchanges.%s.secret_key", name),
				Message: "secret key is required",
			}
		}
	}

	return nil
}

func (c *Config) validateTradingConfig() error {
	if c.Trading.Symbol == "" {
		return ValidationError{
			Field:   "trading.symbol",
			Message: "trading symbol is required",
		}
	}

	if c.Trading.StrategyType == "grid" {
		if c.Trading.PriceInterval <= 0 {
			return ValidationError{
				Field:   "trading.price_interval",
				Value:   c.Trading.PriceInterval,
				Message: "price interval must be positive",
			}
		}
	}

	if c.Trading.OrderQuantity <= 0 {
		return ValidationError{
			Field:   "trading.order_quantity",
			Value:   c.Trading.OrderQuantity,
			Message: "order quantity must be positive",
		}
	}

	return nil
}

func (c *Config) validateSystemConfig() error {
	validLevels := []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
	if !contains(validLevels, strings.ToUpper(c.System.LogLevel)) {
		return ValidationError{
			Field:   "system.log_level",
			Value:   c.System.LogLevel,
			Message: fmt.Sprintf("must be one of: %s", strings.Join(validLevels, ", ")),
		}
	}
	return nil
}

func (c *Config) validateRiskControlConfig() error {
	if !c.RiskControl.Enabled {
		return nil // Skip validation if disabled
	}

	if len(c.RiskControl.MonitorSymbols) == 0 {
		return ValidationError{
			Field:   "risk_control.monitor_symbols",
			Message: "at least one monitor symbol required when risk control is enabled",
		}
	}

	return nil
}

func (c *Config) validateTimingConfig() error {
	return nil
}

func (c *Config) validateConcurrencyConfig() error {
	return nil
}

// GetCurrentExchangeConfig returns the configuration for the currently selected exchange
func (c *Config) GetCurrentExchangeConfig() (*ExchangeConfig, error) {
	exchange, exists := c.Exchanges[c.App.CurrentExchange]
	if !exists {
		return nil, fmt.Errorf("exchange configuration not found for: %s", c.App.CurrentExchange)
	}
	return &exchange, nil
}

// String returns a string representation of the configuration (with sensitive data masked)
func (c *Config) String() string {
	// Create a copy with sensitive data masked
	configCopy := *c
	for name, exchange := range configCopy.Exchanges {
		exchange.APIKey = maskString(exchange.APIKey)
		exchange.SecretKey = maskString(exchange.SecretKey)
		configCopy.Exchanges[name] = exchange
	}

	data, _ := yaml.Marshal(configCopy)
	return string(data)
}

// Helper functions

func expandEnvVars(s string) string {
	return os.Expand(s, func(key string) string {
		value := os.Getenv(key)
		if value == "" && isCriticalEnvVar(key) {
			return ""
		}
		return value
	})
}

// isCriticalEnvVar checks if an environment variable is critical for operation
func isCriticalEnvVar(key string) bool {
	criticalVars := []string{
		"BINANCE_API_KEY", "BINANCE_SECRET_KEY",
		"OKX_API_KEY", "OKX_SECRET_KEY", "OKX_PASSPHRASE",
		"BYBIT_API_KEY", "BYBIT_SECRET_KEY",
	}
	return contains(criticalVars, key)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func maskString(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

// DefaultConfig returns a default configuration for testing
func DefaultConfig() *Config {
	return &Config{
		App: AppConfig{
			CurrentExchange: "binance",
			ActiveExchanges: []string{"binance", "binance_spot"},
			EngineType:      "simple",
		},

		Exchanges: map[string]ExchangeConfig{
			"binance": {
				APIKey:    "test_api_key",
				SecretKey: "test_secret_key",
				FeeRate:   0.0002,
			},
			"binance_spot": {
				APIKey:    "test_api_key",
				SecretKey: "test_secret_key",
				FeeRate:   0.0001,
			},
		},
		Trading: TradingConfig{
			StrategyType:              "grid",
			Symbol:                    "BTCUSDT",
			PriceInterval:             1.0,
			OrderQuantity:             30.0,
			MinOrderValue:             6.0,
			BuyWindowSize:             10,
			SellWindowSize:            10,
			ReconcileInterval:         60,
			OrderCleanupThreshold:     50,
			CleanupBatchSize:          10,
			MarginLockDurationSeconds: 10,
			PositionSafetyCheck:       100,
			GridMode:                  "long",
			DynamicInterval:           false,
			VolatilityScale:           1.0,
			InventorySkewFactor:       0.0,

			// Arbitrage Specific
			ArbitrageSpotExchange: "binance_spot",
			ArbitragePerpExchange: "binance",
			MinSpreadAPR:          0.10,
			ExitSpreadAPR:         0.01,
			LiquidationThreshold:  0.10,
		},
		System: SystemConfig{
			LogLevel:     "INFO",
			CancelOnExit: true,
		},
		RiskControl: RiskControlConfig{
			Enabled:        true,
			MonitorSymbols: []string{"BTCUSDT", "ETHUSDT"},
			Interval:       "1m",
		},
	}
}
