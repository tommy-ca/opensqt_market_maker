package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadConfigWithEnvVars(t *testing.T) {
	// Create a temporary config file with env var placeholders
	tmpFile, err := os.CreateTemp("", "config-test-*.yaml")
	require.NoError(t, err)
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

	_, err = tmpFile.Write([]byte(configContent))
	require.NoError(t, err)
	tmpFile.Close()

	// Set environment variables
	os.Setenv("TEST_BINANCE_API_KEY", "test_api_key_from_env")
	os.Setenv("TEST_BINANCE_SECRET_KEY", "test_secret_key_from_env")
	defer os.Unsetenv("TEST_BINANCE_API_KEY")
	defer os.Unsetenv("TEST_BINANCE_SECRET_KEY")

	// Load config
	config, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err, "LoadConfig() error")

	// Verify environment variables were expanded
	binanceConfig := config.Exchanges["binance"]
	assert.Equal(t, Secret("test_api_key_from_env"), binanceConfig.APIKey)
	assert.Equal(t, Secret("test_secret_key_from_env"), binanceConfig.SecretKey)
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
			assert.Equal(t, tt.expected, result, "isCriticalEnvVar(%q)", tt.envVar)
		})
	}
}

func TestConfig_String(t *testing.T) {
	cfg := &Config{
		Exchanges: map[string]ExchangeConfig{
			"test": {
				APIKey:      Secret("my_super_secret_api_key"),
				SecretKey:   Secret("my_super_secret_secret_key"),
				GRPCAPIKeys: Secret("my_super_secret_grpc_keys"),
				GRPCAPIKey:  Secret("my_super_secret_grpc_key"),
			},
		},
	}
	output := cfg.String()

	// 1. Check for fixed mask
	assert.Contains(t, output, "********", "output should contain masked characters")

	// 2. Ensure full cleartext is GONE
	assert.NotContains(t, output, "my_super_secret_api_key", "output should NOT contain full API key")
	assert.NotContains(t, output, "my_super_secret_secret_key", "output should NOT contain full Secret key")
	assert.NotContains(t, output, "my_super_secret_grpc_keys", "output should NOT contain full GRPC API keys")
	assert.NotContains(t, output, "my_super_secret_grpc_key", "output should NOT contain full GRPC API key")

	// 3. Ensure partial content is NOT leaked
	assert.NotContains(t, output, "my_s", "output should NOT contain partial secret parts")
}
