package exchange

import (
	"context"
	"fmt"
	"io"
	"market_maker/internal/auth"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	"market_maker/pkg/pbu"
	"market_maker/pkg/telemetry"
	"math"
	"time"

	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

type RemoteExchange struct {
	conn          *grpc.ClientConn
	client        pb.ExchangeServiceClient
	healthClient  grpc_health_v1.HealthClient
	logger        core.ILogger
	name          string
	exType        pb.ExchangeType
	isUnified     bool
	reconciler    Reconciler // Optional reconciler for state validation
	apiKey        string     // API key for authentication
	priceDecimals int
	qtyDecimals   int
	baseAsset     string
	quoteAsset    string
}

// Reconciler interface for triggering reconciliation after stream reconnection
type Reconciler interface {
	TriggerImmediateReconciliation()
}

func NewRemoteExchange(address string, logger core.ILogger) (*RemoteExchange, error) {
	return newRemoteExchangeWithOptions(address, logger, "", "", "")
}

// NewRemoteExchangeWithTLS creates a new remote exchange client with TLS encryption
func NewRemoteExchangeWithTLS(address string, logger core.ILogger, certFile, serverName string) (*RemoteExchange, error) {
	return newRemoteExchangeWithOptions(address, logger, certFile, serverName, "")
}

// NewRemoteExchangeWithAuth creates a new remote exchange client with API key authentication
func NewRemoteExchangeWithAuth(address string, logger core.ILogger, apiKey string) (*RemoteExchange, error) {
	return newRemoteExchangeWithOptions(address, logger, "", "", apiKey)
}

// NewRemoteExchangeWithTLSAndAuth creates a new remote exchange client with TLS encryption and API key authentication
func NewRemoteExchangeWithTLSAndAuth(address string, logger core.ILogger, certFile, serverName, apiKey string) (*RemoteExchange, error) {
	return newRemoteExchangeWithOptions(address, logger, certFile, serverName, apiKey)
}

func newRemoteExchangeWithOptions(address string, logger core.ILogger, certFile, serverName, apiKey string) (*RemoteExchange, error) {
	// REQ-GRPC-010.3: Connection retry with exponential backoff
	// Retry policy: 10 attempts, backoff: 1s, 2s, 4s, 8s, 16s, 32s, 60s (max), 60s, 60s, 60s
	const (
		maxRetries      = 10
		initialBackoff  = 1 * time.Second
		maxBackoff      = 60 * time.Second
		backoffMultiple = 2.0
	)

	var conn *grpc.ClientConn
	var err error

	// Prepare transport credentials
	var dialOpts []grpc.DialOption
	if certFile != "" {
		// Use TLS credentials
		creds, err := credentials.NewClientTLSFromFile(certFile, serverName)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS cert from %s: %w", certFile, err)
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
		logger.Info("Using TLS for gRPC connection", "cert", certFile, "server_name", serverName)
	} else {
		// Use insecure credentials (for backward compatibility)
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		logger.Warn("Using insecure gRPC connection (plaintext)")
	}

	// Log authentication status
	if apiKey != "" {
		logger.Info("API key authentication enabled for gRPC client")
	} else {
		logger.Warn("No API key configured - server may reject requests if authentication is required")
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Info("Attempting to connect to exchange_connector",
			"address", address,
			"attempt", attempt,
			"max_attempts", maxRetries)

		// Calculate backoff for this attempt (used if connection fails)
		backoff := time.Duration(float64(initialBackoff) * math.Pow(backoffMultiple, float64(attempt-1)))
		if backoff > maxBackoff {
			backoff = maxBackoff
		}

		// Attempt connection
		conn, err = grpc.NewClient(address, dialOpts...)

		if err == nil {
			// Connection succeeded, verify service is available
			client := pb.NewExchangeServiceClient(conn)
			healthClient := grpc_health_v1.NewHealthClient(conn)

			verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
			nameResp, nameErr := client.GetName(verifyCtx, &pb.GetNameRequest{})
			typeResp, typeErr := client.GetType(verifyCtx, &pb.GetTypeRequest{})
			verifyCancel()

			if nameErr == nil && typeErr == nil {
				logger.Info("Successfully connected to exchange_connector",
					"exchange", nameResp.Name,
					"attempt", attempt)

				return &RemoteExchange{
					conn:         conn,
					client:       client,
					healthClient: healthClient,
					logger:       logger.WithField("exchange", "remote").WithField("remote_name", nameResp.Name),
					name:         nameResp.Name,
					exType:       typeResp.Type,
					isUnified:    typeResp.IsUnifiedMargin,
					apiKey:       apiKey,
				}, nil
			}

			// Service verification failed, close connection and retry
			logger.Warn("Connection succeeded but service verification failed",
				"name_error", nameErr,
				"type_error", typeErr)
			conn.Close()
			err = fmt.Errorf("service verification failed: name_err=%v, type_err=%v", nameErr, typeErr)
		}

		// Connection or verification failed
		if attempt < maxRetries {
			logger.Warn("Connection attempt failed, retrying...",
				"error", err,
				"retry_in", backoff,
				"attempt", attempt,
				"max_attempts", maxRetries)
			time.Sleep(backoff)
		}
	}

	// All retries exhausted
	return nil, fmt.Errorf("failed to connect to exchange_connector at %s after %d attempts: %w",
		address, maxRetries, err)
}

// addAuthMetadata adds API key to context if configured
func (r *RemoteExchange) addAuthMetadata(ctx context.Context) context.Context {
	if r.apiKey == "" {
		return ctx
	}
	md := metadata.Pairs(auth.MetadataKeyAPIKey, r.apiKey)
	return metadata.NewOutgoingContext(ctx, md)
}

func (r *RemoteExchange) CheckHealth(ctx context.Context) error {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "opensqt.market_maker.v1.ExchangeService",
	})
	if err != nil {
		return fmt.Errorf("remote health check failed: %w", err)
	}

	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("remote service status: %s", resp.Status)
	}

	return nil
}

