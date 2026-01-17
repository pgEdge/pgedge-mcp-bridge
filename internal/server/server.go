// Package server implements the HTTP server mode for the MCP HTTP bridge.
// It exposes a stdio-based MCP server over HTTP, handling JSON-RPC requests,
// Server-Sent Events for notifications, and session management.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/auth"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/cors"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/process"
	tlspkg "github.com/pgEdge/pgedge-mcp-bridge/internal/tls"
)

// Server represents the HTTP server that bridges HTTP requests to a stdio MCP server.
// It manages the lifecycle of both the HTTP server and the MCP subprocess.
type Server struct {
	// httpServer is the underlying HTTP server.
	httpServer *http.Server

	// processManager manages the MCP subprocess lifecycle.
	processManager process.Manager

	// sessionManager manages MCP sessions.
	sessionManager *SessionManager

	// auth is the authenticator for validating incoming requests.
	auth auth.Authenticator

	// authMiddleware is the authentication middleware.
	authMiddleware *auth.AuthMiddleware

	// corsHandler handles CORS for the HTTP server.
	corsHandler *cors.Handler

	// tlsConfig holds the TLS configuration for the server.
	tlsConfig *tls.Config

	// logger is used for structured logging.
	logger *logging.Logger

	// cfg holds the server configuration.
	cfg *config.ServerConfig

	// listener is the network listener for the HTTP server.
	listener net.Listener

	// mcpHandler is the handler for MCP requests.
	mcpHandler *MCPHandler

	// running indicates whether the server is currently running.
	running atomic.Bool

	// mu protects concurrent access to server state.
	mu sync.RWMutex

	// shutdownOnce ensures shutdown only happens once.
	shutdownOnce sync.Once

	// done is closed when the server has fully stopped.
	done chan struct{}
}

// NewServer creates a new HTTP server with the provided configuration and logger.
// It initializes the process manager, session manager, authentication, TLS, and CORS
// based on the configuration.
func NewServer(cfg *config.ServerConfig, logger *logging.Logger) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("server config is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	s := &Server{
		cfg:    cfg,
		logger: logger,
		done:   make(chan struct{}),
	}

	// Initialize session manager
	s.sessionManager = NewSessionManager(cfg.Session)

	// Initialize process manager for the MCP subprocess
	pmConfig := process.ManagerConfig{
		Command:                 cfg.MCPServer.Command,
		Args:                    cfg.MCPServer.Args,
		Env:                     cfg.MCPServer.Env,
		Dir:                     cfg.MCPServer.Dir,
		GracefulShutdownTimeout: cfg.MCPServer.GracefulShutdownTimeout,
		RestartOnFailure:        cfg.MCPServer.RestartOnFailure,
		MaxRestarts:             cfg.MCPServer.MaxRestarts,
		RestartDelay:            cfg.MCPServer.RestartDelay,
	}
	s.processManager = process.NewManager(pmConfig)

	// Initialize TLS if enabled
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsConfig, err := tlspkg.NewServerTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("configuring TLS: %w", err)
		}
		s.tlsConfig = tlsConfig
		logger.Info("TLS enabled for server",
			"cert_file", cfg.TLS.CertFile,
			"min_version", cfg.TLS.MinVersion,
		)
	}

	// Initialize CORS handler
	s.corsHandler = cors.NewHandler(cfg.CORS)
	if s.corsHandler.IsEnabled() {
		logger.Info("CORS enabled for server")
	}

	// Initialize authentication
	if cfg.Auth != nil && cfg.Auth.Type != "" && cfg.Auth.Type != "none" {
		authenticator, err := auth.NewAuthenticator(cfg.Auth, true)
		if err != nil {
			return nil, fmt.Errorf("configuring authentication: %w", err)
		}
		s.auth = authenticator
		s.authMiddleware = auth.NewAuthMiddleware(
			authenticator,
			auth.WithSkipPaths("/health", "/ready"),
			auth.WithRealm("MCP Bridge"),
		)
		logger.Info("authentication enabled for server", "type", cfg.Auth.Type)
	}

	// Create MCP handler
	s.mcpHandler = NewMCPHandler(s.processManager, s.sessionManager, logger)

	// Build HTTP router
	mux := s.buildRouter()

	// Apply middleware chain
	var handler http.Handler = mux

	// Apply CORS middleware
	if s.corsHandler.IsEnabled() {
		handler = s.corsHandler.Wrap(handler)
	}

	// Apply auth middleware (skips health/ready endpoints)
	if s.authMiddleware != nil {
		handler = s.authMiddleware.Wrap(handler)
	}

	// Configure HTTP server
	s.httpServer = &http.Server{
		Addr:         cfg.Listen,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		TLSConfig:    s.tlsConfig,
	}

	return s, nil
}

// buildRouter creates the HTTP router with all endpoints.
func (s *Server) buildRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// MCP endpoints
	mux.HandleFunc("POST /mcp", s.mcpHandler.HandlePost)
	mux.HandleFunc("GET /mcp", s.mcpHandler.HandleSSE)
	mux.HandleFunc("DELETE /mcp", s.mcpHandler.HandleSessionClose)

	// Health check endpoints
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)

	return mux
}

