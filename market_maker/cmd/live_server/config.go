package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the live_server configuration
type Config struct {
	App struct {
		CurrentExchange string `yaml:"current_exchange"`
	} `yaml:"app"`

	Exchanges map[string]ExchangeConfig `yaml:"exchanges"`

	Trading struct {
		Symbol          string `yaml:"symbol"`
		Interval        string `yaml:"interval"`
		HistoricalLimit int    `yaml:"historical_limit"`
	} `yaml:"trading"`

	Server struct {
		Port           string   `yaml:"port"`
		EnableCORS     bool     `yaml:"enable_cors"`
		AllowedOrigins []string `yaml:"allowed_origins"`
		EnableAuth     bool     `yaml:"enable_auth"`
		JWTSecret      string   `yaml:"jwt_secret"`
		APIKeys        []string `yaml:"api_keys"`
	} `yaml:"server"`

	Web struct {
		Directory string `yaml:"directory"`
	} `yaml:"web"`

	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
		Output string `yaml:"output"`
	} `yaml:"logging"`

	Performance struct {
		MaxClients      int `yaml:"max_clients"`
		BroadcastBuffer int `yaml:"broadcast_buffer"`
		ClientBuffer    int `yaml:"client_buffer"`
		PingInterval    int `yaml:"ping_interval"`
		PongTimeout     int `yaml:"pong_timeout"`
		ReadTimeout     int `yaml:"read_timeout"`
		WriteTimeout    int `yaml:"write_timeout"`
	} `yaml:"performance"`

	Health struct {
		Enabled  bool   `yaml:"enabled"`
		Endpoint string `yaml:"endpoint"`
	} `yaml:"health"`

	MarketMaker struct {
		GRPCHost string `yaml:"grpc_host"`
		GRPCPort string `yaml:"grpc_port"`
	} `yaml:"market_maker"`
}

// ExchangeConfig represents exchange-specific configuration
type ExchangeConfig struct {
	APIKey     string `yaml:"api_key"`
	SecretKey  string `yaml:"secret_key"`
	Passphrase string `yaml:"passphrase,omitempty"`
	Testnet    bool   `yaml:"testnet,omitempty"`
}

// LoadConfig loads the configuration from a YAML file
func LoadConfig(configPath string) (*Config, error) {
	// Read file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expandedData := expandEnvVars(string(data))

	// Parse YAML
	var config Config
	if err := yaml.Unmarshal([]byte(expandedData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Validate
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.App.CurrentExchange == "" {
		return fmt.Errorf("app.current_exchange is required")
	}

	if _, ok := c.Exchanges[c.App.CurrentExchange]; !ok {
		return fmt.Errorf("exchange '%s' not configured", c.App.CurrentExchange)
	}

	if c.Trading.Symbol == "" {
		return fmt.Errorf("trading.symbol is required")
	}

	if c.Trading.Interval == "" {
		c.Trading.Interval = "1m" // Default
	}

	if c.Trading.HistoricalLimit <= 0 {
		c.Trading.HistoricalLimit = 100 // Default
	}

	if c.Server.Port == "" {
		c.Server.Port = ":8081" // Default
	}

	if c.Web.Directory == "" {
		c.Web.Directory = "web" // Default
	}

	if c.Logging.Level == "" {
		c.Logging.Level = "INFO" // Default
	}

	if c.Logging.Format == "" {
		c.Logging.Format = "text" // Default
	}

	if c.Performance.BroadcastBuffer <= 0 {
		c.Performance.BroadcastBuffer = 256 // Default
	}

	if c.Performance.ClientBuffer <= 0 {
		c.Performance.ClientBuffer = 256 // Default
	}

	if c.Performance.PingInterval <= 0 {
		c.Performance.PingInterval = 54 // Default
	}

	if c.Performance.PongTimeout <= 0 {
		c.Performance.PongTimeout = 60 // Default
	}

	if c.MarketMaker.GRPCHost == "" {
		c.MarketMaker.GRPCHost = "localhost"
	}

	if c.MarketMaker.GRPCPort == "" {
		c.MarketMaker.GRPCPort = "50052"
	}

	return nil
}

// GetExchangeConfig returns the configuration for the current exchange
func (c *Config) GetExchangeConfig() ExchangeConfig {
	return c.Exchanges[c.App.CurrentExchange]
}

// expandEnvVars expands environment variables in the format ${VAR_NAME}
func expandEnvVars(input string) string {
	return os.Expand(input, func(key string) string {
		// Check if it's in ${VAR} format
		if value := os.Getenv(key); value != "" {
			return value
		}
		// Return original if not found (keeps ${VAR} as is)
		return "${" + key + "}"
	})
}

// IsFutures returns true if the exchange supports futures trading
func (c *Config) IsFutures() bool {
	symbol := strings.ToUpper(c.Trading.Symbol)
	// Futures symbols often contain "USDT" as settlement currency
	// This is a simple heuristic, can be improved
	return strings.Contains(symbol, "USD") && !strings.HasSuffix(symbol, "SPOT")
}

// GetLogLevel returns the log level
func (c *Config) GetLogLevel() string {
	return strings.ToUpper(c.Logging.Level)
}
