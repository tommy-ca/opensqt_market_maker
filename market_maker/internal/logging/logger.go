// Package logging provides structured logging functionality
package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"market_maker/internal/core"
)

// Level represents log levels
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// String returns the string representation of a log level
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
		return "UNKNOWN"
	}
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

// Logger implements the ILogger interface
type Logger struct {
	level      Level
	writer     io.Writer
	fields     map[string]interface{}
	timeFormat string
}

// NewLogger creates a new logger instance
func NewLogger(level Level, writer io.Writer) *Logger {
	if writer == nil {
		writer = os.Stdout
	}

	return &Logger{
		level:      level,
		writer:     writer,
		fields:     make(map[string]interface{}),
		timeFormat: "2006-01-02 15:04:05.000",
	}
}

// NewLoggerFromString creates a logger from a level string
func NewLoggerFromString(levelStr string, writer io.Writer) (*Logger, error) {
	level, err := ParseLevel(levelStr)
	if err != nil {
		return nil, err
	}
	return NewLogger(level, writer), nil
}

// log writes a log entry
func (l *Logger) log(level Level, msg string, fields ...interface{}) {
	if level < l.level {
		return
	}

	// Build log entry
	entry := &logEntry{
		Timestamp: time.Now().Format(l.timeFormat),
		Level:     level.String(),
		Message:   msg,
		Fields:    make(map[string]interface{}),
	}

	// Add logger fields
	for k, v := range l.fields {
		entry.Fields[k] = v
	}

	// Add entry fields (key-value pairs)
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			key := fmt.Sprintf("%v", fields[i])
			entry.Fields[key] = fields[i+1]
		}
	}

	// Write entry
	fmt.Fprintln(l.writer, entry.String())
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields ...interface{}) {
	l.log(DebugLevel, msg, fields...)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields ...interface{}) {
	l.log(InfoLevel, msg, fields...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields ...interface{}) {
	l.log(WarnLevel, msg, fields...)
}

// Error logs an error message
func (l *Logger) Error(msg string, fields ...interface{}) {
	l.log(ErrorLevel, msg, fields...)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(msg string, fields ...interface{}) {
	l.log(FatalLevel, msg, fields...)
	os.Exit(1)
}

// WithField returns a logger with an additional field
func (l *Logger) WithField(key string, value interface{}) core.ILogger {
	newLogger := &Logger{
		level:      l.level,
		writer:     l.writer,
		fields:     make(map[string]interface{}),
		timeFormat: l.timeFormat,
	}

	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}

	// Add new field
	newLogger.fields[key] = value

	return newLogger
}

// WithFields returns a logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) core.ILogger {
	newLogger := &Logger{
		level:      l.level,
		writer:     l.writer,
		fields:     make(map[string]interface{}),
		timeFormat: l.timeFormat,
	}

	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}

	// Add new fields
	for k, v := range fields {
		newLogger.fields[k] = v
	}

	return newLogger
}

// logEntry represents a single log entry
type logEntry struct {
	Timestamp string
	Level     string
	Message   string
	Fields    map[string]interface{}
}

// String returns the string representation of a log entry
func (e *logEntry) String() string {
	var parts []string

	// Timestamp and level
	parts = append(parts, fmt.Sprintf("[%s] [%s]", e.Timestamp, e.Level))

	// Message
	parts = append(parts, e.Message)

	// Fields
	if len(e.Fields) > 0 {
		var fieldParts []string
		for k, v := range e.Fields {
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", k, v))
		}
		parts = append(parts, fmt.Sprintf("{%s}", strings.Join(fieldParts, ", ")))
	}

	return strings.Join(parts, " ")
}

// Global logger instance
var globalLogger core.ILogger

// init initializes the global logger
func init() {
	globalLogger = NewLogger(InfoLevel, os.Stdout)
}

// SetGlobalLogger sets the global logger instance
func SetGlobalLogger(logger core.ILogger) {
	globalLogger = logger
}

// GetGlobalLogger returns the global logger instance
func GetGlobalLogger() core.ILogger {
	return globalLogger
}

// Global convenience functions

// Debug logs a debug message using the global logger
func Debug(msg string, fields ...interface{}) {
	globalLogger.Debug(msg, fields...)
}

// Info logs an info message using the global logger
func Info(msg string, fields ...interface{}) {
	globalLogger.Info(msg, fields...)
}

// Warn logs a warning message using the global logger
func Warn(msg string, fields ...interface{}) {
	globalLogger.Warn(msg, fields...)
}

// Error logs an error message using the global logger
func Error(msg string, fields ...interface{}) {
	globalLogger.Error(msg, fields...)
}

// Fatal logs a fatal message using the global logger
func Fatal(msg string, fields ...interface{}) {
	globalLogger.Fatal(msg, fields...)
}
