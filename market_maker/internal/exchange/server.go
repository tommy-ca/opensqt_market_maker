package exchange

import (
	"context"
	"errors"
	"fmt"
	"market_maker/internal/auth"
	"market_maker/internal/config"
	"market_maker/internal/core"
	"market_maker/internal/pb"
	apperrors "market_maker/pkg/errors"
	"market_maker/pkg/pbu"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type ExchangeServer struct {
	pb.UnimplementedExchangeServiceServer
	exchange      core.IExchange
	logger        core.ILogger
	authValidator *auth.APIKeyValidator
}

func NewExchangeServer(exchange core.IExchange, logger core.ILogger) *ExchangeServer {
	return &ExchangeServer{
		exchange:      exchange,
		logger:        logger.WithField("component", "exchange_server"),
		authValidator: nil, // No authentication by default (backward compatibility)
	}
}

// NewExchangeServerWithAuth creates a new exchange server with API key authentication
func NewExchangeServerWithAuth(exchange core.IExchange, logger core.ILogger, apiKeys []string, rateLimit int) *ExchangeServer {
	return &ExchangeServer{
		exchange:      exchange,
		logger:        logger.WithField("component", "exchange_server"),
		authValidator: auth.NewAPIKeyValidator(apiKeys, rateLimit, logger),
	}
}

func (s *ExchangeServer) mapError(err error) error {
	if err == nil {
		return nil
	}

	// If already a gRPC status error, return it
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Map app errors to gRPC codes
	switch {
	case errors.Is(err, apperrors.ErrInsufficientFunds):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, apperrors.ErrOrderNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, apperrors.ErrInvalidOrderParameter), errors.Is(err, apperrors.ErrInvalidSymbol):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, apperrors.ErrAuthenticationFailed):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, apperrors.ErrRateLimitExceeded):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, apperrors.ErrSystemOverload), errors.Is(err, apperrors.ErrNetwork), errors.Is(err, apperrors.ErrExchangeMaintenance):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, apperrors.ErrOrderRejected):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, apperrors.ErrDuplicateOrder):
		return status.Error(codes.AlreadyExists, err.Error())
	}

	return status.Error(codes.Unknown, err.Error())
}

// Serve starts the gRPC server with unified TLS, Auth, and Metadata propagation.
func (s *ExchangeServer) Serve(port int, config *config.ExchangeConfig) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	var opts []grpc.ServerOption

	// 1. Transparent TLS Enforcement
	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(config.TLSCertFile, config.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("failed to load TLS credentials: %w", err)
		}
		opts = append(opts, grpc.Creds(creds))
		s.logger.Info("TLS encryption enabled", "cert", config.TLSCertFile)
	} else if string(config.GRPCAPIKeys) != "" || string(config.GRPCAPIKey) != "" {
		// Enforce TLS if Auth is present (Secure Default)
		return fmt.Errorf("refusing to start in INSECURE mode with Auth enabled. Configure TLS or remove API keys")
	} else {
		s.logger.Warn("Starting server in INSECURE mode (plaintext)")
	}

	// 2. Interceptor Chain (Auth)
	// We'll add metadata propagation in a future iteration or if needed by specific middleware.
	// For now, focus on Auth.
	var unaryInterceptors []grpc.UnaryServerInterceptor
	var streamInterceptors []grpc.StreamServerInterceptor

	// Initialize auth validator if keys present
	apiKeysStr := string(config.GRPCAPIKeys)
	if apiKeysStr == "" {
		apiKeysStr = string(config.GRPCAPIKey)
	}

	if apiKeysStr != "" {
		keys := strings.Split(apiKeysStr, ",")
		rateLimit := config.GRPCRateLimit
		if rateLimit <= 0 {
			rateLimit = 100 // Default
		}
		s.authValidator = auth.NewAPIKeyValidator(keys, rateLimit, s.logger)

		unaryInterceptors = append(unaryInterceptors, s.authValidator.UnaryServerInterceptor())
		streamInterceptors = append(streamInterceptors, s.authValidator.StreamServerInterceptor())
		s.logger.Info("API key authentication enabled")
	}

	if len(unaryInterceptors) > 0 {
		opts = append(opts,
			grpc.ChainUnaryInterceptor(unaryInterceptors...),
			grpc.ChainStreamInterceptor(streamInterceptors...),
		)
	}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterExchangeServiceServer(grpcServer, s)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opensqt.market_maker.v1.ExchangeService", grpc_health_v1.HealthCheckResponse_SERVING)

	s.logger.Info("Exchange gRPC server serving", "port", port)
	return grpcServer.Serve(lis)
}

func (s *ExchangeServer) GetName(ctx context.Context, req *pb.GetNameRequest) (*pb.GetNameResponse, error) {
	return &pb.GetNameResponse{Name: s.exchange.GetName()}, nil
}

