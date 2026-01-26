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
	Listen       string          `yaml:"listen"`
	TLS          *TLSConfig      `yaml:"tls,omitempty"`
	CORS         *CORSConfig     `yaml:"cors,omitempty"`
	Auth         *AuthConfig     `yaml:"auth,omitempty"`
	ReadTimeout  time.Duration   `yaml:"read_timeout"`
	WriteTimeout time.Duration   `yaml:"write_timeout"`
	IdleTimeout  time.Duration   `yaml:"idle_timeout"`
	MCPServer    MCPServerConfig `yaml:"mcp_server"`
	Session      SessionConfig   `yaml:"session"`
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

// FindConfigFile looks for config.yaml in standard locations
func FindConfigFile() (string, error) {
	// Check current directory
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml", nil
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

	// Check common config directories
	configDirs := []string{
		"/etc/mcp-bridge",
		filepath.Join(os.Getenv("HOME"), ".config", "mcp-bridge"),
	}

	for _, dir := range configDirs {
		path := filepath.Join(dir, "config.yaml")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("config.yaml not found in standard locations")
}
