/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package config

import "time"

// Default configuration values
const (
	DefaultServerListen      = ":8080"
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 60 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultClientTimeout     = 30 * time.Second
	DefaultMaxIdleConns      = 10
	DefaultIdleConnTimeout   = 90 * time.Second
	DefaultSessionTimeout    = 30 * time.Minute
	DefaultMaxSessions       = 100
	DefaultCleanupInterval   = 5 * time.Minute
	DefaultGracefulShutdown  = 30 * time.Second
	DefaultMaxRestarts       = 5
	DefaultRestartDelay      = 5 * time.Second
	DefaultRetryMaxRetries   = 3
	DefaultRetryInitialDelay = 100 * time.Millisecond
	DefaultRetryMaxDelay     = 5 * time.Second
	DefaultRetryMultiplier   = 2.0
	DefaultLogLevel          = "info"
	DefaultLogFormat         = "text"
	DefaultLogOutput         = "stderr"
	DefaultTLSMinVersion     = "1.2"
	DefaultCORSMaxAge        = 86400

	// OAuth Server defaults
	DefaultOAuthTokenLifetime        = 1 * time.Hour
	DefaultOAuthRefreshTokenLifetime = 24 * time.Hour
	DefaultOAuthAuthCodeLifetime     = 10 * time.Minute
	DefaultOAuthSigningAlgorithm     = "RS256"
	DefaultOAuthServerMode           = "builtin"
)

// DefaultCORSMethods is the default list of allowed HTTP methods
var DefaultCORSMethods = []string{"GET", "POST", "DELETE", "OPTIONS"}

// DefaultCORSHeaders is the default list of allowed headers
var DefaultCORSHeaders = []string{"Authorization", "Content-Type", "Mcp-Session-Id", "Accept"}

// DefaultOAuthScopes is the default list of supported OAuth scopes
var DefaultOAuthScopes = []string{"mcp:read", "mcp:write"}

// DefaultOAuthRedirectURIs is the default list of allowed redirect URIs for Claude Desktop
var DefaultOAuthRedirectURIs = []string{
	"https://claude.ai/api/mcp/auth_callback",
	"https://claude.com/api/mcp/auth_callback",
}

// applyDefaults sets default values for unset configuration options
func applyDefaults(cfg *Config) {
	// Log defaults
	if cfg.Log.Level == "" {
		cfg.Log.Level = DefaultLogLevel
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = DefaultLogFormat
	}
	if cfg.Log.Output == "" {
		cfg.Log.Output = DefaultLogOutput
	}

	// Server defaults
	if cfg.Server != nil {
		applyServerDefaults(cfg.Server)
	}

	// Client defaults
	if cfg.Client != nil {
		applyClientDefaults(cfg.Client)
	}
}

func applyServerDefaults(s *ServerConfig) {
	if s.Listen == "" {
		s.Listen = DefaultServerListen
	}
	if s.ReadTimeout == 0 {
		s.ReadTimeout = DefaultReadTimeout
	}
	if s.WriteTimeout == 0 {
		s.WriteTimeout = DefaultWriteTimeout
	}
	if s.IdleTimeout == 0 {
		s.IdleTimeout = DefaultIdleTimeout
	}

	// MCP Server defaults
	if s.MCPServer.GracefulShutdownTimeout == 0 {
		s.MCPServer.GracefulShutdownTimeout = DefaultGracefulShutdown
	}
	if s.MCPServer.MaxRestarts == 0 {
		s.MCPServer.MaxRestarts = DefaultMaxRestarts
	}
	if s.MCPServer.RestartDelay == 0 {
		s.MCPServer.RestartDelay = DefaultRestartDelay
	}

	// Session defaults
	if s.Session.Timeout == 0 {
		s.Session.Timeout = DefaultSessionTimeout
	}
	if s.Session.MaxSessions == 0 {
		s.Session.MaxSessions = DefaultMaxSessions
	}
	if s.Session.CleanupInterval == 0 {
		s.Session.CleanupInterval = DefaultCleanupInterval
	}

	// TLS defaults
	if s.TLS != nil && s.TLS.Enabled {
		if s.TLS.MinVersion == "" {
			s.TLS.MinVersion = DefaultTLSMinVersion
		}
		if s.TLS.ClientAuth == "" {
			s.TLS.ClientAuth = "none"
		}
	}

	// CORS defaults
	if s.CORS != nil && s.CORS.Enabled {
		if len(s.CORS.AllowedMethods) == 0 {
			s.CORS.AllowedMethods = DefaultCORSMethods
		}
		if len(s.CORS.AllowedHeaders) == 0 {
			s.CORS.AllowedHeaders = DefaultCORSHeaders
		}
		if s.CORS.MaxAge == 0 {
			s.CORS.MaxAge = DefaultCORSMaxAge
		}
	}

	// OAuth Server defaults
	if s.OAuthServer != nil && s.OAuthServer.Enabled {
		applyOAuthServerDefaults(s.OAuthServer)
	}
}

func applyOAuthServerDefaults(o *OAuthServerConfig) {
	if o.Mode == "" {
		o.Mode = DefaultOAuthServerMode
	}
	if o.TokenLifetime == 0 {
		o.TokenLifetime = DefaultOAuthTokenLifetime
	}
	if o.RefreshTokenLifetime == 0 {
		o.RefreshTokenLifetime = DefaultOAuthRefreshTokenLifetime
	}
	if o.AuthCodeLifetime == 0 {
		o.AuthCodeLifetime = DefaultOAuthAuthCodeLifetime
	}
	if len(o.ScopesSupported) == 0 {
		o.ScopesSupported = DefaultOAuthScopes
	}
	if len(o.AllowedRedirectURIs) == 0 {
		o.AllowedRedirectURIs = DefaultOAuthRedirectURIs
	}
	if o.Signing != nil && o.Signing.Algorithm == "" {
		o.Signing.Algorithm = DefaultOAuthSigningAlgorithm
	}
}

func applyClientDefaults(c *ClientConfig) {
	if c.Timeout == 0 {
		c.Timeout = DefaultClientTimeout
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = DefaultMaxIdleConns
	}
	if c.IdleConnTimeout == 0 {
		c.IdleConnTimeout = DefaultIdleConnTimeout
	}

	// Retry defaults
	if c.Retry.Enabled {
		if c.Retry.MaxRetries == 0 {
			c.Retry.MaxRetries = DefaultRetryMaxRetries
		}
		if c.Retry.InitialDelay == 0 {
			c.Retry.InitialDelay = DefaultRetryInitialDelay
		}
		if c.Retry.MaxDelay == 0 {
			c.Retry.MaxDelay = DefaultRetryMaxDelay
		}
		if c.Retry.Multiplier == 0 {
			c.Retry.Multiplier = DefaultRetryMultiplier
		}
	}
}
