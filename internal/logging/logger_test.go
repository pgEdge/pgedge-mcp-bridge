/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  Level
		expectErr bool
	}{
		{
			name:     "debug lowercase",
			input:    "debug",
			expected: LevelDebug,
		},
		{
			name:     "DEBUG uppercase",
			input:    "DEBUG",
			expected: LevelDebug,
		},
		{
			name:     "Debug mixed case",
			input:    "Debug",
			expected: LevelDebug,
		},
		{
			name:     "info",
			input:    "info",
			expected: LevelInfo,
		},
		{
			name:     "INFO",
			input:    "INFO",
			expected: LevelInfo,
		},
		{
			name:     "warn",
			input:    "warn",
			expected: LevelWarn,
		},
		{
			name:     "warning alias",
			input:    "warning",
			expected: LevelWarn,
		},
		{
			name:     "WARN",
			input:    "WARN",
			expected: LevelWarn,
		},
		{
			name:     "error",
			input:    "error",
			expected: LevelError,
		},
		{
			name:     "ERROR",
			input:    "ERROR",
			expected: LevelError,
		},
		{
			name:     "with spaces",
			input:    "  info  ",
			expected: LevelInfo,
		},
		{
			name:      "unknown level",
			input:     "unknown",
			expectErr: true,
		},
		{
			name:      "empty string",
			input:     "",
			expectErr: true,
		},
		{
			name:      "invalid",
			input:     "critical",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, err := ParseLevel(tt.input)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if level != tt.expected {
				t.Errorf("Expected level %v, got %v", tt.expected, level)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.level.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.level.String())
			}
		})
	}
}

func TestLevelMarshalJSON(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, `"debug"`},
		{LevelInfo, `"info"`},
		{LevelWarn, `"warn"`},
		{LevelError, `"error"`},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			data, err := json.Marshal(tt.level)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			if string(data) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(data))
			}
		})
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := config.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Replace output with buffer for testing
	logger.output = buf

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, `"level":"info"`) {
		t.Errorf("Expected JSON format with level, got: %s", output)
	}
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Errorf("Expected JSON format with message, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("Expected JSON format with key-value, got: %s", output)
	}
}

func TestNewLogger_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Replace output with buffer for testing
	logger.output = buf

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "INFO") {
		t.Errorf("Expected text format with INFO, got: %s", output)
	}
	if !strings.Contains(output, "test message") {
		t.Errorf("Expected text format with message, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected text format with key=value, got: %s", output)
	}
}

func TestNewLogger_FileOutput(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Info("test message to file")

	if err := logger.Close(); err != nil {
		t.Errorf("Failed to close logger: %v", err)
	}

	// Verify file was created and contains the message
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "test message to file") {
		t.Errorf("Expected log file to contain message, got: %s", string(content))
	}
}

func TestNewLogger_InvalidOutput(t *testing.T) {
	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: "/nonexistent/directory/file.log",
	}

	_, err := NewLogger(cfg)
	if err == nil {
		t.Error("Expected error for invalid output path")
	}
}

func TestNewLogger_StdoutOutput(t *testing.T) {
	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	if logger.output != os.Stdout {
		t.Error("Expected output to be stdout")
	}
}

func TestNewLogger_StderrOutput(t *testing.T) {
	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: "stderr",
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	if logger.output != os.Stderr {
		t.Error("Expected output to be stderr")
	}
}

func TestNewLogger_EmptyOutput(t *testing.T) {
	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: "",
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	if logger.output != os.Stdout {
		t.Error("Expected empty output to default to stdout")
	}
}

func TestLogFiltering(t *testing.T) {
	tests := []struct {
		name        string
		loggerLevel Level
		msgLevel    string
		shouldLog   bool
	}{
		{"debug message at debug level", LevelDebug, "debug", true},
		{"info message at debug level", LevelDebug, "info", true},
		{"warn message at debug level", LevelDebug, "warn", true},
		{"error message at debug level", LevelDebug, "error", true},

		{"debug message at info level", LevelInfo, "debug", false},
		{"info message at info level", LevelInfo, "info", true},
		{"warn message at info level", LevelInfo, "warn", true},
		{"error message at info level", LevelInfo, "error", true},

		{"debug message at warn level", LevelWarn, "debug", false},
		{"info message at warn level", LevelWarn, "info", false},
		{"warn message at warn level", LevelWarn, "warn", true},
		{"error message at warn level", LevelWarn, "error", true},

		{"debug message at error level", LevelError, "debug", false},
		{"info message at error level", LevelError, "info", false},
		{"warn message at error level", LevelError, "warn", false},
		{"error message at error level", LevelError, "error", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				level:  tt.loggerLevel,
				format: FormatText,
				output: buf,
				fields: make(map[string]any),
			}

			switch tt.msgLevel {
			case "debug":
				logger.Debug("test")
			case "info":
				logger.Info("test")
			case "warn":
				logger.Warn("test")
			case "error":
				logger.Error("test")
			}

			logged := buf.Len() > 0
			if logged != tt.shouldLog {
				t.Errorf("Expected logged=%v, got logged=%v", tt.shouldLog, logged)
			}
		})
	}
}

