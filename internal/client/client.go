// Package client provides an HTTP client implementation for the MCP HTTP bridge.
// It connects to a remote HTTP MCP server and exposes it locally via stdio,
// enabling MCP clients to communicate with HTTP-based MCP servers.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/auth"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/tls"
)

// Common client errors.
var (
	// ErrClientClosed indicates the client has been closed.
	ErrClientClosed = errors.New("client is closed")

	// ErrNoResponse indicates no response was received from the server.
	ErrNoResponse = errors.New("no response received from server")

	// ErrSessionExpired indicates the MCP session has expired.
	ErrSessionExpired = errors.New("mcp session expired")
)

// Client implements an HTTP client that connects to a remote MCP server
// and bridges communication via stdio. It handles authentication, TLS,
// session management, and retry logic.
type Client struct {
	// Configuration
	config *config.ClientConfig
	logger *logging.Logger

	// HTTP transport
	transport *Transport

	// Authentication
	auth auth.Authenticator

	// Stdio handler
	stdio *StdioHandler

	// Session management
	sessionID string
	sessionMu sync.RWMutex

	// State management
	closed   bool
	closedMu sync.RWMutex
	closeWg  sync.WaitGroup
}

// NewClient creates a new HTTP client for connecting to a remote MCP server.
// It configures TLS and authentication based on the provided configuration.
func NewClient(cfg *config.ClientConfig, logger *logging.Logger) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("client config is required")
	}

	if cfg.URL == "" {
		return nil, errors.New("server URL is required")
	}

	if logger == nil {
		logger = logging.Default()
	}

	// Create authenticator if auth is configured
	var authenticator auth.Authenticator
	var err error
	if cfg.Auth != nil {
		authenticator, err = auth.NewAuthenticator(cfg.Auth, false) // false = client mode
		if err != nil {
			return nil, fmt.Errorf("creating authenticator: %w", err)
		}
	}

	// Create TLS config if configured
	var tlsConfig interface{}
	if cfg.TLS != nil {
		tlsConfig, err = tls.NewClientTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("creating TLS config: %w", err)
		}
	}

	// Create transport
	transport, err := NewTransport(cfg, authenticator, tlsConfig)
	if err != nil {
		if authenticator != nil {
			authenticator.Close()
		}
		return nil, fmt.Errorf("creating transport: %w", err)
	}

	// Create stdio handler with os.Stdin and os.Stdout
	stdio := NewStdioHandler(os.Stdin, os.Stdout, logger)

	client := &Client{
		config:    cfg,
		logger:    logger.WithFields(map[string]any{"component": "client"}),
		transport: transport,
		auth:      authenticator,
		stdio:     stdio,
	}

	return client, nil
}

// Run starts the client and begins processing messages from stdin.
// It reads JSON-RPC messages from stdin, sends them to the remote server,
// and writes responses to stdout. This method blocks until the context
// is cancelled or an error occurs.
func (c *Client) Run(ctx context.Context) error {
	c.closedMu.RLock()
	if c.closed {
		c.closedMu.RUnlock()
		return ErrClientClosed
	}
	c.closedMu.RUnlock()

	c.logger.Info("starting MCP HTTP client", "url", c.config.URL)

	// Start reading messages from stdin
	messages := c.stdio.ReadMessages(ctx)

	// Process messages until context is cancelled
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("client context cancelled, shutting down")
			return ctx.Err()

		case msg, ok := <-messages:
			if !ok {
				c.logger.Info("stdin closed, shutting down")
				return nil
			}

			if err := c.handleMessage(ctx, msg); err != nil {
				// Log error but continue processing unless fatal
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				c.logger.Error("error handling message", "error", err)
			}
		}
	}
}

// handleMessage processes a single message from stdin, sends it to the server,
// and writes the response to stdout.
func (c *Client) handleMessage(ctx context.Context, msg []byte) error {
	c.logger.Debug("received message from stdin", "size", len(msg))

	// Get current session ID
	c.sessionMu.RLock()
	sessionID := c.sessionID
	c.sessionMu.RUnlock()

	// Send message to server
	response, newSessionID, err := c.transport.Send(ctx, msg, sessionID)
	if err != nil {
		return c.handleSendError(ctx, msg, err)
	}

	// Update session ID if server provided a new one
	if newSessionID != "" && newSessionID != sessionID {
		c.sessionMu.Lock()
		c.sessionID = newSessionID
		c.sessionMu.Unlock()
		c.logger.Debug("updated session ID", "session_id", newSessionID)
	}

	// Write response to stdout
	if len(response) > 0 {
		if err := c.stdio.WriteMessage(response); err != nil {
			return fmt.Errorf("writing response to stdout: %w", err)
		}
		c.logger.Debug("wrote response to stdout", "size", len(response))
	}

	return nil
}

// handleSendError handles errors from sending messages to the server.
// It implements retry logic and handles session expiration.
func (c *Client) handleSendError(ctx context.Context, msg []byte, err error) error {
	// Check for session expiration
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.IsSessionExpired() {
		c.logger.Warn("session expired, clearing session ID")
		c.sessionMu.Lock()
		c.sessionID = ""
		c.sessionMu.Unlock()
		// Retry the request without session ID
		return c.retryMessage(ctx, msg, "")
	}

	// Check if retries are enabled
	if !c.config.Retry.Enabled {
		return fmt.Errorf("sending message to server: %w", err)
	}

	// Attempt retry with exponential backoff
	c.sessionMu.RLock()
	sessionID := c.sessionID
	c.sessionMu.RUnlock()
	return c.retryMessage(ctx, msg, sessionID)
}

