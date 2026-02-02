// Package logging provides structured logging functionality using Zap and OpenTelemetry bridge
package logging

import (
	"fmt"
	"market_maker/internal/core"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/log/global"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapLogger implements the ILogger interface using zap.Logger
type ZapLogger struct {
	logger *zap.Logger
}

// NewZapLogger creates a new ZapLogger instance
func NewZapLogger(levelStr string) (*ZapLogger, error) {
	var zapLevel zapcore.Level
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		zapLevel = zap.DebugLevel
	case "INFO":
		zapLevel = zap.InfoLevel
	case "WARN":
		zapLevel = zap.WarnLevel
	case "ERROR":
		zapLevel = zap.ErrorLevel
	case "FATAL":
		zapLevel = zap.FatalLevel
	default:
		zapLevel = zap.InfoLevel
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	core_ := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)

	// Add OTel bridge
	otelCore := otelzap.NewCore("market_maker", otelzap.WithLoggerProvider(global.GetLoggerProvider()))
	combinedCore := zapcore.NewTee(core_, otelCore)

	logger := zap.New(combinedCore, zap.AddCaller(), zap.AddCallerSkip(1))

	return &ZapLogger{
		logger: logger,
	}, nil
}

// Level represents log levels
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case FatalLevel:
		return "FATAL"
	default:
		return "INFO"
	}
}

// NewLogger creates a new Zap logger for backward compatibility
func NewLogger(level Level, _ interface{}) core.ILogger {
	logger, _ := NewZapLogger(level.String())
	return logger
}

// NewLoggerFromString creates a logger from a level string for backward compatibility
func NewLoggerFromString(levelStr string, _ interface{}) (core.ILogger, error) {
	return NewZapLogger(levelStr)
}

// ParseLevel parses a log level string
func ParseLevel(level string) (Level, error) {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return DebugLevel, nil
	case "INFO":
		return InfoLevel, nil
	case "WARN":
		return WarnLevel, nil
	case "ERROR":
		return ErrorLevel, nil
	case "FATAL":
		return FatalLevel, nil
	default:
		return InfoLevel, fmt.Errorf("invalid log level: %s", level)
	}
}

// convertToZapFields converts variadic interface fields to zap.Field
func (l *ZapLogger) convertToZapFields(fields []interface{}) []zap.Field {
	zapFields := make([]zap.Field, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			key, ok := fields[i].(string)
			if !ok {
				key = fmt.Sprintf("%v", fields[i])
			}
			zapFields = append(zapFields, zap.Any(key, fields[i+1]))
		}
	}
	return zapFields
}

func (l *ZapLogger) Debug(msg string, fields ...interface{}) {
	l.logger.Debug(msg, l.convertToZapFields(fields)...)
}

func (l *ZapLogger) Info(msg string, fields ...interface{}) {
	l.logger.Info(msg, l.convertToZapFields(fields)...)
}

func (l *ZapLogger) Warn(msg string, fields ...interface{}) {
	l.logger.Warn(msg, l.convertToZapFields(fields)...)
}

func (l *ZapLogger) Error(msg string, fields ...interface{}) {
	l.logger.Error(msg, l.convertToZapFields(fields)...)
}

func (l *ZapLogger) Fatal(msg string, fields ...interface{}) {
	l.logger.Fatal(msg, l.convertToZapFields(fields)...)
}

func (l *ZapLogger) WithField(key string, value interface{}) core.ILogger {
	return &ZapLogger{
		logger: l.logger.With(zap.Any(key, value)),
	}
}

func (l *ZapLogger) WithFields(fields map[string]interface{}) core.ILogger {
	zapFields := make([]zap.Field, 0, len(fields))
	for k, v := range fields {
		zapFields = append(zapFields, zap.Any(k, v))
	}
	return &ZapLogger{
		logger: l.logger.With(zapFields...),
	}
}

// Sync flushes any buffered log entries
func (l *ZapLogger) Sync() error {
	return l.logger.Sync()
}

// Global logger instance
var globalLogger core.ILogger

func init() {
	// Default logger if not initialized
	logger, _ := NewZapLogger("INFO")
	globalLogger = logger
}

// SetGlobalLogger sets the global logger instance
func SetGlobalLogger(logger core.ILogger) {
	globalLogger = logger
}

// GetGlobalLogger returns the global logger instance
func GetGlobalLogger() core.ILogger {
	return globalLogger
}

// Debug - Global convenience functions
func Debug(msg string, fields ...interface{}) { globalLogger.Debug(msg, fields...) }

// Info - Global convenience functions
func Info(msg string, fields ...interface{}) { globalLogger.Info(msg, fields...) }

// Warn - Global convenience functions
func Warn(msg string, fields ...interface{}) { globalLogger.Warn(msg, fields...) }

// Error - Global convenience functions
func Error(msg string, fields ...interface{}) { globalLogger.Error(msg, fields...) }

// Fatal - Global convenience functions
func Fatal(msg string, fields ...interface{}) { globalLogger.Fatal(msg, fields...) }