func (r *RemoteExchange) IsUnifiedMargin() bool {
	return r.isUnified
}

func (r *RemoteExchange) GetName() string {
	return r.name
}

func (r *RemoteExchange) GetType() pb.ExchangeType {
	return r.exType
}

func (r *RemoteExchange) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	ctx = r.addAuthMetadata(ctx)
	start := time.Now()
	resp, err := r.client.PlaceOrder(ctx, req)
	duration := time.Since(start).Milliseconds()

	telemetry.GetGlobalMetrics().LatencyExchange.Record(ctx, float64(duration),
		metric.WithAttributes(
			attribute.String("exchange", r.name),
			attribute.String("operation", "PlaceOrder"),
		))

	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (r *RemoteExchange) BatchPlaceOrders(ctx context.Context, orders []*pb.PlaceOrderRequest) ([]*pb.Order, bool) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.BatchPlaceOrders(ctx, &pb.BatchPlaceOrdersRequest{Orders: orders})
	if err != nil {
		r.logger.Error("Remote BatchPlaceOrders failed", "error", err)
		return nil, false
	}

	return resp.Orders, resp.AllSuccess
}

func (r *RemoteExchange) CancelOrder(ctx context.Context, symbol string, orderID int64, useMargin bool) error {
	ctx = r.addAuthMetadata(ctx)
	_, err := r.client.CancelOrder(ctx, &pb.CancelOrderRequest{
		Symbol:    symbol,
		OrderId:   orderID,
		UseMargin: useMargin,
	})
	return err
}

func (r *RemoteExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64, useMargin bool) error {
	ctx = r.addAuthMetadata(ctx)
	_, err := r.client.BatchCancelOrders(ctx, &pb.BatchCancelOrdersRequest{
		Symbol:    symbol,
		OrderIds:  orderIDs,
		UseMargin: useMargin,
	})
	return err
}

func (r *RemoteExchange) CancelAllOrders(ctx context.Context, symbol string, useMargin bool) error {
	ctx = r.addAuthMetadata(ctx)
	_, err := r.client.CancelAllOrders(ctx, &pb.CancelAllOrdersRequest{
		Symbol:    symbol,
		UseMargin: useMargin,
	})
	return err
}

func (r *RemoteExchange) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string, useMargin bool) (*pb.Order, error) {
	ctx = r.addAuthMetadata(ctx)
	return r.client.GetOrder(ctx, &pb.GetOrderRequest{
		Symbol:        symbol,
		OrderId:       orderID,
		ClientOrderId: clientOrderID,
		UseMargin:     useMargin,
	})
}

func (r *RemoteExchange) GetOpenOrders(ctx context.Context, symbol string, useMargin bool) ([]*pb.Order, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetOpenOrders(ctx, &pb.GetOpenOrdersRequest{
		Symbol:    symbol,
		UseMargin: useMargin,
	})
	if err != nil {
		return nil, err
	}
	return resp.Orders, nil
}

