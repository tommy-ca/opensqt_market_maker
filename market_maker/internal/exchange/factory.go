// Package exchange provides exchange implementations
package exchange

import (
	"fmt"
	"strings"

	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/exchange/binance"
	"market_maker/internal/exchange/bitget"
	"market_maker/internal/exchange/bybit"
	"market_maker/internal/exchange/gate"
	"market_maker/internal/exchange/okx"
	"market_maker/pkg/concurrency"
)

// NewExchange creates a new exchange instance based on configuration
func NewExchange(exchangeName string, cfg *config.Config, logger core.ILogger, pool *concurrency.WorkerPool) (core.IExchange, error) {
	exchangeConfig, exists := cfg.Exchanges[exchangeName]
	if !exists {
		return nil, fmt.Errorf("configuration not found for exchange: %s", exchangeName)
	}

	switch strings.ToLower(exchangeName) {
	case "binance":
		return binance.NewBinanceExchange(&exchangeConfig, logger, pool), nil
	case "bitget":
		return bitget.NewBitgetExchange(&exchangeConfig, logger), nil
	case "gate":
		return gate.NewGateExchange(&exchangeConfig, logger), nil
	case "okx":
		return okx.NewOKXExchange(&exchangeConfig, logger), nil
	case "bybit":
		return bybit.NewBybitExchange(&exchangeConfig, logger), nil
	case "remote":
		// For remote exchange, BaseURL is treated as the gRPC server address
		if exchangeConfig.BaseURL == "" {
			return nil, fmt.Errorf("base_url is required for remote exchange (e.g. localhost:50051)")
		}
		// Use TLS if certificate is configured
		if exchangeConfig.TLSCertFile != "" {
			logger.Info("Creating remote exchange with TLS",
				"address", exchangeConfig.BaseURL,
				"cert", exchangeConfig.TLSCertFile,
				"server_name", exchangeConfig.TLSServerName)

			if exchangeConfig.GRPCAPIKey != "" {
				return NewRemoteExchangeWithTLSAndAuth(
					exchangeConfig.BaseURL,
					logger,
					exchangeConfig.TLSCertFile,
					exchangeConfig.TLSServerName,
					exchangeConfig.GRPCAPIKey,
				)
			}
			return NewRemoteExchangeWithTLS(
				exchangeConfig.BaseURL,
				logger,
				exchangeConfig.TLSCertFile,
				exchangeConfig.TLSServerName,
			)
		}

		if exchangeConfig.GRPCAPIKey != "" {
			logger.Warn("Creating remote exchange with Auth but without TLS (insecure)", "address", exchangeConfig.BaseURL)
			return NewRemoteExchangeWithAuth(exchangeConfig.BaseURL, logger, exchangeConfig.GRPCAPIKey)
		}

		logger.Warn("Creating remote exchange without TLS or Auth (insecure)", "address", exchangeConfig.BaseURL)
		return NewRemoteExchange(exchangeConfig.BaseURL, logger)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchangeName)
	}
}