// retryMessage attempts to resend a message with exponential backoff.
func (c *Client) retryMessage(ctx context.Context, msg []byte, sessionID string) error {
	if !c.config.Retry.Enabled {
		return fmt.Errorf("retries not enabled")
	}

	delay := c.config.Retry.InitialDelay
	maxRetries := c.config.Retry.MaxRetries

	for attempt := 1; attempt <= maxRetries; attempt++ {
		c.logger.Debug("retrying message", "attempt", attempt, "max_retries", maxRetries)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// Continue with retry
		}

		response, newSessionID, err := c.transport.Send(ctx, msg, sessionID)
		if err == nil {
			// Success - update session ID and write response
			if newSessionID != "" {
				c.sessionMu.Lock()
				c.sessionID = newSessionID
				c.sessionMu.Unlock()
			}

			if len(response) > 0 {
				if err := c.stdio.WriteMessage(response); err != nil {
					return fmt.Errorf("writing response to stdout: %w", err)
				}
			}
			return nil
		}

		c.logger.Warn("retry attempt failed", "attempt", attempt, "error", err)

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * c.config.Retry.Multiplier)
		if delay > c.config.Retry.MaxDelay {
			delay = c.config.Retry.MaxDelay
		}
	}

	return fmt.Errorf("max retries (%d) exceeded", maxRetries)
}

// RunWithSSE starts the client in SSE mode for streaming responses.
// This is useful for long-running operations where the server sends
// multiple events over time.
func (c *Client) RunWithSSE(ctx context.Context) error {
	c.closedMu.RLock()
	if c.closed {
		c.closedMu.RUnlock()
		return ErrClientClosed
	}
	c.closedMu.RUnlock()

	c.logger.Info("starting MCP HTTP client with SSE support", "url", c.config.URL)

	// Start reading messages from stdin
	messages := c.stdio.ReadMessages(ctx)

	// Process messages until context is cancelled
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("client context cancelled, shutting down")
			return ctx.Err()

		case msg, ok := <-messages:
			if !ok {
				c.logger.Info("stdin closed, shutting down")
				return nil
			}

			if err := c.handleMessageWithSSE(ctx, msg); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				c.logger.Error("error handling SSE message", "error", err)
			}
		}
	}
}

// handleMessageWithSSE processes a message and handles streaming SSE responses.
func (c *Client) handleMessageWithSSE(ctx context.Context, msg []byte) error {
	c.logger.Debug("received message from stdin (SSE mode)", "size", len(msg))

	// Get current session ID
	c.sessionMu.RLock()
	sessionID := c.sessionID
	c.sessionMu.RUnlock()

	// Send message and get SSE event channel
	events, newSessionID, err := c.transport.SendWithSSE(ctx, msg, sessionID)
	if err != nil {
		return fmt.Errorf("sending SSE request: %w", err)
	}

	// Update session ID if server provided a new one
	if newSessionID != "" && newSessionID != sessionID {
		c.sessionMu.Lock()
		c.sessionID = newSessionID
		c.sessionMu.Unlock()
		c.logger.Debug("updated session ID (SSE)", "session_id", newSessionID)
	}

	// Process SSE events
	for event := range events {
		if len(event) > 0 {
			if err := c.stdio.WriteMessage(event); err != nil {
				return fmt.Errorf("writing SSE event to stdout: %w", err)
			}
			c.logger.Debug("wrote SSE event to stdout", "size", len(event))
		}
	}

	return nil
}

// SessionID returns the current MCP session ID.
func (c *Client) SessionID() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.sessionID
}

// SetSessionID sets the MCP session ID.
// This can be used to resume a previous session.
func (c *Client) SetSessionID(sessionID string) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	c.sessionID = sessionID
}

// Close shuts down the client and releases all resources.
func (c *Client) Close() error {
	c.closedMu.Lock()
	if c.closed {
		c.closedMu.Unlock()
		return nil
	}
	c.closed = true
	c.closedMu.Unlock()

	c.logger.Info("closing MCP HTTP client")

	var errs []error

	// Close transport
	if c.transport != nil {
		if err := c.transport.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing transport: %w", err))
		}
	}

	// Close authenticator
	if c.auth != nil {
		if err := c.auth.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing authenticator: %w", err))
		}
	}

	// Close stdio handler
	if c.stdio != nil {
		if err := c.stdio.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing stdio handler: %w", err))
		}
	}

	// Wait for any pending operations
	c.closeWg.Wait()

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Closed returns true if the client has been closed.
func (c *Client) Closed() bool {
	c.closedMu.RLock()
	defer c.closedMu.RUnlock()
	return c.closed
}

// WriteError writes a JSON-RPC error response to stdout.
func (c *Client) WriteError(err error) error {
	// Create a JSON-RPC error response
	errMsg := fmt.Sprintf(`{"jsonrpc":"2.0","error":{"code":-32603,"message":%q},"id":null}`, err.Error())
	return c.stdio.WriteMessage([]byte(errMsg))
}

// StdioReader returns the underlying io.Reader for stdin.
// This is useful for custom message handling.
func (c *Client) StdioReader() io.Reader {
	return c.stdio.Reader()
}

// StdioWriter returns the underlying io.Writer for stdout.
// This is useful for custom message handling.
func (c *Client) StdioWriter() io.Writer {
	return c.stdio.Writer()
}

// Config returns the client configuration.
func (c *Client) Config() *config.ClientConfig {
	return c.config
}

// Transport returns the underlying HTTP transport.
// This can be used for advanced operations.
func (c *Client) Transport() *Transport {
	return c.transport
}

// RefreshAuth refreshes the authentication credentials if applicable.
// This is useful for OAuth tokens that may expire.
func (c *Client) RefreshAuth(ctx context.Context) error {
	if c.auth == nil {
		return nil
	}
	return c.auth.Refresh(ctx)
}
