package main

import (
	"context"
	"testing"
	"time"

	"market_maker/internal/config"
	"market_maker/internal/exchange"
	"market_maker/pkg/logging"
)

// TestCredentialValidation_ValidCredentials tests that CheckHealth succeeds with valid credentials
func TestCredentialValidation_ValidCredentials(t *testing.T) {
	t.Skip("Integration test - requires real exchange credentials")

	logger, _ := logging.NewZapLogger("INFO")

	cfg := config.DefaultConfig()
	cfg.App.CurrentExchange = "binance"

	binanceCfg := cfg.Exchanges["binance"]
	binanceCfg.APIKey = "your_real_api_key"
	binanceCfg.SecretKey = "your_real_secret_key"
	cfg.Exchanges["binance"] = binanceCfg

	exch, err := exchange.NewExchange("binance", cfg, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create exchange: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := exch.CheckHealth(ctx); err != nil {
		t.Errorf("CheckHealth should succeed with valid credentials, got: %v", err)
	}
}

// TestCredentialValidation_InvalidCredentials tests that CheckHealth fails with invalid credentials
func TestCredentialValidation_InvalidCredentials(t *testing.T) {
	t.Skip("skip: offline env cannot validate credentials")
	logger, _ := logging.NewZapLogger("INFO")

	cfg := config.DefaultConfig()
	cfg.App.CurrentExchange = "binance"

	binanceCfg := cfg.Exchanges["binance"]
	binanceCfg.APIKey = "invalid_key"
	binanceCfg.SecretKey = "invalid_secret"
	cfg.Exchanges["binance"] = binanceCfg

	exch, err := exchange.NewExchange("binance", cfg, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create exchange: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = exch.CheckHealth(ctx)
	if err == nil {
		t.Error("CheckHealth should fail with invalid credentials")
	}

	t.Logf("Got expected error: %v", err)
}

// TestCredentialValidation_EmptyCredentials tests that CheckHealth fails with empty credentials
func TestCredentialValidation_EmptyCredentials(t *testing.T) {
	t.Skip("skip: offline env cannot validate credentials")
	logger, _ := logging.NewZapLogger("INFO")

	cfg := config.DefaultConfig()
	cfg.App.CurrentExchange = "binance"

	binanceCfg := cfg.Exchanges["binance"]
	binanceCfg.APIKey = ""
	binanceCfg.SecretKey = ""
	cfg.Exchanges["binance"] = binanceCfg

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Should fail during NewExchange or CheckHealth
	exch, err := exchange.NewExchange("binance", cfg, logger, nil)
	if err != nil {
		// Expected - creation should fail with empty credentials
		t.Logf("Creation failed as expected: %v", err)
		return
	}

	// If creation succeeded, CheckHealth should fail
	err = exch.CheckHealth(ctx)
	if err == nil {
		t.Error("CheckHealth should fail with empty credentials")
	}
}
