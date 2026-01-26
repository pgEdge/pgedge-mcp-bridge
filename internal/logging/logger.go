// Package logging provides structured logging with configurable levels and formats
// for the MCP HTTP bridge.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// Level represents the logging level.
type Level int

const (
	// LevelDebug is the most verbose logging level.
	LevelDebug Level = iota
	// LevelInfo is the default logging level for informational messages.
	LevelInfo
	// LevelWarn is for warning messages that indicate potential issues.
	LevelWarn
	// LevelError is for error messages that indicate failures.
	LevelError
)

// String returns the string representation of the log level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON implements json.Marshaler for Level.
func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.ToLower(l.String()))
}

// ParseLevel parses a string into a Level.
// Accepts case-insensitive level names: debug, info, warn, warning, error.
// Returns an error if the level string is not recognized.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %q", s)
	}
}

// Format represents the output format for log messages.
type Format int

const (
	// FormatText outputs logs in human-readable text format.
	FormatText Format = iota
	// FormatJSON outputs logs in JSON format.
	FormatJSON
)

// parseFormat parses a string into a Format.
func parseFormat(s string) Format {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return FormatJSON
	default:
		return FormatText
	}
}

// Logger provides structured logging with configurable levels and formats.
// It is safe for concurrent use by multiple goroutines.
type Logger struct {
	mu     sync.Mutex
	level  Level
	format Format
	output io.Writer
	fields map[string]any
	file   *os.File // non-nil if we opened a file that needs closing
}

// NewLogger creates a new Logger based on the provided configuration.
// The config specifies the log level, format (json/text), and output destination.
// Output can be "stdout", "stderr", or a file path.
// Returns an error if the configuration is invalid or the output file cannot be opened.
func NewLogger(cfg config.LogConfig) (*Logger, error) {
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		// Default to info level if parsing fails, but log a warning
		level = LevelInfo
	}

	format := parseFormat(cfg.Format)

	output, file, err := parseOutput(cfg.Output)
	if err != nil {
		return nil, fmt.Errorf("configuring log output: %w", err)
	}

	return &Logger{
		level:  level,
		format: format,
		output: output,
		fields: make(map[string]any),
		file:   file,
	}, nil
}

// parseOutput parses the output configuration and returns the appropriate writer.
// Returns the writer, an optional file handle (if a file was opened), and any error.
func parseOutput(s string) (io.Writer, *os.File, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "stdout":
		return os.Stdout, nil, nil
	case "stderr":
		return os.Stderr, nil, nil
	default:
		// Treat as file path
		f, err := os.OpenFile(s, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("opening log file %q: %w", s, err)
		}
		return f, f, nil
	}
}

// Close closes any resources held by the logger, such as open file handles.
// It is safe to call Close multiple times.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// WithFields returns a new Logger with the provided fields added to the context.
// The returned logger shares the same output and level configuration as the parent.
// Fields are included in every log message from the returned logger.
func (l *Logger) WithFields(fields map[string]any) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Create new logger with merged fields
	newFields := make(map[string]any, len(l.fields)+len(fields))
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}

	return &Logger{
		level:  l.level,
		format: l.format,
		output: l.output,
		fields: newFields,
		file:   nil, // Don't transfer file ownership to child loggers
	}
}

// WithError returns a new Logger with an "error" field set to the provided error.
// This is a convenience method for adding error context to log messages.
func (l *Logger) WithError(err error) *Logger {
	if err == nil {
		return l
	}
	return l.WithFields(map[string]any{"error": err.Error()})
}

// Debug logs a message at debug level with optional key-value pairs.
// Key-value pairs should be provided as alternating keys (strings) and values.
func (l *Logger) Debug(msg string, keyvals ...any) {
	l.log(LevelDebug, msg, keyvals...)
}

// Info logs a message at info level with optional key-value pairs.
// Key-value pairs should be provided as alternating keys (strings) and values.
func (l *Logger) Info(msg string, keyvals ...any) {
	l.log(LevelInfo, msg, keyvals...)
}

// Warn logs a message at warn level with optional key-value pairs.
// Key-value pairs should be provided as alternating keys (strings) and values.
func (l *Logger) Warn(msg string, keyvals ...any) {
	l.log(LevelWarn, msg, keyvals...)
}

// Error logs a message at error level with optional key-value pairs.
// Key-value pairs should be provided as alternating keys (strings) and values.
func (l *Logger) Error(msg string, keyvals ...any) {
	l.log(LevelError, msg, keyvals...)
}

