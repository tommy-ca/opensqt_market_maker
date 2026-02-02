package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/exchange"
	"market_maker/internal/mock"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/logging"
)

var (
	configFile   = flag.String("config", "configs/config.yaml", "Path to configuration file")
	exchangeFlag = flag.String("exchange", "", "Exchange name (e.g., binance, okx, bybit)")
	portFlag     = flag.Int("port", 50051, "gRPC server port")
)

func main() {
	flag.Parse()

	// 1. Initialize Logger
	logger, _ := logging.NewZapLogger("INFO")

	// 2. Override flags with Env Vars if present
	if envExchange := os.Getenv("EXCHANGE"); envExchange != "" {
		*exchangeFlag = envExchange
	}
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			*portFlag = p
		}
	}
	if envConfig := os.Getenv("CONFIG_FILE"); envConfig != "" {
		*configFile = envConfig
	}

	if *exchangeFlag == "" {
		logger.Fatal("Exchange name must be provided via --exchange or EXCHANGE env var")
	}

	logger.Info("Starting Unified gRPC Exchange Connector...",
		"exchange", *exchangeFlag,
		"port", *portFlag)

	// 3. Load Configuration (use default if not found)
	// Note: exchange_connector uses minimal config - mainly for credentials
	cfg := config.DefaultConfig()
	if _, err := os.Stat(*configFile); err == nil {
		loadedCfg, err := config.LoadConfig(*configFile)
		if err != nil {
			logger.Warn("Failed to load config file, using defaults", "error", err)
		} else {
			cfg = loadedCfg
		}
	} else {
		logger.Info("Config file not found, using default configuration")
	}

	// Override current_exchange to match the connector's exchange
	cfg.App.CurrentExchange = *exchangeFlag

	// 4. Initialize Exchange Adapter via Factory
	// Initialize Worker Pool for Stream Handling
	streamPool := concurrency.NewWorkerPool(concurrency.PoolConfig{
		Name:        "StreamPool",
		MaxWorkers:  10,   // Default for standalone connector
		MaxCapacity: 1000, // Reasonable buffer
		NonBlocking: true,
	}, logger)
	defer streamPool.Stop()

	var exch core.IExchange
	if *exchangeFlag == "mock" {
		exch = mock.NewMockExchange("mock")
		logger.Info("Using MOCK exchange for testing")
	} else {
		var err error
		exch, err = exchange.NewExchange(*exchangeFlag, cfg, logger, streamPool)
		if err != nil {
			logger.Fatal("Failed to initialize exchange adapter", "error", err)
		}
	}

	// 4b. Validate credentials before serving (REQ-GRPC-002.3)
	logger.Info("Validating exchange credentials...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := exch.CheckHealth(ctx); err != nil {
		logger.Fatal("Credential validation failed - please check your API keys",
			"error", err,
			"exchange", *exchangeFlag)
	}
	logger.Info("Credentials validated successfully")

	// 5. Start gRPC Server with TLS
	server := exchange.NewExchangeServer(exch, logger)

	// Check if TLS certificates are configured
	tlsCertFile := ""
	tlsKeyFile := ""
	if currentExchCfg, err := cfg.GetCurrentExchangeConfig(); err == nil {
		tlsCertFile = currentExchCfg.TLSCertFile
		tlsKeyFile = currentExchCfg.TLSKeyFile
	}

	errCh := make(chan error, 1)
	go func() {
		var err error
		if tlsCertFile != "" && tlsKeyFile != "" {
			// Start with TLS encryption (SECURE)
			logger.Info("Starting gRPC server with TLS encryption",
				"cert", tlsCertFile,
				"key", tlsKeyFile)
			err = server.StartWithTLS(*portFlag, tlsCertFile, tlsKeyFile)
		} else {
			// Start without TLS (INSECURE - backward compatibility only)
			logger.Warn("⚠️  Starting gRPC server WITHOUT TLS encryption (INSECURE)",
				"reason", "No TLS certificates configured")
			logger.Warn("⚠️  API keys and trading data will be transmitted in PLAINTEXT")
			logger.Warn("⚠️  Configure tls_cert_file and tls_key_file in config.yaml to enable TLS")
			err = server.Start(*portFlag)
		}
		if err != nil {
			errCh <- err
		}
	}()

	// 6. Wait for Signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		logger.Fatal("gRPC server failed", "error", err)
	case sig := <-sigChan:
		logger.Info("Received signal, shutting down", "signal", sig)
	}
}
