/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package config provides configuration types and loading for the MCP bridge.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Mode represents the bridge operating mode
type Mode string

const (
	ModeServer Mode = "server"
	ModeClient Mode = "client"
)

// Config is the root configuration structure
type Config struct {
	Mode   Mode          `yaml:"mode"`
	Server *ServerConfig `yaml:"server,omitempty"`
	Client *ClientConfig `yaml:"client,omitempty"`
	Log    LogConfig     `yaml:"log"`
}

// ServerConfig contains HTTP server mode configuration
type ServerConfig struct {
	Listen       string             `yaml:"listen"`
	TLS          *TLSConfig         `yaml:"tls,omitempty"`
	CORS         *CORSConfig        `yaml:"cors,omitempty"`
	Auth         *AuthConfig        `yaml:"auth,omitempty"`
	OAuthServer  *OAuthServerConfig `yaml:"oauth_server,omitempty"`
	ReadTimeout  time.Duration      `yaml:"read_timeout"`
	WriteTimeout time.Duration      `yaml:"write_timeout"`
	IdleTimeout  time.Duration      `yaml:"idle_timeout"`
	MCPServer    MCPServerConfig    `yaml:"mcp_server"`
	Session      SessionConfig      `yaml:"session"`
}

// ClientConfig contains HTTP client mode configuration
type ClientConfig struct {
	URL             string           `yaml:"url"`
	TLS             *TLSClientConfig `yaml:"tls,omitempty"`
	Auth            *AuthConfig      `yaml:"auth,omitempty"`
	Timeout         time.Duration    `yaml:"timeout"`
	MaxIdleConns    int              `yaml:"max_idle_conns"`
	IdleConnTimeout time.Duration    `yaml:"idle_conn_timeout"`
	Retry           RetryConfig      `yaml:"retry"`
}

// MCPServerConfig defines the subprocess MCP server
type MCPServerConfig struct {
	Command                 string            `yaml:"command"`
	Args                    []string          `yaml:"args"`
	Env                     map[string]string `yaml:"env,omitempty"`
	Dir                     string            `yaml:"dir,omitempty"`
	GracefulShutdownTimeout time.Duration     `yaml:"graceful_shutdown_timeout"`
	RestartOnFailure        bool              `yaml:"restart_on_failure"`
	MaxRestarts             int               `yaml:"max_restarts"`
	RestartDelay            time.Duration     `yaml:"restart_delay"`
}

// TLSConfig for server mode
type TLSConfig struct {
	Enabled    bool   `yaml:"enabled"`
	CertFile   string `yaml:"cert_file"`
	KeyFile    string `yaml:"key_file"`
	ClientCA   string `yaml:"client_ca,omitempty"`
	ClientAuth string `yaml:"client_auth,omitempty"`
	MinVersion string `yaml:"min_version,omitempty"`
	MaxVersion string `yaml:"max_version,omitempty"`
}

// TLSClientConfig for client mode
type TLSClientConfig struct {
	CACert             string `yaml:"ca_cert,omitempty"`
	CertFile           string `yaml:"cert_file,omitempty"`
	KeyFile            string `yaml:"key_file,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty"`
	ServerName         string `yaml:"server_name,omitempty"`
}

