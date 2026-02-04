package bootstrap

import (
	"log/slog"
	"os"
)

// InitLogger initializes the global slog logger based on configuration.
func InitLogger(cfg *Config) *slog.Logger {
	var handler slog.Handler

	// Default to INFO if not set
	level := slog.LevelInfo
	switch cfg.System.LogLevel {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	// Always use TextHandler for now, can add JSON later if needed
	handler = slog.NewTextHandler(os.Stdout, opts)

	logger := slog.New(handler).With(
		"symbol", cfg.Trading.Symbol,
	)

	// Set as global logger
	slog.SetDefault(logger)

	return logger
}