// Start starts the HTTP server and MCP subprocess.
// It blocks until the server is shut down or an error occurs.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running.Load() {
		s.mu.Unlock()
		return errors.New("server is already running")
	}

	// Reset done channel if restarting
	select {
	case <-s.done:
		s.done = make(chan struct{})
		s.shutdownOnce = sync.Once{}
	default:
	}

	s.mu.Unlock()

	// Start the MCP subprocess
	s.logger.Info("starting MCP subprocess",
		"command", s.cfg.MCPServer.Command,
		"args", s.cfg.MCPServer.Args,
	)

	if err := s.processManager.Start(ctx); err != nil {
		return fmt.Errorf("starting MCP subprocess: %w", err)
	}

	// Start monitoring process events
	go s.monitorProcessEvents(ctx)

	// Start session cleanup
	s.sessionManager.StartCleanup(ctx)

	// Create listener
	var listener net.Listener
	var err error

	if s.tlsConfig != nil {
		listener, err = tls.Listen("tcp", s.cfg.Listen, s.tlsConfig)
	} else {
		listener, err = net.Listen("tcp", s.cfg.Listen)
	}
	if err != nil {
		// Clean up subprocess on listener error
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.processManager.Stop(stopCtx)
		return fmt.Errorf("creating listener: %w", err)
	}

	s.mu.Lock()
	s.listener = listener
	s.running.Store(true)
	s.mu.Unlock()

	scheme := "http"
	if s.tlsConfig != nil {
		scheme = "https"
	}
	s.logger.Info("HTTP server started",
		"address", listener.Addr().String(),
		"scheme", scheme,
	)

	// Serve requests
	err = s.httpServer.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving HTTP: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the HTTP server and MCP subprocess.
// It waits for active connections to complete up to the context deadline.
func (s *Server) Stop(ctx context.Context) error {
	var stopErr error

	s.shutdownOnce.Do(func() {
		s.logger.Info("stopping server")

		s.running.Store(false)

		// Create a timeout context if none provided
		if ctx == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
		}

		// Shutdown HTTP server first
		if s.httpServer != nil {
			s.logger.Debug("shutting down HTTP server")
			if err := s.httpServer.Shutdown(ctx); err != nil {
				s.logger.Error("error shutting down HTTP server", "error", err)
				stopErr = fmt.Errorf("shutting down HTTP server: %w", err)
			}
		}

		// Stop session manager cleanup
		s.sessionManager.StopCleanup()

		// Close all active sessions
		s.sessionManager.CloseAllSessions()

		// Stop the MCP subprocess
		if s.processManager != nil {
			s.logger.Debug("stopping MCP subprocess")
			if err := s.processManager.Stop(ctx); err != nil {
				s.logger.Error("error stopping MCP subprocess", "error", err)
				if stopErr == nil {
					stopErr = fmt.Errorf("stopping MCP subprocess: %w", err)
				}
			}
		}

		// Close the authenticator
		if s.auth != nil {
			if err := s.auth.Close(); err != nil {
				s.logger.Error("error closing authenticator", "error", err)
			}
		}

		// Close the process manager
		if s.processManager != nil {
			if err := s.processManager.Close(); err != nil {
				s.logger.Error("error closing process manager", "error", err)
			}
		}

		// Signal completion
		close(s.done)
		s.logger.Info("server stopped")
	})

	return stopErr
}

// Done returns a channel that is closed when the server has fully stopped.
func (s *Server) Done() <-chan struct{} {
	return s.done
}

// Running returns true if the server is currently running.
func (s *Server) Running() bool {
	return s.running.Load()
}

// Addr returns the address the server is listening on.
// Returns empty string if the server is not running.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// monitorProcessEvents monitors the MCP subprocess for lifecycle events.
func (s *Server) monitorProcessEvents(ctx context.Context) {
	events := s.processManager.Events()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case evt, ok := <-events:
			if !ok {
				return
			}

			switch evt.Type {
			case process.EventStarted:
				s.logger.Info("MCP subprocess started", "pid", evt.PID)
			case process.EventStopped:
				s.logger.Info("MCP subprocess stopped", "exit_code", evt.ExitCode)
			case process.EventFailed:
				s.logger.Error("MCP subprocess failed",
					"exit_code", evt.ExitCode,
					"error", evt.Error,
				)
			case process.EventRestarting:
				s.logger.Warn("MCP subprocess restarting")
			case process.EventMaxRestartsReached:
				s.logger.Error("MCP subprocess max restarts reached")
				// Optionally shut down the server
				go func() {
					stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					s.Stop(stopCtx)
				}()
			}
		}
	}
}

// handleHealth handles the /health endpoint.
// It returns a simple health check response indicating the server is running.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

// handleReady handles the /ready endpoint.
// It checks if the server is ready to serve requests, including whether
// the MCP subprocess is running.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check if MCP subprocess is running
	if !s.processManager.Running() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"not_ready","reason":"mcp_subprocess_not_running"}`))
		return
	}

	// Check if server is running
	if !s.running.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"not_ready","reason":"server_not_running"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
}

// SessionManager returns the server's session manager.
func (s *Server) SessionManager() *SessionManager {
	return s.sessionManager
}

// ProcessManager returns the server's process manager.
func (s *Server) ProcessManager() process.Manager {
	return s.processManager
}
