package server

import (
	"context"
	"encoding/json"
	"market_maker/internal/core"
	"market_maker/pkg/telemetry"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HealthServer struct {
	port   string
	logger core.ILogger
	srv    *http.Server
	mu     sync.RWMutex
	status map[string]string
	hm     core.IHealthMonitor
}

func NewHealthServer(port string, logger core.ILogger, hm core.IHealthMonitor) *HealthServer {
	return &HealthServer{
		port:   port,
		logger: logger.WithField("component", "health_server"),
		status: make(map[string]string),
		hm:     hm,
	}
}

func (s *HealthServer) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.Handle("/metrics", promhttp.Handler())

	s.srv = &http.Server{
		Addr:    ":" + s.port,
		Handler: mux,
	}

	go func() {
		s.logger.Info("Starting health server", "port", s.port)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Health server failed", "error", err)
		}
	}()
}

func (s *HealthServer) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *HealthServer) UpdateStatus(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[key] = value
}

func (s *HealthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	metrics := telemetry.GetGlobalMetrics()

	health := map[string]interface{}{
		"status": "ok",
		"time":   time.Now(),
		"metrics": map[string]interface{}{
			"active_orders":  metrics.GetActiveOrders(),
			"unrealized_pnl": metrics.GetUnrealizedPnL(),
			"position_size":  metrics.GetPositionSize(),
		},
	}

	if s.hm != nil {
		health["components"] = s.hm.GetStatus()
		if !s.hm.IsHealthy() {
			health["status"] = "unhealthy"
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	} else {
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (s *HealthServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	mergedStatus := make(map[string]string)
	for k, v := range s.status {
		mergedStatus[k] = v
	}
	s.mu.RUnlock()

	if s.hm != nil {
		compStatus := s.hm.GetStatus()
		for k, v := range compStatus {
			mergedStatus[k] = v
		}
	}

	data, _ := json.Marshal(mergedStatus)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