func (r *RemoteExchange) GetAccount(ctx context.Context) (*pb.Account, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetAccount(ctx, &pb.GetAccountRequest{})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (r *RemoteExchange) GetPositions(ctx context.Context, symbol string) ([]*pb.Position, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetPositions(ctx, &pb.GetPositionsRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	return resp.Positions, nil
}

func (r *RemoteExchange) GetBalance(ctx context.Context, asset string) (decimal.Decimal, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetBalance(ctx, &pb.GetBalanceRequest{Asset: asset})
	if err != nil {
		return decimal.Zero, err
	}
	return pbu.ToGoDecimal(resp.Balance), nil
}

// SetReconciler sets the optional reconciler for triggering state validation after reconnection
func (r *RemoteExchange) SetReconciler(reconciler Reconciler) {
	r.reconciler = reconciler
}

// triggerReconciliation triggers immediate reconciliation if reconciler is set
func (r *RemoteExchange) triggerReconciliation() {
	if r.reconciler != nil {
		r.logger.Info("Triggering reconciliation after stream reconnection")
		r.reconciler.TriggerImmediateReconciliation()
	}
}

// min returns the minimum of two time.Duration values
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (r *RemoteExchange) StartOrderStream(ctx context.Context, callback func(update *pb.OrderUpdate)) error {
	return r.startStreamWithRetry(
		ctx,
		"OrderStream",
		func(ctx context.Context) (interface{}, error) {
			return r.client.SubscribeOrders(ctx, &pb.SubscribeOrdersRequest{})
		},
		func(stream interface{}) (interface{}, error) {
			return stream.(pb.ExchangeService_SubscribeOrdersClient).Recv()
		},
		func(msg interface{}) {
			callback(msg.(*pb.OrderUpdate))
		},
	)
}

func (r *RemoteExchange) StopOrderStream() error {
	return nil
}

func (r *RemoteExchange) StartPriceStream(ctx context.Context, symbols []string, callback func(change *pb.PriceChange)) error {
	return r.startStreamWithRetry(
		ctx,
		"PriceStream",
		func(ctx context.Context) (interface{}, error) {
			return r.client.SubscribePrice(ctx, &pb.SubscribePriceRequest{Symbols: symbols})
		},
		func(stream interface{}) (interface{}, error) {
			return stream.(pb.ExchangeService_SubscribePriceClient).Recv()
		},
		func(msg interface{}) {
			callback(msg.(*pb.PriceChange))
		},
	)
}

func (r *RemoteExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle *pb.Candle)) error {
	return r.startStreamWithRetry(
		ctx,
		"KlineStream",
		func(ctx context.Context) (interface{}, error) {
			return r.client.SubscribeKlines(ctx, &pb.SubscribeKlinesRequest{Symbols: symbols, Interval: interval})
		},
		func(stream interface{}) (interface{}, error) {
			return stream.(pb.ExchangeService_SubscribeKlinesClient).Recv()
		},
		func(msg interface{}) {
			callback(msg.(*pb.Candle))
		},
	)
}

func (r *RemoteExchange) StopKlineStream() error {
	return nil
}

func (r *RemoteExchange) StartAccountStream(ctx context.Context, callback func(account *pb.Account)) error {
	go func() {
		backoff := time.Second
		maxBackoff := 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				r.logger.Info("Account stream context cancelled, stopping")
				return
			default:
			}

			// Add authentication metadata to context
			authCtx := r.addAuthMetadata(ctx)

			// Establish stream
			stream, err := r.client.SubscribeAccount(authCtx, &pb.SubscribeAccountRequest{})
			if err != nil {
				r.logger.Error("Failed to subscribe to account, retrying...",
					"error", err, "backoff", backoff)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			r.logger.Info("Account stream connected")
			backoff = time.Second // Reset on success

			// Read loop
			for {
				account, err := stream.Recv()
				if err == io.EOF || err != nil {
					r.logger.Error("Account stream failed, reconnecting...", "error", err)
					break // Reconnect
				}

				callback(account)
			}
		}
	}()

	return nil
}

func (r *RemoteExchange) StartPositionStream(ctx context.Context, callback func(position *pb.Position)) error {
	go func() {
		backoff := time.Second
		maxBackoff := 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				r.logger.Info("Position stream context cancelled, stopping")
				return
			default:
			}

			// Add authentication metadata to context
			authCtx := r.addAuthMetadata(ctx)

			// Establish stream
			stream, err := r.client.SubscribePositions(authCtx, &pb.SubscribePositionsRequest{})
			if err != nil {
				r.logger.Error("Failed to subscribe to positions, retrying...",
					"error", err, "backoff", backoff)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			r.logger.Info("Position stream connected")
			backoff = time.Second // Reset on success

			// Trigger reconciliation on reconnect to catch missed updates
			r.triggerReconciliation()

			// Read loop
			for {
				position, err := stream.Recv()
				if err == io.EOF || err != nil {
					r.logger.Error("Position stream failed, reconnecting...", "error", err)
					break // Reconnect
				}

				callback(position)
			}
		}
	}()

	return nil
}

