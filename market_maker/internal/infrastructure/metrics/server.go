package metrics

import (
	"context"
	"fmt"
	"market_maker/internal/core"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server handles Prometheus metrics export
type Server struct {
	port   int
	logger core.ILogger
	srv    *http.Server
}

// NewServer creates a new metrics server
func NewServer(port int, logger core.ILogger) *Server {
	return &Server{
		port:   port,
		logger: logger.WithField("component", "metrics_server"),
	}
}

// Start starts the metrics HTTP server
func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	go func() {
		s.logger.Info("Starting Prometheus metrics server", "port", s.port)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Metrics server failed", "error", err)
		}
	}()
}

// Stop gracefully stops the metrics server
func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	s.logger.Info("Stopping metrics server")
	return s.srv.Shutdown(ctx)
}