func (s *ExchangeServer) GetType(ctx context.Context, req *pb.GetTypeRequest) (*pb.GetTypeResponse, error) {
	return &pb.GetTypeResponse{
		Type:            pb.ExchangeType_EXCHANGE_TYPE_FUTURES,
		IsUnifiedMargin: s.exchange.IsUnifiedMargin(),
	}, nil
}

func (s *ExchangeServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.Order, error) {
	order, err := s.exchange.PlaceOrder(ctx, req)
	if err != nil {
		return nil, s.mapError(err)
	}
	return order, nil
}

func (s *ExchangeServer) BatchPlaceOrders(ctx context.Context, req *pb.BatchPlaceOrdersRequest) (*pb.BatchPlaceOrdersResponse, error) {
	orders, allSuccess := s.exchange.BatchPlaceOrders(ctx, req.Orders)

	// Note: BatchPlaceOrders currently swallows individual errors and returns bool.
	// Future improvement: Return error details per order in response.
	// For now, if no orders returned and allSuccess false, maybe return error?
	// The interface returns ([]*Order, bool).

	return &pb.BatchPlaceOrdersResponse{
		Orders:     orders,
		AllSuccess: allSuccess,
	}, nil
}

func (s *ExchangeServer) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	err := s.exchange.CancelOrder(ctx, req.Symbol, req.OrderId, req.UseMargin)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.CancelOrderResponse{}, nil
}

func (s *ExchangeServer) BatchCancelOrders(ctx context.Context, req *pb.BatchCancelOrdersRequest) (*pb.BatchCancelOrdersResponse, error) {
	err := s.exchange.BatchCancelOrders(ctx, req.Symbol, req.OrderIds, req.UseMargin)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.BatchCancelOrdersResponse{}, nil
}

func (s *ExchangeServer) CancelAllOrders(ctx context.Context, req *pb.CancelAllOrdersRequest) (*pb.CancelAllOrdersResponse, error) {
	err := s.exchange.CancelAllOrders(ctx, req.Symbol, req.UseMargin)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.CancelAllOrdersResponse{}, nil
}

func (s *ExchangeServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.Order, error) {
	order, err := s.exchange.GetOrder(ctx, req.Symbol, req.OrderId, req.ClientOrderId, req.UseMargin)
	if err != nil {
		return nil, s.mapError(err)
	}
	return order, nil
}

func (s *ExchangeServer) GetOpenOrders(ctx context.Context, req *pb.GetOpenOrdersRequest) (*pb.GetOpenOrdersResponse, error) {
	orders, err := s.exchange.GetOpenOrders(ctx, req.Symbol, req.UseMargin)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetOpenOrdersResponse{Orders: orders}, nil
}

func (s *ExchangeServer) GetAccount(ctx context.Context, req *pb.GetAccountRequest) (*pb.Account, error) {
	acc, err := s.exchange.GetAccount(ctx)
	if err != nil {
		return nil, s.mapError(err)
	}
	return acc, nil
}

func (s *ExchangeServer) GetPositions(ctx context.Context, req *pb.GetPositionsRequest) (*pb.GetPositionsResponse, error) {
	pos, err := s.exchange.GetPositions(ctx, req.Symbol)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetPositionsResponse{Positions: pos}, nil
}

func (s *ExchangeServer) GetBalance(ctx context.Context, req *pb.GetBalanceRequest) (*pb.GetBalanceResponse, error) {
	bal, err := s.exchange.GetBalance(ctx, req.Asset)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetBalanceResponse{Balance: pbu.FromGoDecimal(bal)}, nil
}

func (s *ExchangeServer) GetLatestPrice(ctx context.Context, req *pb.GetLatestPriceRequest) (*pb.GetLatestPriceResponse, error) {
	price, err := s.exchange.GetLatestPrice(ctx, req.Symbol)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetLatestPriceResponse{Price: pbu.FromGoDecimal(price)}, nil
}

func (s *ExchangeServer) GetHistoricalKlines(ctx context.Context, req *pb.GetHistoricalKlinesRequest) (*pb.GetHistoricalKlinesResponse, error) {
	klines, err := s.exchange.GetHistoricalKlines(ctx, req.Symbol, req.Interval, int(req.Limit))
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetHistoricalKlinesResponse{Candles: klines}, nil
}

func (s *ExchangeServer) FetchExchangeInfo(ctx context.Context, req *pb.FetchExchangeInfoRequest) (*pb.FetchExchangeInfoResponse, error) {
	err := s.exchange.FetchExchangeInfo(ctx, req.Symbol)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.FetchExchangeInfoResponse{}, nil
}

func (s *ExchangeServer) GetSymbolInfo(ctx context.Context, req *pb.GetSymbolInfoRequest) (*pb.SymbolInfo, error) {
	info, err := s.exchange.GetSymbolInfo(ctx, req.Symbol)
	if err != nil {
		return nil, s.mapError(err)
	}
	return info, nil
}

func (s *ExchangeServer) GetSymbols(ctx context.Context, req *pb.GetSymbolsRequest) (*pb.GetSymbolsResponse, error) {
	symbols, err := s.exchange.GetSymbols(ctx)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetSymbolsResponse{Symbols: symbols}, nil
}

