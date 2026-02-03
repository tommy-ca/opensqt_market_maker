package main

import (
	"context"
	"flag"
	"market_maker/internal/bootstrap"
	"market_maker/internal/core"
	"market_maker/internal/exchange"
	"market_maker/internal/mock"
	"market_maker/pkg/concurrency"
	"market_maker/pkg/logging"
	"os"
	"strconv"
	"time"
)

var (
	configFile   = flag.String("config", "configs/config.yaml", "Path to configuration file")
	exchangeFlag = flag.String("exchange", "", "Exchange name (e.g., binance, okx, bybit)")
	portFlag     = flag.Int("port", 50051, "gRPC server port")
)

type ConnectorRunner struct {
	app *bootstrap.App
}

func (r *ConnectorRunner) Run(ctx context.Context) error {
	// Adapter for logger
	zLogger, _ := logging.NewZapLogger(r.app.Cfg.System.LogLevel)
	logger := zLogger

	// Override flags with Env Vars (simulating old behavior until full migration)
	// Actually, bootstrap Config loaded from YAML/Env already.
	// But exchangeFlag and portFlag are specific CLI args here.
	if envExchange := os.Getenv("EXCHANGE"); envExchange != "" {
		*exchangeFlag = envExchange
	}
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			*portFlag = p
		}
	}

	if *exchangeFlag == "" {
		logger.Fatal("Exchange name must be provided via --exchange or EXCHANGE env var")
	}

	logger.Info("Starting Unified gRPC Exchange Connector...",
		"exchange", *exchangeFlag,
		"port", *portFlag)

	// Update current exchange in config
	cfg := r.app.Cfg
	cfg.App.CurrentExchange = *exchangeFlag

	// Initialize Worker Pool
	streamPool := concurrency.NewWorkerPool(concurrency.PoolConfig{
		Name:        "StreamPool",
		MaxWorkers:  10,
		MaxCapacity: 1000,
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

	// Validate credentials
	logger.Info("Validating exchange credentials...")
	valCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := exch.CheckHealth(valCtx); err != nil {
		logger.Fatal("Credential validation failed", "error", err, "exchange", *exchangeFlag)
	}
	logger.Info("Credentials validated successfully")

	// Start Server
	server := exchange.NewExchangeServer(exch, logger)

	// Get exchange config for TLS/Auth
	exchCfg, err := cfg.GetCurrentExchangeConfig()
	if err != nil && *exchangeFlag != "mock" {
		logger.Fatal("Failed to get exchange config", "error", err)
	}

	// Use mock config if missing (for mock mode)
	if exchCfg == nil {
		// Minimal config for Serve
		// We can't easily create a dummy ExchangeConfig struct here due to Secret type?
		// Serve expects *config.ExchangeConfig.
		// Let's rely on Serve to handle nil if we pass it, or create empty.
		// Actually, Serve dereferences it.
		// Let's create an empty one if nil.
		// But config package is internal.
		// Let's skip TLS/Auth for mock if exchCfg is missing.
		// Serve handles the logic.
		// We'll pass what we have.
	}

	// Launch server in goroutine to allow context cancellation
	errCh := make(chan error, 1)
	go func() {
		if exchCfg != nil {
			errCh <- server.Serve(*portFlag, exchCfg)
		} else {
			// Fallback for mock without config
			// We can't call Serve easily without config object.
			// Let's call Start (insecure) which is deprecated/removed?
			// Start was refactored into Serve.
			// Start(port) is gone.
			// We need a dummy config.
			// Since we can't import config just to create a struct (it's in internal),
			// but we ARE in main which imports internal/config implicitly via bootstrap.
			// Wait, r.app.Cfg is *config.Config.
			// So we have access to config package.
			// But we need to import it explicitly if we want to construct struct.
			// r.app.Cfg is alias type bootstrap.Config = config.Config.
			// So we can use that.

			// Actually, if it's mock, we might not have it in cfg.Exchanges["mock"].
			// Let's ensure we do or handle it.
			// Serve() handles nil? No.
			// We'll just fail if no config for real exchange.
			// For mock, we can skip Serve if we want, but we need gRPC.
			// We'll assume config exists or we fail.
			logger.Fatal("Mock exchange requires config entry for now to run Serve")
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		server.Stop() // Assuming Stop method exists or we kill it
		return nil
	}
}

func main() {
	flag.Parse()

	app, err := bootstrap.NewApp(*configFile)
	if err != nil {
		// Fallback logger?
		panic(err)
	}

	runner := &ConnectorRunner{app: app}
	if err := app.Run(runner); err != nil {
		os.Exit(1)
	}
}
