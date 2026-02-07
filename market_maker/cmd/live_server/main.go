package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/exchange"
	"market_maker/internal/infrastructure/grpc/client"
	"market_maker/pkg/concurrency"
	pkgexchange "market_maker/pkg/exchange"
	"market_maker/pkg/liveserver"
	"market_maker/pkg/logging"
	"market_maker/pkg/telemetry"
)

var (
	// Version information (set via build flags)
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "configs/live_server.yaml", "Path to configuration file")
	port := flag.String("port", "", "Server port (overrides config)")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	// Show version if requested
	if *showVersion {
		fmt.Printf("live_server version %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Load configuration
	liveConfig, err := LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Override port if specified
	if *port != "" {
		liveConfig.Server.Port = *port
	}

	// Initialize logger
	logger, err := logging.NewZapLogger(liveConfig.GetLogLevel())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}

	logger.Info("Starting live_server",
		"version", version,
		"exchange", liveConfig.App.CurrentExchange,
		"symbol", liveConfig.Trading.Symbol,
		"port", liveConfig.Server.Port,
	)

	// Initialize metrics
	if err := telemetry.InitMetrics(); err != nil {
		logger.Warn("Failed to initialize metrics exporter", "error", err)
	} else {
		logger.Info("Metrics exporter initialized")
	}

	// Create exchange instance using existing factory
	exch, err := createExchange(liveConfig, logger)
	if err != nil {
		logger.Error("Failed to create exchange", "error", err)
		os.Exit(1)
	}

	// Check exchange health
	ctx := context.Background()
	if err := exch.CheckHealth(ctx); err != nil {
		logger.Warn("Exchange health check failed (will continue)", "error", err)
	} else {
		logger.Info("Exchange health check passed", "exchange", exch.GetName())
	}

	// Create WebSocket hub
	hub := liveserver.NewHub(logger)

	// Create Market Maker gRPC Client
	mmAddr := fmt.Sprintf("%s:%s", liveConfig.MarketMaker.GRPCHost, liveConfig.MarketMaker.GRPCPort)
	mmClient, err := client.NewMarketMakerClient(mmAddr, logger)
	if err != nil {
		logger.Error("Failed to connect to market_maker gRPC", "error", err, "addr", mmAddr)
		os.Exit(1)
	}
	defer mmClient.Close()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start hub in background
	go hub.Run(ctx)
	logger.Info("WebSocket hub started")

	// Create and start stream handlers
	streams := NewStreamHandlers(exch, mmClient, hub, liveConfig, logger)
	if err := streams.StartAll(ctx); err != nil {
		logger.Error("Failed to start streams", "error", err)
		os.Exit(1)
	}
	logger.Info("Stream handlers started",
		"symbol", liveConfig.Trading.Symbol,
		"interval", liveConfig.Trading.Interval,
	)

	// Create and configure HTTP/WebSocket server
	server := liveserver.NewServer(hub, logger, liveConfig.Server.AllowedOrigins)
	if liveConfig.Web.Directory != "" {
		server.SetStaticDir(liveConfig.Web.Directory)
	}

	// Start server in background
	go func() {
		logger.Info("Starting HTTP/WebSocket server", "port", liveConfig.Server.Port, "web_dir", liveConfig.Web.Directory)
		if err := server.Start(ctx, liveConfig.Server.Port); err != nil {
			logger.Error("Server error", "error", err)
			cancel()
		}
	}()

	// Log startup complete
	logger.Info("live_server is running",
		"websocket_url", fmt.Sprintf("ws://localhost%s/ws", liveConfig.Server.Port),
		"health_url", fmt.Sprintf("http://localhost%s/health", liveConfig.Server.Port),
		"web_url", fmt.Sprintf("http://localhost%s/", liveConfig.Server.Port),
	)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	<-sigChan
	logger.Info("Received shutdown signal, gracefully shutting down...")

	// Cancel context to trigger graceful shutdown
	cancel()

	// Stop server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10)
	defer shutdownCancel()

	if err := server.Stop(shutdownCtx); err != nil {
		logger.Error("Error during server shutdown", "error", err)
	}

	logger.Info("live_server stopped")
}

// createExchange creates an exchange instance by converting live_server config to internal config format
func createExchange(liveConfig *Config, logger core.ILogger) (pkgexchange.Exchange, error) {
	// Convert live_server config to internal config format
	internalConfig := &config.Config{
		Exchanges: make(map[string]config.ExchangeConfig),
	}

	// Copy exchange configurations
	for name, exchCfg := range liveConfig.Exchanges {
		internalConfig.Exchanges[name] = config.ExchangeConfig{
			APIKey:     config.Secret(exchCfg.APIKey),
			SecretKey:  config.Secret(exchCfg.SecretKey),
			Passphrase: config.Secret(exchCfg.Passphrase),
		}
	}

	// Create internal exchange via factory
	// Use a dedicated worker pool for stream processing
	pool := concurrency.NewWorkerPool(concurrency.PoolConfig{
		Name:        "LiveServerStreamPool",
		MaxWorkers:  10,
		MaxCapacity: 1000,
		NonBlocking: true,
	}, logger)

	internalExch, err := exchange.NewExchange(liveConfig.App.CurrentExchange, internalConfig, logger, pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create exchange: %w", err)
	}

	// Initialize exchange with symbol
	if err := internalExch.FetchExchangeInfo(context.Background(), liveConfig.Trading.Symbol); err != nil {
		logger.Warn("Failed to fetch exchange info", "error", err, "symbol", liveConfig.Trading.Symbol)
	}

	// Wrap with adapter to expose as pkg/exchange.Exchange
	return pkgexchange.NewAdapter(internalExch), nil
}