// CORSConfig defines CORS settings
type CORSConfig struct {
	Enabled          bool     `yaml:"enabled"`
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	ExposedHeaders   []string `yaml:"exposed_headers,omitempty"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Type   string            `yaml:"type"`
	Bearer *BearerAuthConfig `yaml:"bearer,omitempty"`
	OAuth  *OAuthConfig      `yaml:"oauth,omitempty"`
}

// BearerAuthConfig for simple bearer token auth
type BearerAuthConfig struct {
	ValidTokens        []string `yaml:"valid_tokens,omitempty"`
	ValidationEndpoint string   `yaml:"validation_endpoint,omitempty"`
	Token              string   `yaml:"token,omitempty"`
	TokenEnv           string   `yaml:"token_env,omitempty"`
}

// OAuthConfig for OAuth 2.0/2.1 authentication
type OAuthConfig struct {
	DiscoveryURL     string   `yaml:"discovery_url,omitempty"`
	AuthorizationURL string   `yaml:"authorization_url,omitempty"`
	TokenURL         string   `yaml:"token_url,omitempty"`
	ClientID         string   `yaml:"client_id"`
	ClientSecret     string   `yaml:"client_secret,omitempty"`
	Scopes           []string `yaml:"scopes,omitempty"`
	IntrospectionURL string   `yaml:"introspection_url,omitempty"`
	JWKSURL          string   `yaml:"jwks_url,omitempty"`
	Resource         string   `yaml:"resource,omitempty"`
	UsePKCE          bool     `yaml:"use_pkce"`
}

// OAuthServerConfig configures the OAuth 2.0 Authorization Server
type OAuthServerConfig struct {
	// Enabled turns on the authorization server functionality
	Enabled bool `yaml:"enabled"`

	// Issuer is the OAuth issuer URL (typically the bridge's external URL)
	Issuer string `yaml:"issuer"`

	// Mode: "builtin" or "federated"
	Mode string `yaml:"mode"`

	// TokenLifetime is the access token validity duration
	TokenLifetime time.Duration `yaml:"token_lifetime"`

	// RefreshTokenLifetime is the refresh token validity duration
	RefreshTokenLifetime time.Duration `yaml:"refresh_token_lifetime"`

	// AuthCodeLifetime is the authorization code validity (short, e.g., 10min)
	AuthCodeLifetime time.Duration `yaml:"auth_code_lifetime"`

	// Signing configuration for JWT signing
	Signing *SigningConfig `yaml:"signing,omitempty"`

	// BuiltIn configuration for built-in mode
	BuiltIn *BuiltInAuthConfig `yaml:"builtin,omitempty"`

	// Federated configuration for federated mode
	Federated *FederatedAuthConfig `yaml:"federated,omitempty"`

	// AllowedRedirectURIs lists allowed redirect URI patterns
	AllowedRedirectURIs []string `yaml:"allowed_redirect_uris"`

	// ScopesSupported lists the scopes this server supports
	ScopesSupported []string `yaml:"scopes_supported,omitempty"`

	// AllowDynamicRegistration enables the /oauth/register endpoint
	AllowDynamicRegistration bool `yaml:"allow_dynamic_registration"`
}

// SigningConfig for JWT token signing
type SigningConfig struct {
	// Algorithm: RS256, ES256, etc.
	Algorithm string `yaml:"algorithm"`

	// KeyFile path to private key (PEM format)
	KeyFile string `yaml:"key_file,omitempty"`

	// KeyID for JWKS
	KeyID string `yaml:"key_id,omitempty"`

	// GenerateKey generates an ephemeral key if true (dev mode only)
	GenerateKey bool `yaml:"generate_key,omitempty"`
}

// BuiltInAuthConfig for built-in user management
type BuiltInAuthConfig struct {
	// Users is a list of configured users
	Users []UserConfig `yaml:"users"`

	// LoginTemplate path to custom login page template
	LoginTemplate string `yaml:"login_template,omitempty"`
}

// UserConfig for a built-in user
type UserConfig struct {
	Username     string   `yaml:"username"`
	PasswordHash string   `yaml:"password_hash,omitempty"` // bcrypt hash
	PasswordEnv  string   `yaml:"password_env,omitempty"`  // env var for plaintext password (hashed at runtime)
	Scopes       []string `yaml:"scopes,omitempty"`
}

// FederatedAuthConfig for upstream IdP federation
type FederatedAuthConfig struct {
	// UpstreamIssuer is the upstream IdP's issuer URL
	UpstreamIssuer string `yaml:"upstream_issuer"`

	// UpstreamDiscoveryURL (optional, defaults to issuer + well-known)
	UpstreamDiscoveryURL string `yaml:"upstream_discovery_url,omitempty"`

	// ClientID for the upstream IdP
	ClientID string `yaml:"client_id"`

	// ClientSecret for the upstream IdP
	ClientSecret string `yaml:"client_secret,omitempty"`

	// ClientSecretEnv to read client secret from environment
	ClientSecretEnv string `yaml:"client_secret_env,omitempty"`

	// Scopes to request from upstream
	Scopes []string `yaml:"scopes,omitempty"`

	// AllowedDomains restricts which email domains can authenticate (optional)
	AllowedDomains []string `yaml:"allowed_domains,omitempty"`

	// DefaultScopes to grant to federated users
	DefaultScopes []string `yaml:"default_scopes,omitempty"`

	// AdminUsers list of users (by email or subject) to grant admin scopes
	AdminUsers []string `yaml:"admin_users,omitempty"`

	// AdminScopes to grant to admin users
	AdminScopes []string `yaml:"admin_scopes,omitempty"`
}

// SessionConfig for managing MCP sessions
type SessionConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxSessions     int           `yaml:"max_sessions"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

// RetryConfig for client mode retry behavior
type RetryConfig struct {
	Enabled      bool          `yaml:"enabled"`
	MaxRetries   int           `yaml:"max_retries"`
	InitialDelay time.Duration `yaml:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay"`
	Multiplier   float64       `yaml:"multiplier"`
}

// LogConfig for logging configuration
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

// envVarPattern matches ${VAR} or $VAR patterns
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// Load reads and parses a configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables
	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	// Validate
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// expandEnvVars replaces ${VAR} and $VAR with environment variable values
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		var name string
		if strings.HasPrefix(match, "${") {
			name = match[2 : len(match)-1]
		} else {
			name = match[1:]
		}
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match
	})
}

// FindConfigFile looks for config.yaml in standard locations.
// Search order: /etc/pgedge/config.yaml, then same directory as binary.
func FindConfigFile() (string, error) {
	// Check /etc/pgedge first
	etcPath := "/etc/pgedge/config.yaml"
	if _, err := os.Stat(etcPath); err == nil {
		return etcPath, nil
	}

	// Check executable directory
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		path := filepath.Join(exeDir, "config.yaml")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("config.yaml not found in /etc/pgedge or executable directory")
}
