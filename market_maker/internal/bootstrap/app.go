package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// App represents the application context and holds core dependencies.
type App struct {
	Cfg    *Config
	Logger *slog.Logger
	// Add other core dependencies here, e.g.:
	// DB     *sql.DB
	// Redis  *redis.Client
}

// NewApp creates a new App instance by bootstrapping all dependencies.
func NewApp(configPath string) (*App, error) {
	// 1. Load Configuration
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	// 2. Initialize Logger
	logger := InitLogger(cfg)

	// 3. Initialize other dependencies (DB, etc.)
	// db, err := initDB(cfg.DB)
	// if err != nil { return nil, err }

	return &App{
		Cfg:    cfg,
		Logger: logger,
	}, nil
}

// Runner is an interface for components that can be run and stopped gracefully.
type Runner interface {
	Run(ctx context.Context) error
}

// Run orchestrates the application lifecycle, including signal handling.
func (a *App) Run(runners ...Runner) error {
	// Create a context that is canceled when a termination signal is received.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	a.Logger.Info("starting application")

	// Start all runners in the error group
	for _, runner := range runners {
		r := runner // capture loop variable
		g.Go(func() error {
			return r.Run(ctx)
		})
	}

	// Wait for all runners to finish or for a signal to be received
	if err := g.Wait(); err != nil {
		if ctx.Err() == nil {
			// The error was not caused by a signal (context cancellation)
			a.Logger.Error("application stopped with error", "error", err)
			return err
		}
	}

	a.Logger.Info("application shut down gracefully")
	return nil
}

// Shutdown handles manual cleanup tasks (closing DB connections, etc.)
func (a *App) Shutdown(timeout time.Duration) {
	a.Logger.Info("cleaning up resources", "timeout", timeout)

	// Create a timeout context for cleanup
	// _, cancel := context.WithTimeout(context.Background(), timeout)
	// defer cancel()

	// Perform cleanup:
	// a.DB.Close()
}
