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

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Validate checks the configuration for errors
func Validate(cfg *Config) error {
	if cfg.Mode == "" {
		return errors.New("mode is required (server or client)")
	}

	switch cfg.Mode {
	case ModeServer:
		if cfg.Server == nil {
			return errors.New("server configuration is required when mode is 'server'")
		}
		return validateServer(cfg.Server)
	case ModeClient:
		if cfg.Client == nil {
			return errors.New("client configuration is required when mode is 'client'")
		}
		return validateClient(cfg.Client)
	default:
		return fmt.Errorf("invalid mode: %s (must be 'server' or 'client')", cfg.Mode)
	}
}

func validateServer(s *ServerConfig) error {
	// Validate listen address
	if s.Listen == "" {
		return errors.New("server.listen is required")
	}

	// Validate TLS
	if s.TLS != nil && s.TLS.Enabled {
		if err := validateTLS(s.TLS); err != nil {
			return fmt.Errorf("server.tls: %w", err)
		}
	}

	// Validate auth
	if s.Auth != nil {
		if err := validateAuth(s.Auth, true); err != nil {
			return fmt.Errorf("server.auth: %w", err)
		}
	}

	// Validate MCP server
	if err := validateMCPServer(&s.MCPServer); err != nil {
		return fmt.Errorf("server.mcp_server: %w", err)
	}

	// Validate CORS
	if s.CORS != nil && s.CORS.Enabled {
		if err := validateCORS(s.CORS); err != nil {
			return fmt.Errorf("server.cors: %w", err)
		}
	}

	return nil
}

func validateClient(c *ClientConfig) error {
	// Validate URL
	if c.URL == "" {
		return errors.New("client.url is required")
	}
	if _, err := url.Parse(c.URL); err != nil {
		return fmt.Errorf("client.url is invalid: %w", err)
	}

	// Validate TLS
	if c.TLS != nil {
		if err := validateClientTLS(c.TLS); err != nil {
			return fmt.Errorf("client.tls: %w", err)
		}
	}

	// Validate auth
	if c.Auth != nil {
		if err := validateAuth(c.Auth, false); err != nil {
			return fmt.Errorf("client.auth: %w", err)
		}
	}

	return nil
}

func validateTLS(t *TLSConfig) error {
	if t.CertFile == "" {
		return errors.New("cert_file is required when TLS is enabled")
	}
	if t.KeyFile == "" {
		return errors.New("key_file is required when TLS is enabled")
	}

	// Check if files exist
	if _, err := os.Stat(t.CertFile); os.IsNotExist(err) {
		return fmt.Errorf("cert_file does not exist: %s", t.CertFile)
	}
	if _, err := os.Stat(t.KeyFile); os.IsNotExist(err) {
		return fmt.Errorf("key_file does not exist: %s", t.KeyFile)
	}

	// Validate client_auth
	validClientAuth := map[string]bool{
		"none": true, "request": true, "require": true, "verify": true,
	}
	if t.ClientAuth != "" && !validClientAuth[t.ClientAuth] {
		return fmt.Errorf("invalid client_auth: %s", t.ClientAuth)
	}

	// Validate TLS versions
	validVersions := map[string]bool{"1.2": true, "1.3": true}
	if t.MinVersion != "" && !validVersions[t.MinVersion] {
		return fmt.Errorf("invalid min_version: %s (must be 1.2 or 1.3)", t.MinVersion)
	}
	if t.MaxVersion != "" && !validVersions[t.MaxVersion] {
		return fmt.Errorf("invalid max_version: %s (must be 1.2 or 1.3)", t.MaxVersion)
	}

	return nil
}

func validateClientTLS(t *TLSClientConfig) error {
	// If cert is provided, key must also be provided
	if (t.CertFile != "" && t.KeyFile == "") || (t.CertFile == "" && t.KeyFile != "") {
		return errors.New("both cert_file and key_file must be provided for client certificates")
	}

	// Check files exist if specified
	if t.CACert != "" {
		if _, err := os.Stat(t.CACert); os.IsNotExist(err) {
			return fmt.Errorf("ca_cert does not exist: %s", t.CACert)
		}
	}
	if t.CertFile != "" {
		if _, err := os.Stat(t.CertFile); os.IsNotExist(err) {
			return fmt.Errorf("cert_file does not exist: %s", t.CertFile)
		}
	}
	if t.KeyFile != "" {
		if _, err := os.Stat(t.KeyFile); os.IsNotExist(err) {
			return fmt.Errorf("key_file does not exist: %s", t.KeyFile)
		}
	}

	return nil
}

func validateAuth(a *AuthConfig, isServer bool) error {
	if a.Type == "" {
		return errors.New("type is required")
	}

	switch a.Type {
	case "bearer":
		return validateBearerAuth(a.Bearer, isServer)
	case "oauth":
		return validateOAuthAuth(a.OAuth, isServer)
	default:
		return fmt.Errorf("invalid auth type: %s (must be 'bearer' or 'oauth')", a.Type)
	}
}

func validateBearerAuth(b *BearerAuthConfig, isServer bool) error {
	if b == nil {
		return errors.New("bearer configuration is required when type is 'bearer'")
	}

	if isServer {
		// Server needs either valid_tokens or validation_endpoint
		if len(b.ValidTokens) == 0 && b.ValidationEndpoint == "" {
			return errors.New("either valid_tokens or validation_endpoint is required for server mode")
		}
	} else {
		// Client needs a token
		if b.Token == "" && b.TokenEnv == "" {
			return errors.New("either token or token_env is required for client mode")
		}
		// If token_env is specified, check it exists
		if b.TokenEnv != "" && os.Getenv(b.TokenEnv) == "" {
			return fmt.Errorf("environment variable %s is not set", b.TokenEnv)
		}
	}

	return nil
}

func validateOAuthAuth(o *OAuthConfig, isServer bool) error {
	if o == nil {
		return errors.New("oauth configuration is required when type is 'oauth'")
	}

	if o.ClientID == "" {
		return errors.New("client_id is required")
	}

	if isServer {
		// Server needs token validation capability
		if o.JWKSURL == "" && o.IntrospectionURL == "" && o.DiscoveryURL == "" {
			return errors.New("jwks_url, introspection_url, or discovery_url is required for server mode")
		}
	} else {
		// Client needs token endpoint
		if o.TokenURL == "" && o.DiscoveryURL == "" {
			return errors.New("token_url or discovery_url is required for client mode")
		}
	}

	return nil
}

func validateMCPServer(m *MCPServerConfig) error {
	if m.Command == "" {
		return errors.New("command is required")
	}

	// Check if command exists (basic check)
	// Note: This doesn't verify it's in PATH, just that it's specified
	if strings.TrimSpace(m.Command) == "" {
		return errors.New("command cannot be empty or whitespace")
	}

	return nil
}

func validateCORS(c *CORSConfig) error {
	if len(c.AllowedOrigins) == 0 {
		return errors.New("allowed_origins is required when CORS is enabled")
	}

	// Validate that origins look like URLs or wildcards
	for _, origin := range c.AllowedOrigins {
		if origin != "*" && !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
			return fmt.Errorf("invalid origin: %s (must be '*' or start with http:// or https://)", origin)
		}
	}

	return nil
}