func (s *ExchangeServer) GetFundingRate(ctx context.Context, req *pb.GetFundingRateRequest) (*pb.FundingRate, error) {
	rate, err := s.exchange.GetFundingRate(ctx, req.Symbol)
	if err != nil {
		return nil, s.mapError(err)
	}
	return rate, nil
}

func (s *ExchangeServer) GetFundingRates(ctx context.Context, req *pb.GetFundingRatesRequest) (*pb.GetFundingRatesResponse, error) {
	rates, err := s.exchange.GetFundingRates(ctx)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetFundingRatesResponse{Rates: rates}, nil
}

func (s *ExchangeServer) GetHistoricalFundingRates(ctx context.Context, req *pb.GetHistoricalFundingRatesRequest) (*pb.GetHistoricalFundingRatesResponse, error) {
	rates, err := s.exchange.GetHistoricalFundingRates(ctx, req.Symbol, int(req.Limit))
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetHistoricalFundingRatesResponse{Rates: rates}, nil
}

func (s *ExchangeServer) GetTickers(ctx context.Context, req *pb.GetTickersRequest) (*pb.GetTickersResponse, error) {
	tickers, err := s.exchange.GetTickers(ctx)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetTickersResponse{Tickers: tickers}, nil
}

func (s *ExchangeServer) GetOpenInterest(ctx context.Context, req *pb.GetOpenInterestRequest) (*pb.GetOpenInterestResponse, error) {
	oi, err := s.exchange.GetOpenInterest(ctx, req.Symbol)
	if err != nil {
		return nil, s.mapError(err)
	}
	return &pb.GetOpenInterestResponse{OpenInterest: pbu.FromGoDecimal(oi)}, nil
}

func (s *ExchangeServer) SubscribeFundingRate(req *pb.SubscribeFundingRateRequest, stream pb.ExchangeService_SubscribeFundingRateServer) error {
	errCh := make(chan error, 1)
	err := s.exchange.StartFundingRateStream(stream.Context(), req.Symbol, func(update *pb.FundingUpdate) {
		if err := stream.Send(update); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	})
	if err != nil {
		return err
	}

	select {
	case <-stream.Context().Done():
		return stream.Context().Err()
	case err := <-errCh:
		return err
	}
}

func (s *ExchangeServer) SubscribePrice(req *pb.SubscribePriceRequest, stream pb.ExchangeService_SubscribePriceServer) error {
	errCh := make(chan error, 1)
	err := s.exchange.StartPriceStream(stream.Context(), req.Symbols, func(change *pb.PriceChange) {
		if err := stream.Send(change); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	})
	if err != nil {
		return err
	}

	select {
	case <-stream.Context().Done():
		return stream.Context().Err()
	case err := <-errCh:
		return err
	}
}

func (s *ExchangeServer) SubscribeOrders(req *pb.SubscribeOrdersRequest, stream pb.ExchangeService_SubscribeOrdersServer) error {
	errCh := make(chan error, 1)
	err := s.exchange.StartOrderStream(stream.Context(), func(update *pb.OrderUpdate) {
		if err := stream.Send(update); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	})
	if err != nil {
		return err
	}

	select {
	case <-stream.Context().Done():
		return stream.Context().Err()
	case err := <-errCh:
		return err
	}
}

func (s *ExchangeServer) SubscribeKlines(req *pb.SubscribeKlinesRequest, stream pb.ExchangeService_SubscribeKlinesServer) error {
	errCh := make(chan error, 1)
	err := s.exchange.StartKlineStream(stream.Context(), req.Symbols, req.Interval, func(candle *pb.Candle) {
		if err := stream.Send(candle); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	})
	if err != nil {
		return err
	}

	select {
	case <-stream.Context().Done():
		return stream.Context().Err()
	case err := <-errCh:
		return err
	}
}

func (s *ExchangeServer) SubscribeAccount(req *pb.SubscribeAccountRequest, stream pb.ExchangeService_SubscribeAccountServer) error {
	errCh := make(chan error, 1)
	err := s.exchange.StartAccountStream(stream.Context(), func(account *pb.Account) {
		if err := stream.Send(account); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	})
	if err != nil {
		return err
	}

	select {
	case <-stream.Context().Done():
		return stream.Context().Err()
	case err := <-errCh:
		return err
	}
}

func (s *ExchangeServer) SubscribePositions(req *pb.SubscribePositionsRequest, stream pb.ExchangeService_SubscribePositionsServer) error {
	errCh := make(chan error, 1)
	err := s.exchange.StartPositionStream(stream.Context(), func(position *pb.Position) {
		// Filter by symbol if requested
		if req.Symbol != "" && position.Symbol != req.Symbol {
			return
		}

		if err := stream.Send(position); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	})
	if err != nil {
		return err
	}

	select {
	case <-stream.Context().Done():
		return stream.Context().Err()
	case err := <-errCh:
		return err
	}
}
