package config

import (
	"os"
	"testing"
)

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:  "expand single env var",
			input: "api_key: ${TEST_API_KEY}",
			envVars: map[string]string{
				"TEST_API_KEY": "test_key_123",
			},
			expected: "api_key: test_key_123",
		},
		{
			name:  "expand multiple env vars",
			input: "api_key: ${API_KEY}\nsecret: ${SECRET_KEY}",
			envVars: map[string]string{
				"API_KEY":    "key_value",
				"SECRET_KEY": "secret_value",
			},
			expected: "api_key: key_value\nsecret: secret_value",
		},
		{
			name:     "missing env var returns empty string",
			input:    "api_key: ${MISSING_VAR}",
			envVars:  map[string]string{},
			expected: "api_key: ",
		},
		{
			name:  "mixed static and env vars",
			input: "static_value: 123\napi_key: ${TEST_KEY}",
			envVars: map[string]string{
				"TEST_KEY": "dynamic_key",
			},
			expected: "static_value: 123\napi_key: dynamic_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			result := expandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvVars() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoadConfigWithEnvVars(t *testing.T) {
	// Create a temporary config file with env var placeholders
	tmpFile, err := os.CreateTemp("", "config-test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	configContent := `app:
  current_exchange: "binance"

exchanges:
  binance:
    api_key: "${TEST_BINANCE_API_KEY}"
    secret_key: "${TEST_BINANCE_SECRET_KEY}"
    fee_rate: 0.0002

trading:
  symbol: "BTCUSDT"
  price_interval: 1.0
  order_quantity: 30.0
  min_order_value: 6.0
  buy_window_size: 10
  sell_window_size: 10
  reconcile_interval: 60
  order_cleanup_threshold: 50
  cleanup_batch_size: 10
  margin_lock_duration_seconds: 10
  position_safety_check: 100
  grid_mode: "long"

system:
  log_level: "INFO"
  cancel_on_exit: true

risk_control:
  enabled: true
  monitor_symbols: ["BTCUSDT"]
  interval: "1m"
  volume_multiplier: 3.0
  average_window: 20
  recovery_threshold: 2

timing:
  websocket_reconnect_delay: 5
  websocket_write_wait: 10
  websocket_pong_wait: 60
  websocket_ping_interval: 20
  listen_key_keepalive_interval: 30
  price_send_interval: 50
  rate_limit_retry_delay: 1
  order_retry_delay: 500
  price_poll_interval: 500
  status_print_interval: 1
  order_cleanup_interval: 10
`

	if _, err := tmpFile.Write([]byte(configContent)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Set environment variables
	os.Setenv("TEST_BINANCE_API_KEY", "test_api_key_from_env")
	os.Setenv("TEST_BINANCE_SECRET_KEY", "test_secret_key_from_env")
	defer os.Unsetenv("TEST_BINANCE_API_KEY")
	defer os.Unsetenv("TEST_BINANCE_SECRET_KEY")

	// Load config
	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Verify environment variables were expanded
	binanceConfig := config.Exchanges["binance"]
	if binanceConfig.APIKey != "test_api_key_from_env" {
		t.Errorf("APIKey = %q, want %q", binanceConfig.APIKey, "test_api_key_from_env")
	}
	if binanceConfig.SecretKey != "test_secret_key_from_env" {
		t.Errorf("SecretKey = %q, want %q", binanceConfig.SecretKey, "test_secret_key_from_env")
	}
}

func TestIsCriticalEnvVar(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		expected bool
	}{
		{"binance api key is critical", "BINANCE_API_KEY", true},
		{"binance secret is critical", "BINANCE_SECRET_KEY", true},
		{"okx api key is critical", "OKX_API_KEY", true},
		{"okx secret is critical", "OKX_SECRET_KEY", true},
		{"okx passphrase is critical", "OKX_PASSPHRASE", true},
		{"bybit api key is critical", "BYBIT_API_KEY", true},
		{"bybit secret is critical", "BYBIT_SECRET_KEY", true},
		{"random var is not critical", "RANDOM_VAR", false},
		{"empty var is not critical", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCriticalEnvVar(tt.envVar)
			if result != tt.expected {
				t.Errorf("isCriticalEnvVar(%q) = %v, want %v", tt.envVar, result, tt.expected)
			}
		})
	}
}