func TestWithFields(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatJSON,
		output: buf,
		fields: make(map[string]any),
	}

	childLogger := logger.WithFields(map[string]any{
		"component": "test",
		"version":   "1.0",
	})

	childLogger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, `"component":"test"`) {
		t.Errorf("Expected fields in output, got: %s", output)
	}
	if !strings.Contains(output, `"version":"1.0"`) {
		t.Errorf("Expected fields in output, got: %s", output)
	}
}

func TestWithFields_DoesNotModifyParent(t *testing.T) {
	parentBuf := &bytes.Buffer{}
	parent := &Logger{
		level:  LevelInfo,
		format: FormatJSON,
		output: parentBuf,
		fields: map[string]any{"parent": "value"},
	}

	child := parent.WithFields(map[string]any{"child": "value"})

	// Child should have both fields
	if child.fields["parent"] != "value" {
		t.Error("Child should inherit parent fields")
	}
	if child.fields["child"] != "value" {
		t.Error("Child should have new fields")
	}

	// Parent should only have original fields
	if _, ok := parent.fields["child"]; ok {
		t.Error("Parent should not have child fields")
	}
}

func TestWithError(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatJSON,
		output: buf,
		fields: make(map[string]any),
	}

	err := errors.New("test error")
	childLogger := logger.WithError(err)

	childLogger.Info("operation failed")

	output := buf.String()
	if !strings.Contains(output, `"error":"test error"`) {
		t.Errorf("Expected error field in output, got: %s", output)
	}
}

func TestWithError_NilError(t *testing.T) {
	logger := &Logger{
		level:  LevelInfo,
		format: FormatJSON,
		output: &bytes.Buffer{},
		fields: make(map[string]any),
	}

	childLogger := logger.WithError(nil)

	// Should return the same logger when error is nil
	if childLogger != logger {
		t.Error("Expected same logger when error is nil")
	}
}

func TestSetLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatText,
		output: buf,
		fields: make(map[string]any),
	}

	// Debug should not log at INFO level
	logger.Debug("should not appear")
	if buf.Len() > 0 {
		t.Error("Debug should not log at INFO level")
	}

	// Change to DEBUG level
	logger.SetLevel(LevelDebug)

	// Debug should now log
	logger.Debug("should appear")
	if buf.Len() == 0 {
		t.Error("Debug should log at DEBUG level")
	}
}

func TestGetLevel(t *testing.T) {
	logger := &Logger{
		level:  LevelWarn,
		format: FormatText,
		output: &bytes.Buffer{},
		fields: make(map[string]any),
	}

	if logger.GetLevel() != LevelWarn {
		t.Errorf("Expected LevelWarn, got %v", logger.GetLevel())
	}

	logger.SetLevel(LevelError)
	if logger.GetLevel() != LevelError {
		t.Errorf("Expected LevelError, got %v", logger.GetLevel())
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Info("test message")

	// Close should succeed
	if err := logger.Close(); err != nil {
		t.Errorf("Failed to close logger: %v", err)
	}

	// Close again should be safe
	if err := logger.Close(); err != nil {
		t.Errorf("Second close should not error: %v", err)
	}
}

func TestDefaultLogger(t *testing.T) {
	// Reset global logger for testing
	globalLoggerMu.Lock()
	oldLogger := globalLogger
	globalLogger = nil
	globalLoggerMu.Unlock()

	// Restore after test
	defer func() {
		globalLoggerMu.Lock()
		globalLogger = oldLogger
		globalLoggerMu.Unlock()
	}()

	// Reset the once
	globalLoggerOnce = sync.Once{}

	// Get default logger
	logger := Default()
	if logger == nil {
		t.Fatal("Expected non-nil default logger")
	}

	// Should return the same logger on subsequent calls
	logger2 := Default()
	if logger2 != logger {
		t.Error("Default() should return the same logger")
	}
}

func TestSetDefault(t *testing.T) {
	// Reset global logger for testing
	globalLoggerMu.Lock()
	oldLogger := globalLogger
	globalLoggerMu.Unlock()

	// Restore after test
	defer func() {
		globalLoggerMu.Lock()
		globalLogger = oldLogger
		globalLoggerMu.Unlock()
	}()

	// Reset the once
	globalLoggerOnce = sync.Once{}

	customLogger := &Logger{
		level:  LevelDebug,
		format: FormatJSON,
		output: &bytes.Buffer{},
		fields: make(map[string]any),
	}

	SetDefault(customLogger)

	if Default() != customLogger {
		t.Error("SetDefault should set the default logger")
	}
}

func TestPackageLevelFunctions(t *testing.T) {
	// Reset global logger for testing
	globalLoggerMu.Lock()
	oldLogger := globalLogger
	globalLoggerMu.Unlock()

	// Restore after test
	defer func() {
		globalLoggerMu.Lock()
		globalLogger = oldLogger
		globalLoggerMu.Unlock()
	}()

	// Reset the once
	globalLoggerOnce = sync.Once{}

	buf := &bytes.Buffer{}
	customLogger := &Logger{
		level:  LevelDebug,
		format: FormatText,
		output: buf,
		fields: make(map[string]any),
	}

	SetDefault(customLogger)

	// Test package-level functions
	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	output := buf.String()
	if !strings.Contains(output, "DEBUG") {
		t.Error("Expected DEBUG in output")
	}
	if !strings.Contains(output, "INFO") {
		t.Error("Expected INFO in output")
	}
	if !strings.Contains(output, "WARN") {
		t.Error("Expected WARN in output")
	}
	if !strings.Contains(output, "ERROR") {
		t.Error("Expected ERROR in output")
	}
}

func TestConcurrentLogging(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatText,
		output: buf,
		fields: make(map[string]any),
	}

	var wg sync.WaitGroup
	numGoroutines := 10
	numMessages := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numMessages; j++ {
				logger.Info("message", "goroutine", id, "count", j)
			}
		}(i)
	}

	wg.Wait()

	// Should have logged all messages without panic
	lines := strings.Split(buf.String(), "\n")
	// -1 because of trailing newline
	if len(lines)-1 < numGoroutines*numMessages {
		t.Errorf("Expected at least %d lines, got %d", numGoroutines*numMessages, len(lines)-1)
	}
}

