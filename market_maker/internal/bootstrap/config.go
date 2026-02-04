package bootstrap

import (
	"fmt"
	"market_maker/internal/config"
	"os"
)

// Config is an alias for the project's main configuration struct
type Config = config.Config

// LoadConfig delegates to the project's config loader
func LoadConfig(path string) (*Config, error) {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, err
	}

	// Pre-flight Checks
	if err := checkPreFlight(cfg); err != nil {
		return nil, fmt.Errorf("pre-flight checks failed: %w", err)
	}

	return cfg, nil
}

// checkPreFlight performs environment checks beyond schema validation
func checkPreFlight(cfg *Config) error {
	// Check DatabaseURL if using DBOS
	if cfg.App.EngineType == "dbos" {
		if cfg.App.DatabaseURL == "" {
			return fmt.Errorf("database_url is required when engine_type is 'dbos'")
		}
	}

	// Check TLS file permissions (0600)
	// We check the current exchange if configured
	if cfg.App.CurrentExchange != "" && cfg.App.CurrentExchange != "mock" {
		if exch, ok := cfg.Exchanges[cfg.App.CurrentExchange]; ok {
			if exch.TLSKeyFile != "" {
				info, err := os.Stat(exch.TLSKeyFile)
				if err != nil {
					if os.IsNotExist(err) {
						return fmt.Errorf("tls_key_file not found: %s", exch.TLSKeyFile)
					}
					return err
				}
				// Allow 0600 (rw-------) or 0400 (r--------)
				mode := info.Mode().Perm()
				if mode&0077 != 0 {
					return fmt.Errorf("insecure permissions on tls_key_file %s: %04o (should be 0600)", exch.TLSKeyFile, mode)
				}
			}
		}
	}

	return nil
}