func (r *RemoteExchange) GetLatestPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetLatestPrice(ctx, &pb.GetLatestPriceRequest{Symbol: symbol})
	if err != nil {
		return decimal.Zero, err
	}
	return pbu.ToGoDecimal(resp.Price), nil
}

func (r *RemoteExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*pb.Candle, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetHistoricalKlines(ctx, &pb.GetHistoricalKlinesRequest{
		Symbol:   symbol,
		Interval: interval,
		Limit:    int32(limit),
	})
	if err != nil {
		return nil, err
	}

	return resp.Candles, nil
}

func (r *RemoteExchange) FetchExchangeInfo(ctx context.Context, symbol string) error {
	ctx = r.addAuthMetadata(ctx)
	_, err := r.client.FetchExchangeInfo(ctx, &pb.FetchExchangeInfoRequest{Symbol: symbol})
	if err != nil {
		return err
	}

	// Fetch detailed info to populate decimals
	info, err := r.client.GetSymbolInfo(ctx, &pb.GetSymbolInfoRequest{Symbol: symbol})
	if err != nil {
		r.logger.Warn("Failed to fetch symbol info after exchange info sync", "symbol", symbol, "error", err)
		return nil // Non-fatal
	}

	r.priceDecimals = int(info.PricePrecision)
	r.qtyDecimals = int(info.QuantityPrecision)
	r.baseAsset = info.BaseAsset
	r.quoteAsset = info.QuoteAsset

	return nil
}

func (r *RemoteExchange) GetSymbolInfo(ctx context.Context, symbol string) (*pb.SymbolInfo, error) {
	ctx = r.addAuthMetadata(ctx)
	return r.client.GetSymbolInfo(ctx, &pb.GetSymbolInfoRequest{Symbol: symbol})
}

func (r *RemoteExchange) GetSymbols(ctx context.Context) ([]string, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetSymbols(ctx, &pb.GetSymbolsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Symbols, nil
}

func (r *RemoteExchange) GetPriceDecimals() int {
	return r.priceDecimals
}

func (r *RemoteExchange) GetQuantityDecimals() int {
	return r.qtyDecimals
}

func (r *RemoteExchange) GetBaseAsset() string {
	return r.baseAsset
}

func (r *RemoteExchange) GetQuoteAsset() string {
	return r.quoteAsset
}

func (r *RemoteExchange) GetFundingRate(ctx context.Context, symbol string) (*pb.FundingRate, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetFundingRate(ctx, &pb.GetFundingRateRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (r *RemoteExchange) GetFundingRates(ctx context.Context) ([]*pb.FundingRate, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetFundingRates(ctx, &pb.GetFundingRatesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Rates, nil
}

func (r *RemoteExchange) GetHistoricalFundingRates(ctx context.Context, symbol string, limit int) ([]*pb.FundingRate, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetHistoricalFundingRates(ctx, &pb.GetHistoricalFundingRatesRequest{
		Symbol: symbol,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return resp.Rates, nil
}

func (r *RemoteExchange) GetTickers(ctx context.Context) ([]*pb.Ticker, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetTickers(ctx, &pb.GetTickersRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Tickers, nil
}

func (r *RemoteExchange) GetOpenInterest(ctx context.Context, symbol string) (decimal.Decimal, error) {
	ctx = r.addAuthMetadata(ctx)
	resp, err := r.client.GetOpenInterest(ctx, &pb.GetOpenInterestRequest{Symbol: symbol})
	if err != nil {
		return decimal.Zero, err
	}
	return pbu.ToGoDecimal(resp.OpenInterest), nil
}

func (r *RemoteExchange) StartFundingRateStream(ctx context.Context, symbol string, callback func(update *pb.FundingUpdate)) error {
	return r.startStreamWithRetry(
		ctx,
		"FundingRateStream",
		func(ctx context.Context) (interface{}, error) {
			return r.client.SubscribeFundingRate(ctx, &pb.SubscribeFundingRateRequest{Symbol: symbol})
		},
		func(stream interface{}) (interface{}, error) {
			return stream.(pb.ExchangeService_SubscribeFundingRateClient).Recv()
		},
		func(msg interface{}) {
			callback(msg.(*pb.FundingUpdate))
		},
	)
}