func TestOddKeyValues(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatJSON,
		output: buf,
		fields: make(map[string]any),
	}

	// Odd number of key-value pairs should handle gracefully
	logger.Info("test", "key1", "value1", "key2")

	output := buf.String()
	if !strings.Contains(output, `"key2":"MISSING_VALUE"`) {
		t.Errorf("Expected MISSING_VALUE for odd key, got: %s", output)
	}
}

func TestNonStringKey(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatJSON,
		output: buf,
		fields: make(map[string]any),
	}

	// Non-string key should be converted to string
	logger.Info("test", 123, "value")

	output := buf.String()
	if !strings.Contains(output, `"123":"value"`) {
		t.Errorf("Expected converted key, got: %s", output)
	}
}

func TestTextFormatWithSpaces(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatText,
		output: buf,
		fields: make(map[string]any),
	}

	// Value with spaces should be quoted
	logger.Info("test", "key", "value with spaces")

	output := buf.String()
	if !strings.Contains(output, `key="value with spaces"`) {
		t.Errorf("Expected quoted value, got: %s", output)
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	cfg := config.LogConfig{
		Level:  "invalid",
		Format: "text",
		Output: "stdout",
	}

	// Should default to info level, not error
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Should not error for invalid level: %v", err)
	}
	defer logger.Close()

	if logger.level != LevelInfo {
		t.Errorf("Expected default to INFO level, got %v", logger.level)
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string without spaces", "hello", "hello"},
		{"string with spaces", "hello world", `"hello world"`},
		{"string with tabs", "hello\tworld", `"hello\tworld"`},
		{"string with newlines", "hello\nworld", `"hello\nworld"`},
		{"error", errors.New("test error"), "test error"},
		{"integer", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool", true, "true"},
		{"nil", nil, "<nil>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValue(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestJSONMarshalFailure(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		level:  LevelInfo,
		format: FormatJSON,
		output: buf,
		fields: make(map[string]any),
	}

	// Create a value that can't be marshaled to JSON (channel)
	ch := make(chan int)
	logger.Info("test", "channel", ch)

	output := buf.String()
	// Should still produce some output (fallback format)
	if buf.Len() == 0 {
		t.Error("Expected some output even on JSON marshal failure")
	}
	// The fallback should mention json marshal failed
	if !strings.Contains(output, "time") && !strings.Contains(output, "json marshal failed") {
		t.Errorf("Expected fallback output, got: %s", output)
	}
}

func TestChildLoggerDoesNotInheritFile(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	child := logger.WithFields(map[string]any{"key": "value"})

	// Child should not have file ownership
	if child.file != nil {
		t.Error("Child logger should not inherit file ownership")
	}

	// Close parent
	if err := logger.Close(); err != nil {
		t.Errorf("Failed to close parent: %v", err)
	}

	// Close child should be safe (no file to close)
	if err := child.Close(); err != nil {
		t.Errorf("Child close should not error: %v", err)
	}
}