// log is the internal logging method that handles level filtering, formatting,
// and writing to the output.
func (l *Logger) log(level Level, msg string, keyvals ...any) {
	// Level filtering
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Merge context fields with provided key-value pairs
	fields := l.mergeFields(keyvals...)

	timestamp := time.Now().UTC().Format(time.RFC3339)

	var output string
	if l.format == FormatJSON {
		output = l.formatJSON(timestamp, level, msg, fields)
	} else {
		output = l.formatText(timestamp, level, msg, fields)
	}

	// Write to output
	fmt.Fprintln(l.output, output)
}

// mergeFields merges the logger's context fields with the provided key-value pairs.
func (l *Logger) mergeFields(keyvals ...any) map[string]any {
	fields := make(map[string]any, len(l.fields)+len(keyvals)/2)

	// Copy context fields
	for k, v := range l.fields {
		fields[k] = v
	}

	// Add key-value pairs
	for i := 0; i < len(keyvals)-1; i += 2 {
		key, ok := keyvals[i].(string)
		if !ok {
			key = fmt.Sprintf("%v", keyvals[i])
		}
		fields[key] = keyvals[i+1]
	}

	// Handle odd number of keyvals (missing value for last key)
	if len(keyvals)%2 != 0 {
		key, ok := keyvals[len(keyvals)-1].(string)
		if !ok {
			key = fmt.Sprintf("%v", keyvals[len(keyvals)-1])
		}
		fields[key] = "MISSING_VALUE"
	}

	return fields
}

// formatJSON formats a log entry as JSON.
func (l *Logger) formatJSON(timestamp string, level Level, msg string, fields map[string]any) string {
	entry := make(map[string]any)
	entry["time"] = timestamp
	entry["level"] = strings.ToLower(level.String())
	entry["msg"] = msg

	// Add fields directly to the root object
	for k, v := range fields {
		entry[k] = v
	}

	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback to basic format if JSON marshaling fails
		return fmt.Sprintf(`{"time":%q,"level":%q,"msg":%q,"error":"json marshal failed"}`,
			timestamp, strings.ToLower(level.String()), msg)
	}

	return string(data)
}

// formatText formats a log entry as human-readable text.
func (l *Logger) formatText(timestamp string, level Level, msg string, fields map[string]any) string {
	var sb strings.Builder

	sb.WriteString(timestamp)
	sb.WriteString(" ")
	sb.WriteString(level.String())
	sb.WriteString(" ")
	sb.WriteString(msg)

	// Add fields as key=value pairs
	for k, v := range fields {
		sb.WriteString(" ")
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(formatValue(v))
	}

	return sb.String()
}

// formatValue formats a value for text output.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		// Quote strings that contain spaces
		if strings.ContainsAny(val, " \t\n") {
			return fmt.Sprintf("%q", val)
		}
		return val
	case error:
		return val.Error()
	default:
		return fmt.Sprintf("%v", val)
	}
}

// SetLevel changes the logging level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current logging level.
func (l *Logger) GetLevel() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

// Global logger instance
var (
	globalLogger     *Logger
	globalLoggerOnce sync.Once
	globalLoggerMu   sync.RWMutex
)

// initDefaultLogger initializes the global logger with default settings.
func initDefaultLogger() {
	globalLogger = &Logger{
		level:  LevelInfo,
		format: FormatText,
		output: os.Stderr,
		fields: make(map[string]any),
	}
}

// Default returns the global default logger.
// If no logger has been configured, it returns a logger that writes
// info level and above to stderr in text format.
func Default() *Logger {
	globalLoggerOnce.Do(initDefaultLogger)

	globalLoggerMu.RLock()
	defer globalLoggerMu.RUnlock()
	return globalLogger
}

// SetDefault sets the global default logger.
// This should be called early in application startup to configure logging.
func SetDefault(l *Logger) {
	globalLoggerOnce.Do(initDefaultLogger) // Ensure initialization

	globalLoggerMu.Lock()
	defer globalLoggerMu.Unlock()
	globalLogger = l
}

// Package-level convenience functions that use the default logger

// Debug logs a message at debug level using the default logger.
func Debug(msg string, keyvals ...any) {
	Default().Debug(msg, keyvals...)
}

// Info logs a message at info level using the default logger.
func Info(msg string, keyvals ...any) {
	Default().Info(msg, keyvals...)
}

// Warn logs a message at warn level using the default logger.
func Warn(msg string, keyvals ...any) {
	Default().Warn(msg, keyvals...)
}

// Error logs a message at error level using the default logger.
func Error(msg string, keyvals ...any) {
	Default().Error(msg, keyvals...)
}
