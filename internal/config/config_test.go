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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Test fixtures path
const testdataPath = "../../testdata/configs"

// ===========================================================================
// Load Function Tests
// ===========================================================================

func TestLoad_ValidServerConfig(t *testing.T) {
	cfg, err := Load(filepath.Join(testdataPath, "valid_server.yaml"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Mode != ModeServer {
		t.Errorf("expected mode 'server', got '%s'", cfg.Mode)
	}

	if cfg.Server == nil {
		t.Fatal("expected server config to be non-nil")
	}

	if cfg.Server.Listen != ":8443" {
		t.Errorf("expected listen ':8443', got '%s'", cfg.Server.Listen)
	}

	if cfg.Server.ReadTimeout != 45*time.Second {
		t.Errorf("expected read_timeout 45s, got '%v'", cfg.Server.ReadTimeout)
	}

	if cfg.Server.WriteTimeout != 90*time.Second {
		t.Errorf("expected write_timeout 90s, got '%v'", cfg.Server.WriteTimeout)
	}

	if cfg.Server.MCPServer.Command != "/usr/local/bin/mcp-server" {
		t.Errorf("expected command '/usr/local/bin/mcp-server', got '%s'", cfg.Server.MCPServer.Command)
	}

	if len(cfg.Server.MCPServer.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(cfg.Server.MCPServer.Args))
	}

	if cfg.Server.MCPServer.Env["MCP_DEBUG"] != "true" {
		t.Errorf("expected MCP_DEBUG='true', got '%s'", cfg.Server.MCPServer.Env["MCP_DEBUG"])
	}

	if cfg.Server.Session.MaxSessions != 500 {
		t.Errorf("expected max_sessions 500, got %d", cfg.Server.Session.MaxSessions)
	}

	if cfg.Log.Level != "debug" {
		t.Errorf("expected log level 'debug', got '%s'", cfg.Log.Level)
	}

	if cfg.Log.Format != "json" {
		t.Errorf("expected log format 'json', got '%s'", cfg.Log.Format)
	}
}

func TestLoad_ValidClientConfig(t *testing.T) {
	cfg, err := Load(filepath.Join(testdataPath, "valid_client.yaml"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Mode != ModeClient {
		t.Errorf("expected mode 'client', got '%s'", cfg.Mode)
	}

	if cfg.Client == nil {
		t.Fatal("expected client config to be non-nil")
	}

	if cfg.Client.URL != "https://mcp.example.com:8443/mcp" {
		t.Errorf("expected url 'https://mcp.example.com:8443/mcp', got '%s'", cfg.Client.URL)
	}

	if cfg.Client.Timeout != 60*time.Second {
		t.Errorf("expected timeout 60s, got '%v'", cfg.Client.Timeout)
	}

	if cfg.Client.MaxIdleConns != 20 {
		t.Errorf("expected max_idle_conns 20, got %d", cfg.Client.MaxIdleConns)
	}

	if !cfg.Client.Retry.Enabled {
		t.Error("expected retry.enabled to be true")
	}

	if cfg.Client.Retry.MaxRetries != 5 {
		t.Errorf("expected retry.max_retries 5, got %d", cfg.Client.Retry.MaxRetries)
	}

	if cfg.Client.Retry.Multiplier != 2.5 {
		t.Errorf("expected retry.multiplier 2.5, got %f", cfg.Client.Retry.Multiplier)
	}
}

func TestLoad_EnvironmentVariableExpansion(t *testing.T) {
	// Set environment variables for the test
	os.Setenv("TEST_LISTEN_ADDR", ":9090")
	os.Setenv("TEST_MCP_COMMAND", "/custom/mcp-server")
	os.Setenv("TEST_API_KEY", "secret-key-123")
	os.Setenv("TEST_LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("TEST_LISTEN_ADDR")
		os.Unsetenv("TEST_MCP_COMMAND")
		os.Unsetenv("TEST_API_KEY")
		os.Unsetenv("TEST_LOG_LEVEL")
	}()

	cfg, err := Load(filepath.Join(testdataPath, "env_vars.yaml"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Server.Listen != ":9090" {
		t.Errorf("expected listen ':9090' (from ${TEST_LISTEN_ADDR}), got '%s'", cfg.Server.Listen)
	}

	if cfg.Server.MCPServer.Command != "/custom/mcp-server" {
		t.Errorf("expected command '/custom/mcp-server' (from $TEST_MCP_COMMAND), got '%s'", cfg.Server.MCPServer.Command)
	}

	if cfg.Server.MCPServer.Env["API_KEY"] != "secret-key-123" {
		t.Errorf("expected API_KEY 'secret-key-123', got '%s'", cfg.Server.MCPServer.Env["API_KEY"])
	}

	if cfg.Log.Level != "debug" {
		t.Errorf("expected log level 'debug' (from $TEST_LOG_LEVEL), got '%s'", cfg.Log.Level)
	}
}

func TestLoad_EnvironmentVariableNotSet(t *testing.T) {
	// Ensure the env vars are not set
	os.Unsetenv("TEST_LISTEN_ADDR")
	os.Unsetenv("TEST_MCP_COMMAND")
	os.Unsetenv("TEST_API_KEY")
	os.Unsetenv("TEST_LOG_LEVEL")

	// Create a temporary config file with unset env vars
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `mode: server
server:
  listen: ${UNSET_VAR}
  mcp_server:
    command: /usr/bin/mcp
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// When env var is not set, the pattern should remain unchanged
	if cfg.Server.Listen != "${UNSET_VAR}" {
		t.Errorf("expected listen '${UNSET_VAR}' (unset var unchanged), got '%s'", cfg.Server.Listen)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}

	if !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("expected error to contain 'reading config file', got: %v", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	_, err := Load(filepath.Join(testdataPath, "invalid.yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}

	if !strings.Contains(err.Error(), "parsing config file") {
		t.Errorf("expected error to contain 'parsing config file', got: %v", err)
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error for empty config file")
	}

	if !strings.Contains(err.Error(), "mode is required") {
		t.Errorf("expected error about missing mode, got: %v", err)
	}
}

// ===========================================================================
// Validation Tests
// ===========================================================================

func TestValidate_ModeRequired(t *testing.T) {
	cfg := &Config{}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing mode")
	}
	if !strings.Contains(err.Error(), "mode is required") {
		t.Errorf("expected error about mode, got: %v", err)
	}
}

func TestValidate_InvalidMode(t *testing.T) {
	cfg := &Config{Mode: "invalid"}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "invalid mode") {
		t.Errorf("expected error about invalid mode, got: %v", err)
	}
}

func TestValidate_ServerModeWithoutServerConfig(t *testing.T) {
	cfg := &Config{Mode: ModeServer}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error when server config is missing")
	}
	if !strings.Contains(err.Error(), "server configuration is required") {
		t.Errorf("expected error about missing server config, got: %v", err)
	}
}

func TestValidate_ClientModeWithoutClientConfig(t *testing.T) {
	cfg := &Config{Mode: ModeClient}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error when client config is missing")
	}
	if !strings.Contains(err.Error(), "client configuration is required") {
		t.Errorf("expected error about missing client config, got: %v", err)
	}
}

func TestValidateServer_ValidConfig(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for valid server config, got: %v", err)
	}
}

func TestValidateServer_MissingListenAddress(t *testing.T) {
	server := &ServerConfig{
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing listen address")
	}
	if !strings.Contains(err.Error(), "server.listen is required") {
		t.Errorf("expected error about listen address, got: %v", err)
	}
}

func TestValidateServer_InvalidTLSConfig_MissingCert(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		TLS: &TLSConfig{
			Enabled: true,
			KeyFile: "/path/to/key.pem",
			// CertFile is missing
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing cert file")
	}
	if !strings.Contains(err.Error(), "cert_file is required") {
		t.Errorf("expected error about missing cert_file, got: %v", err)
	}
}

func TestValidateServer_InvalidTLSConfig_MissingKey(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certFile, []byte("cert content"), 0644); err != nil {
		t.Fatalf("failed to write test cert file: %v", err)
	}

	server := &ServerConfig{
		Listen: ":8080",
		TLS: &TLSConfig{
			Enabled:  true,
			CertFile: certFile,
			// KeyFile is missing
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
	if !strings.Contains(err.Error(), "key_file is required") {
		t.Errorf("expected error about missing key_file, got: %v", err)
	}
}

func TestValidateServer_TLSConfig_NonExistentCertFile(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		TLS: &TLSConfig{
			Enabled:  true,
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  "/nonexistent/key.pem",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for non-existent cert file")
	}
	if !strings.Contains(err.Error(), "cert_file does not exist") {
		t.Errorf("expected error about cert_file not existing, got: %v", err)
	}
}

func TestValidateServer_TLSConfig_NonExistentKeyFile(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certFile, []byte("cert content"), 0644); err != nil {
		t.Fatalf("failed to write test cert file: %v", err)
	}

	server := &ServerConfig{
		Listen: ":8080",
		TLS: &TLSConfig{
			Enabled:  true,
			CertFile: certFile,
			KeyFile:  "/nonexistent/key.pem",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for non-existent key file")
	}
	if !strings.Contains(err.Error(), "key_file does not exist") {
		t.Errorf("expected error about key_file not existing, got: %v", err)
	}
}

func TestValidateServer_TLSConfig_InvalidClientAuth(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	if err := os.WriteFile(certFile, []byte("cert content"), 0644); err != nil {
		t.Fatalf("failed to write test cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("key content"), 0644); err != nil {
		t.Fatalf("failed to write test key file: %v", err)
	}

	server := &ServerConfig{
		Listen: ":8080",
		TLS: &TLSConfig{
			Enabled:    true,
			CertFile:   certFile,
			KeyFile:    keyFile,
			ClientAuth: "invalid_auth",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for invalid client_auth")
	}
	if !strings.Contains(err.Error(), "invalid client_auth") {
		t.Errorf("expected error about invalid client_auth, got: %v", err)
	}
}

func TestValidateServer_TLSConfig_InvalidMinVersion(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)

	server := &ServerConfig{
		Listen: ":8080",
		TLS: &TLSConfig{
			Enabled:    true,
			CertFile:   certFile,
			KeyFile:    keyFile,
			MinVersion: "1.0",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for invalid min_version")
	}
	if !strings.Contains(err.Error(), "invalid min_version") {
		t.Errorf("expected error about invalid min_version, got: %v", err)
	}
}

func TestValidateServer_TLSConfig_InvalidMaxVersion(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)

	server := &ServerConfig{
		Listen: ":8080",
		TLS: &TLSConfig{
			Enabled:    true,
			CertFile:   certFile,
			KeyFile:    keyFile,
			MaxVersion: "1.1",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for invalid max_version")
	}
	if !strings.Contains(err.Error(), "invalid max_version") {
		t.Errorf("expected error about invalid max_version, got: %v", err)
	}
}

func TestValidateServer_TLSConfig_ValidVersions(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)

	testCases := []struct {
		name       string
		minVersion string
		maxVersion string
	}{
		{"TLS 1.2 only", "1.2", "1.2"},
		{"TLS 1.3 only", "1.3", "1.3"},
		{"TLS 1.2-1.3", "1.2", "1.3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := &ServerConfig{
				Listen: ":8080",
				TLS: &TLSConfig{
					Enabled:    true,
					CertFile:   certFile,
					KeyFile:    keyFile,
					MinVersion: tc.minVersion,
					MaxVersion: tc.maxVersion,
				},
				MCPServer: MCPServerConfig{
					Command: "/usr/bin/mcp",
				},
			}
			err := validateServer(server)
			if err != nil {
				t.Errorf("expected no error for valid TLS versions, got: %v", err)
			}
		})
	}
}

func TestValidateServer_InvalidAuthConfig_MissingType(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth:   &AuthConfig{},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing auth type")
	}
	if !strings.Contains(err.Error(), "type is required") {
		t.Errorf("expected error about missing type, got: %v", err)
	}
}

func TestValidateServer_InvalidAuthConfig_InvalidType(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "invalid_type",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for invalid auth type")
	}
	if !strings.Contains(err.Error(), "invalid auth type") {
		t.Errorf("expected error about invalid auth type, got: %v", err)
	}
}

func TestValidateServer_BearerAuth_MissingBearerConfig(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "bearer",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing bearer config")
	}
	if !strings.Contains(err.Error(), "bearer configuration is required") {
		t.Errorf("expected error about missing bearer config, got: %v", err)
	}
}

func TestValidateServer_BearerAuth_NoValidationMethod(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type:   "bearer",
			Bearer: &BearerAuthConfig{},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for bearer auth without validation method")
	}
	if !strings.Contains(err.Error(), "either valid_tokens or validation_endpoint is required") {
		t.Errorf("expected error about validation method, got: %v", err)
	}
}

func TestValidateServer_BearerAuth_ValidWithTokens(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "bearer",
			Bearer: &BearerAuthConfig{
				ValidTokens: []string{"token1", "token2"},
			},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for valid bearer auth with tokens, got: %v", err)
	}
}

func TestValidateServer_BearerAuth_ValidWithEndpoint(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "bearer",
			Bearer: &BearerAuthConfig{
				ValidationEndpoint: "https://auth.example.com/validate",
			},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for valid bearer auth with endpoint, got: %v", err)
	}
}

func TestValidateServer_OAuthAuth_MissingOAuthConfig(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "oauth",
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing oauth config")
	}
	if !strings.Contains(err.Error(), "oauth configuration is required") {
		t.Errorf("expected error about missing oauth config, got: %v", err)
	}
}

func TestValidateServer_OAuthAuth_MissingClientID(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				DiscoveryURL: "https://auth.example.com/.well-known/openid-configuration",
			},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing client_id")
	}
	if !strings.Contains(err.Error(), "client_id is required") {
		t.Errorf("expected error about missing client_id, got: %v", err)
	}
}

func TestValidateServer_OAuthAuth_NoValidationMethod(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				ClientID: "my-client",
			},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for oauth auth without validation method")
	}
	if !strings.Contains(err.Error(), "jwks_url, introspection_url, or discovery_url is required") {
		t.Errorf("expected error about validation method, got: %v", err)
	}
}

func TestValidateServer_OAuthAuth_ValidWithJWKS(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				ClientID: "my-client",
				JWKSURL:  "https://auth.example.com/.well-known/jwks.json",
			},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for valid oauth auth with JWKS, got: %v", err)
	}
}

func TestValidateServer_OAuthAuth_ValidWithIntrospection(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				ClientID:         "my-client",
				IntrospectionURL: "https://auth.example.com/oauth/introspect",
			},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for valid oauth auth with introspection, got: %v", err)
	}
}

func TestValidateServer_OAuthAuth_ValidWithDiscovery(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				ClientID:     "my-client",
				DiscoveryURL: "https://auth.example.com/.well-known/openid-configuration",
			},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for valid oauth auth with discovery, got: %v", err)
	}
}

func TestValidateServer_MissingMCPCommand(t *testing.T) {
	server := &ServerConfig{
		Listen:    ":8080",
		MCPServer: MCPServerConfig{},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing MCP command")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Errorf("expected error about missing command, got: %v", err)
	}
}

func TestValidateServer_WhitespaceMCPCommand(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		MCPServer: MCPServerConfig{
			Command: "   ",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for whitespace-only MCP command")
	}
	if !strings.Contains(err.Error(), "command cannot be empty") {
		t.Errorf("expected error about empty command, got: %v", err)
	}
}

func TestValidateServer_ValidCORS(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		CORS: &CORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{"https://example.com", "http://localhost:3000"},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for valid CORS config, got: %v", err)
	}
}

func TestValidateServer_CORS_WildcardOrigin(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		CORS: &CORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{"*"},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err != nil {
		t.Errorf("expected no error for wildcard CORS origin, got: %v", err)
	}
}

func TestValidateServer_CORS_MissingOrigins(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		CORS: &CORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for missing allowed origins")
	}
	if !strings.Contains(err.Error(), "allowed_origins is required") {
		t.Errorf("expected error about allowed_origins, got: %v", err)
	}
}

func TestValidateServer_CORS_InvalidOrigin(t *testing.T) {
	server := &ServerConfig{
		Listen: ":8080",
		CORS: &CORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{"example.com"},
		},
		MCPServer: MCPServerConfig{
			Command: "/usr/bin/mcp",
		},
	}
	err := validateServer(server)
	if err == nil {
		t.Fatal("expected error for invalid origin")
	}
	if !strings.Contains(err.Error(), "invalid origin") {
		t.Errorf("expected error about invalid origin, got: %v", err)
	}
}

func TestValidateClient_ValidConfig(t *testing.T) {
	client := &ClientConfig{
		URL: "https://mcp.example.com:8443",
	}
	err := validateClient(client)
	if err != nil {
		t.Errorf("expected no error for valid client config, got: %v", err)
	}
}

func TestValidateClient_MissingURL(t *testing.T) {
	client := &ClientConfig{}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
	if !strings.Contains(err.Error(), "client.url is required") {
		t.Errorf("expected error about missing URL, got: %v", err)
	}
}

func TestValidateClient_InvalidURL(t *testing.T) {
	testCases := []struct {
		name string
		url  string
	}{
		{"control characters", "http://example.com/\x00path"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &ClientConfig{URL: tc.url}
			err := validateClient(client)
			if err == nil {
				t.Fatal("expected error for invalid URL")
			}
			if !strings.Contains(err.Error(), "client.url is invalid") {
				t.Errorf("expected error about invalid URL, got: %v", err)
			}
		})
	}
}

func TestValidateClient_ValidURLs(t *testing.T) {
	validURLs := []string{
		"http://localhost:8080",
		"https://mcp.example.com",
		"https://mcp.example.com:8443/mcp",
		"http://192.168.1.1:9090",
	}

	for _, url := range validURLs {
		t.Run(url, func(t *testing.T) {
			client := &ClientConfig{URL: url}
			err := validateClient(client)
			if err != nil {
				t.Errorf("expected no error for valid URL '%s', got: %v", url, err)
			}
		})
	}
}

func TestValidateClient_InvalidTLSConfig_PartialCert(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)

	client := &ClientConfig{
		URL: "https://mcp.example.com",
		TLS: &TLSClientConfig{
			CertFile: certFile,
			// KeyFile is missing
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for partial TLS cert config")
	}
	if !strings.Contains(err.Error(), "both cert_file and key_file must be provided") {
		t.Errorf("expected error about both cert and key, got: %v", err)
	}
}

func TestValidateClient_InvalidTLSConfig_PartialKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(keyFile, []byte("key content"), 0644)

	client := &ClientConfig{
		URL: "https://mcp.example.com",
		TLS: &TLSClientConfig{
			KeyFile: keyFile,
			// CertFile is missing
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for partial TLS key config")
	}
	if !strings.Contains(err.Error(), "both cert_file and key_file must be provided") {
		t.Errorf("expected error about both cert and key, got: %v", err)
	}
}

func TestValidateClient_InvalidTLSConfig_NonExistentCA(t *testing.T) {
	client := &ClientConfig{
		URL: "https://mcp.example.com",
		TLS: &TLSClientConfig{
			CACert: "/nonexistent/ca.pem",
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for non-existent CA cert")
	}
	if !strings.Contains(err.Error(), "ca_cert does not exist") {
		t.Errorf("expected error about ca_cert not existing, got: %v", err)
	}
}

func TestValidateClient_InvalidTLSConfig_NonExistentCert(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(keyFile, []byte("key content"), 0644)

	client := &ClientConfig{
		URL: "https://mcp.example.com",
		TLS: &TLSClientConfig{
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  keyFile,
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for non-existent cert file")
	}
	if !strings.Contains(err.Error(), "cert_file does not exist") {
		t.Errorf("expected error about cert_file not existing, got: %v", err)
	}
}

func TestValidateClient_InvalidTLSConfig_NonExistentKey(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)

	client := &ClientConfig{
		URL: "https://mcp.example.com",
		TLS: &TLSClientConfig{
			CertFile: certFile,
			KeyFile:  "/nonexistent/key.pem",
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for non-existent key file")
	}
	if !strings.Contains(err.Error(), "key_file does not exist") {
		t.Errorf("expected error about key_file not existing, got: %v", err)
	}
}

func TestValidateClient_ValidTLSConfig(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	caFile := filepath.Join(tmpDir, "ca.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)
	os.WriteFile(caFile, []byte("ca content"), 0644)

	client := &ClientConfig{
		URL: "https://mcp.example.com",
		TLS: &TLSClientConfig{
			CACert:             caFile,
			CertFile:           certFile,
			KeyFile:            keyFile,
			InsecureSkipVerify: false,
			ServerName:         "mcp.example.com",
		},
	}
	err := validateClient(client)
	if err != nil {
		t.Errorf("expected no error for valid TLS config, got: %v", err)
	}
}

func TestValidateClient_BearerAuth_NoToken(t *testing.T) {
	client := &ClientConfig{
		URL: "https://mcp.example.com",
		Auth: &AuthConfig{
			Type:   "bearer",
			Bearer: &BearerAuthConfig{},
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for bearer auth without token")
	}
	if !strings.Contains(err.Error(), "either token or token_env is required") {
		t.Errorf("expected error about token requirement, got: %v", err)
	}
}

func TestValidateClient_BearerAuth_ValidToken(t *testing.T) {
	client := &ClientConfig{
		URL: "https://mcp.example.com",
		Auth: &AuthConfig{
			Type: "bearer",
			Bearer: &BearerAuthConfig{
				Token: "my-secret-token",
			},
		},
	}
	err := validateClient(client)
	if err != nil {
		t.Errorf("expected no error for valid bearer auth with token, got: %v", err)
	}
}

func TestValidateClient_BearerAuth_ValidTokenEnv(t *testing.T) {
	os.Setenv("TEST_AUTH_TOKEN", "secret-token")
	defer os.Unsetenv("TEST_AUTH_TOKEN")

	client := &ClientConfig{
		URL: "https://mcp.example.com",
		Auth: &AuthConfig{
			Type: "bearer",
			Bearer: &BearerAuthConfig{
				TokenEnv: "TEST_AUTH_TOKEN",
			},
		},
	}
	err := validateClient(client)
	if err != nil {
		t.Errorf("expected no error for valid bearer auth with token_env, got: %v", err)
	}
}

func TestValidateClient_BearerAuth_UnsetTokenEnv(t *testing.T) {
	os.Unsetenv("UNSET_TOKEN_VAR")

	client := &ClientConfig{
		URL: "https://mcp.example.com",
		Auth: &AuthConfig{
			Type: "bearer",
			Bearer: &BearerAuthConfig{
				TokenEnv: "UNSET_TOKEN_VAR",
			},
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for unset token_env")
	}
	if !strings.Contains(err.Error(), "environment variable UNSET_TOKEN_VAR is not set") {
		t.Errorf("expected error about unset env var, got: %v", err)
	}
}

func TestValidateClient_OAuthAuth_NoTokenURL(t *testing.T) {
	client := &ClientConfig{
		URL: "https://mcp.example.com",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				ClientID: "my-client",
			},
		},
	}
	err := validateClient(client)
	if err == nil {
		t.Fatal("expected error for oauth auth without token_url or discovery_url")
	}
	if !strings.Contains(err.Error(), "token_url or discovery_url is required") {
		t.Errorf("expected error about token_url requirement, got: %v", err)
	}
}

func TestValidateClient_OAuthAuth_ValidTokenURL(t *testing.T) {
	client := &ClientConfig{
		URL: "https://mcp.example.com",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				ClientID:     "my-client",
				ClientSecret: "my-secret",
				TokenURL:     "https://auth.example.com/oauth/token",
				Scopes:       []string{"read", "write"},
			},
		},
	}
	err := validateClient(client)
	if err != nil {
		t.Errorf("expected no error for valid oauth auth with token_url, got: %v", err)
	}
}

func TestValidateClient_OAuthAuth_ValidDiscoveryURL(t *testing.T) {
	client := &ClientConfig{
		URL: "https://mcp.example.com",
		Auth: &AuthConfig{
			Type: "oauth",
			OAuth: &OAuthConfig{
				ClientID:     "my-client",
				DiscoveryURL: "https://auth.example.com/.well-known/openid-configuration",
			},
		},
	}
	err := validateClient(client)
	if err != nil {
		t.Errorf("expected no error for valid oauth auth with discovery_url, got: %v", err)
	}
}

// ===========================================================================
// Defaults Tests
// ===========================================================================

func TestDefaults_ServerMode(t *testing.T) {
	cfg, err := Load(filepath.Join(testdataPath, "minimal_server.yaml"))
	if err != nil {
		t.Fatalf("failed to load minimal server config: %v", err)
	}

	// Check server defaults
	if cfg.Server.Listen != DefaultServerListen {
		t.Errorf("expected default listen '%s', got '%s'", DefaultServerListen, cfg.Server.Listen)
	}
	if cfg.Server.ReadTimeout != DefaultReadTimeout {
		t.Errorf("expected default read_timeout %v, got %v", DefaultReadTimeout, cfg.Server.ReadTimeout)
	}
	if cfg.Server.WriteTimeout != DefaultWriteTimeout {
		t.Errorf("expected default write_timeout %v, got %v", DefaultWriteTimeout, cfg.Server.WriteTimeout)
	}
	if cfg.Server.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("expected default idle_timeout %v, got %v", DefaultIdleTimeout, cfg.Server.IdleTimeout)
	}

	// Check MCP server defaults
	if cfg.Server.MCPServer.GracefulShutdownTimeout != DefaultGracefulShutdown {
		t.Errorf("expected default graceful_shutdown_timeout %v, got %v", DefaultGracefulShutdown, cfg.Server.MCPServer.GracefulShutdownTimeout)
	}
	if cfg.Server.MCPServer.MaxRestarts != DefaultMaxRestarts {
		t.Errorf("expected default max_restarts %d, got %d", DefaultMaxRestarts, cfg.Server.MCPServer.MaxRestarts)
	}
	if cfg.Server.MCPServer.RestartDelay != DefaultRestartDelay {
		t.Errorf("expected default restart_delay %v, got %v", DefaultRestartDelay, cfg.Server.MCPServer.RestartDelay)
	}

	// Check session defaults
	if cfg.Server.Session.Timeout != DefaultSessionTimeout {
		t.Errorf("expected default session timeout %v, got %v", DefaultSessionTimeout, cfg.Server.Session.Timeout)
	}
	if cfg.Server.Session.MaxSessions != DefaultMaxSessions {
		t.Errorf("expected default max_sessions %d, got %d", DefaultMaxSessions, cfg.Server.Session.MaxSessions)
	}
	if cfg.Server.Session.CleanupInterval != DefaultCleanupInterval {
		t.Errorf("expected default cleanup_interval %v, got %v", DefaultCleanupInterval, cfg.Server.Session.CleanupInterval)
	}

	// Check log defaults
	if cfg.Log.Level != DefaultLogLevel {
		t.Errorf("expected default log level '%s', got '%s'", DefaultLogLevel, cfg.Log.Level)
	}
	if cfg.Log.Format != DefaultLogFormat {
		t.Errorf("expected default log format '%s', got '%s'", DefaultLogFormat, cfg.Log.Format)
	}
	if cfg.Log.Output != DefaultLogOutput {
		t.Errorf("expected default log output '%s', got '%s'", DefaultLogOutput, cfg.Log.Output)
	}
}

func TestDefaults_ClientMode(t *testing.T) {
	cfg, err := Load(filepath.Join(testdataPath, "minimal_client.yaml"))
	if err != nil {
		t.Fatalf("failed to load minimal client config: %v", err)
	}

	// Check client defaults
	if cfg.Client.Timeout != DefaultClientTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultClientTimeout, cfg.Client.Timeout)
	}
	if cfg.Client.MaxIdleConns != DefaultMaxIdleConns {
		t.Errorf("expected default max_idle_conns %d, got %d", DefaultMaxIdleConns, cfg.Client.MaxIdleConns)
	}
	if cfg.Client.IdleConnTimeout != DefaultIdleConnTimeout {
		t.Errorf("expected default idle_conn_timeout %v, got %v", DefaultIdleConnTimeout, cfg.Client.IdleConnTimeout)
	}

	// Check log defaults
	if cfg.Log.Level != DefaultLogLevel {
		t.Errorf("expected default log level '%s', got '%s'", DefaultLogLevel, cfg.Log.Level)
	}
	if cfg.Log.Format != DefaultLogFormat {
		t.Errorf("expected default log format '%s', got '%s'", DefaultLogFormat, cfg.Log.Format)
	}
	if cfg.Log.Output != DefaultLogOutput {
		t.Errorf("expected default log output '%s', got '%s'", DefaultLogOutput, cfg.Log.Output)
	}
}

func TestDefaults_ClientRetry(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `mode: client
client:
  url: http://localhost:8080
  retry:
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if !cfg.Client.Retry.Enabled {
		t.Error("expected retry to be enabled")
	}
	if cfg.Client.Retry.MaxRetries != DefaultRetryMaxRetries {
		t.Errorf("expected default max_retries %d, got %d", DefaultRetryMaxRetries, cfg.Client.Retry.MaxRetries)
	}
	if cfg.Client.Retry.InitialDelay != DefaultRetryInitialDelay {
		t.Errorf("expected default initial_delay %v, got %v", DefaultRetryInitialDelay, cfg.Client.Retry.InitialDelay)
	}
	if cfg.Client.Retry.MaxDelay != DefaultRetryMaxDelay {
		t.Errorf("expected default max_delay %v, got %v", DefaultRetryMaxDelay, cfg.Client.Retry.MaxDelay)
	}
	if cfg.Client.Retry.Multiplier != DefaultRetryMultiplier {
		t.Errorf("expected default multiplier %f, got %f", DefaultRetryMultiplier, cfg.Client.Retry.Multiplier)
	}
}

func TestDefaults_ServerTLS(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `mode: server
server:
  listen: ":8443"
  tls:
    enabled: true
    cert_file: ` + certFile + `
    key_file: ` + keyFile + `
  mcp_server:
    command: /usr/bin/mcp
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server.TLS.MinVersion != DefaultTLSMinVersion {
		t.Errorf("expected default TLS min_version '%s', got '%s'", DefaultTLSMinVersion, cfg.Server.TLS.MinVersion)
	}
	if cfg.Server.TLS.ClientAuth != "none" {
		t.Errorf("expected default TLS client_auth 'none', got '%s'", cfg.Server.TLS.ClientAuth)
	}
}

func TestDefaults_ServerCORS(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `mode: server
server:
  listen: ":8080"
  cors:
    enabled: true
    allowed_origins:
      - "*"
  mcp_server:
    command: /usr/bin/mcp
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Server.CORS.AllowedMethods) != len(DefaultCORSMethods) {
		t.Errorf("expected default CORS methods %v, got %v", DefaultCORSMethods, cfg.Server.CORS.AllowedMethods)
	}
	if len(cfg.Server.CORS.AllowedHeaders) != len(DefaultCORSHeaders) {
		t.Errorf("expected default CORS headers %v, got %v", DefaultCORSHeaders, cfg.Server.CORS.AllowedHeaders)
	}
	if cfg.Server.CORS.MaxAge != DefaultCORSMaxAge {
		t.Errorf("expected default CORS max_age %d, got %d", DefaultCORSMaxAge, cfg.Server.CORS.MaxAge)
	}
}

func TestDefaults_ExplicitValuesOverride(t *testing.T) {
	cfg, err := Load(filepath.Join(testdataPath, "valid_server.yaml"))
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Values from valid_server.yaml should not be overridden by defaults
	if cfg.Server.Listen == DefaultServerListen {
		t.Errorf("expected listen to be ':8443', not default '%s'", DefaultServerListen)
	}
	if cfg.Server.ReadTimeout == DefaultReadTimeout {
		t.Errorf("expected read_timeout to be 45s, not default %v", DefaultReadTimeout)
	}
	if cfg.Server.Session.MaxSessions == DefaultMaxSessions {
		t.Errorf("expected max_sessions to be 500, not default %d", DefaultMaxSessions)
	}
	if cfg.Log.Level == DefaultLogLevel {
		t.Errorf("expected log level to be 'debug', not default '%s'", DefaultLogLevel)
	}
}

func TestDefaults_NoRetryDefaults_WhenDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `mode: client
client:
  url: http://localhost:8080
  retry:
    enabled: false
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// When retry is disabled, defaults should not be applied
	if cfg.Client.Retry.MaxRetries != 0 {
		t.Errorf("expected max_retries to be 0 when disabled, got %d", cfg.Client.Retry.MaxRetries)
	}
	if cfg.Client.Retry.InitialDelay != 0 {
		t.Errorf("expected initial_delay to be 0 when disabled, got %v", cfg.Client.Retry.InitialDelay)
	}
}

// ===========================================================================
// FindConfigFile Tests
// ===========================================================================

func TestFindConfigFile_NotFound(t *testing.T) {
	// FindConfigFile searches /etc/pgedge and executable directory.
	// In a test environment, neither should have config.yaml,
	// so we expect an error.
	_, err := FindConfigFile()
	if err == nil {
		// Config might exist in /etc/pgedge or executable dir on some systems
		t.Skip("config.yaml found in standard location, skipping not-found test")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error about file not found, got: %v", err)
	}
}

// ===========================================================================
// expandEnvVars Tests
// ===========================================================================

func TestExpandEnvVars_BracketSyntax(t *testing.T) {
	os.Setenv("TEST_VAR", "test-value")
	defer os.Unsetenv("TEST_VAR")

	result := expandEnvVars("prefix ${TEST_VAR} suffix")
	expected := "prefix test-value suffix"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestExpandEnvVars_DollarSyntax(t *testing.T) {
	os.Setenv("TEST_VAR", "test-value")
	defer os.Unsetenv("TEST_VAR")

	result := expandEnvVars("prefix $TEST_VAR suffix")
	expected := "prefix test-value suffix"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestExpandEnvVars_MultipleVars(t *testing.T) {
	os.Setenv("VAR1", "value1")
	os.Setenv("VAR2", "value2")
	defer os.Unsetenv("VAR1")
	defer os.Unsetenv("VAR2")

	result := expandEnvVars("${VAR1} and $VAR2")
	expected := "value1 and value2"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestExpandEnvVars_UnsetVar(t *testing.T) {
	os.Unsetenv("UNSET_VAR")

	result := expandEnvVars("${UNSET_VAR}")
	expected := "${UNSET_VAR}"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestExpandEnvVars_EmptyString(t *testing.T) {
	result := expandEnvVars("")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestExpandEnvVars_NoVars(t *testing.T) {
	result := expandEnvVars("no variables here")
	expected := "no variables here"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestExpandEnvVars_SpecialChars(t *testing.T) {
	os.Setenv("TEST_VAR", "value with spaces and $pecial chars!")
	defer os.Unsetenv("TEST_VAR")

	result := expandEnvVars("${TEST_VAR}")
	expected := "value with spaces and $pecial chars!"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

// ===========================================================================
// Table-Driven Validation Tests
// ===========================================================================

func TestValidation_TableDriven(t *testing.T) {
	testCases := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty config",
			config:      &Config{},
			wantErr:     true,
			errContains: "mode is required",
		},
		{
			name:        "invalid mode",
			config:      &Config{Mode: "invalid"},
			wantErr:     true,
			errContains: "invalid mode",
		},
		{
			name: "server mode without server config",
			config: &Config{
				Mode: ModeServer,
			},
			wantErr:     true,
			errContains: "server configuration is required",
		},
		{
			name: "client mode without client config",
			config: &Config{
				Mode: ModeClient,
			},
			wantErr:     true,
			errContains: "client configuration is required",
		},
		{
			name: "valid server config",
			config: &Config{
				Mode: ModeServer,
				Server: &ServerConfig{
					Listen: ":8080",
					MCPServer: MCPServerConfig{
						Command: "/usr/bin/mcp",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid client config",
			config: &Config{
				Mode: ModeClient,
				Client: &ClientConfig{
					URL: "http://localhost:8080",
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.config)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain '%s', got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestValidateTLS_TableDriven(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)

	testCases := []struct {
		name        string
		tlsConfig   *TLSConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "missing cert_file",
			tlsConfig: &TLSConfig{
				Enabled: true,
				KeyFile: keyFile,
			},
			wantErr:     true,
			errContains: "cert_file is required",
		},
		{
			name: "missing key_file",
			tlsConfig: &TLSConfig{
				Enabled:  true,
				CertFile: certFile,
			},
			wantErr:     true,
			errContains: "key_file is required",
		},
		{
			name: "invalid client_auth",
			tlsConfig: &TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "invalid",
			},
			wantErr:     true,
			errContains: "invalid client_auth",
		},
		{
			name: "valid client_auth - none",
			tlsConfig: &TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "none",
			},
			wantErr: false,
		},
		{
			name: "valid client_auth - request",
			tlsConfig: &TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "request",
			},
			wantErr: false,
		},
		{
			name: "valid client_auth - require",
			tlsConfig: &TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "require",
			},
			wantErr: false,
		},
		{
			name: "valid client_auth - verify",
			tlsConfig: &TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "verify",
			},
			wantErr: false,
		},
		{
			name: "invalid min_version",
			tlsConfig: &TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				MinVersion: "1.0",
			},
			wantErr:     true,
			errContains: "invalid min_version",
		},
		{
			name: "valid config with TLS 1.3",
			tlsConfig: &TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				MinVersion: "1.3",
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTLS(tc.tlsConfig)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain '%s', got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestValidateCORS_TableDriven(t *testing.T) {
	testCases := []struct {
		name        string
		corsConfig  *CORSConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "missing allowed_origins",
			corsConfig: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{},
			},
			wantErr:     true,
			errContains: "allowed_origins is required",
		},
		{
			name: "invalid origin format",
			corsConfig: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"example.com"},
			},
			wantErr:     true,
			errContains: "invalid origin",
		},
		{
			name: "valid wildcard origin",
			corsConfig: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
			},
			wantErr: false,
		},
		{
			name: "valid http origin",
			corsConfig: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"http://localhost:3000"},
			},
			wantErr: false,
		},
		{
			name: "valid https origin",
			corsConfig: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"https://example.com"},
			},
			wantErr: false,
		},
		{
			name: "mixed valid origins",
			corsConfig: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"*", "http://localhost:3000", "https://example.com"},
			},
			wantErr: false,
		},
		{
			name: "one invalid origin among valid ones",
			corsConfig: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"https://example.com", "invalid.com"},
			},
			wantErr:     true,
			errContains: "invalid origin: invalid.com",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCORS(tc.corsConfig)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain '%s', got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestValidateMCPServer_TableDriven(t *testing.T) {
	testCases := []struct {
		name        string
		mcpConfig   *MCPServerConfig
		wantErr     bool
		errContains string
	}{
		{
			name:        "missing command",
			mcpConfig:   &MCPServerConfig{},
			wantErr:     true,
			errContains: "command is required",
		},
		{
			name: "empty command",
			mcpConfig: &MCPServerConfig{
				Command: "",
			},
			wantErr:     true,
			errContains: "command is required",
		},
		{
			name: "whitespace-only command",
			mcpConfig: &MCPServerConfig{
				Command: "   ",
			},
			wantErr:     true,
			errContains: "command cannot be empty",
		},
		{
			name: "valid command",
			mcpConfig: &MCPServerConfig{
				Command: "/usr/bin/mcp",
			},
			wantErr: false,
		},
		{
			name: "valid command with args",
			mcpConfig: &MCPServerConfig{
				Command: "mcp-server",
				Args:    []string{"--port", "8080"},
			},
			wantErr: false,
		},
		{
			name: "valid command with env",
			mcpConfig: &MCPServerConfig{
				Command: "/usr/bin/mcp",
				Env: map[string]string{
					"DEBUG": "true",
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMCPServer(tc.mcpConfig)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain '%s', got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// ===========================================================================
// Edge Cases and Integration Tests
// ===========================================================================

func TestLoad_CompleteServerConfig(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `mode: server
server:
  listen: ":8443"
  read_timeout: 45s
  write_timeout: 90s
  idle_timeout: 180s
  tls:
    enabled: true
    cert_file: ` + certFile + `
    key_file: ` + keyFile + `
    min_version: "1.2"
    client_auth: require
  cors:
    enabled: true
    allowed_origins:
      - "https://example.com"
    allowed_methods:
      - GET
      - POST
    allow_credentials: true
  auth:
    type: bearer
    bearer:
      valid_tokens:
        - "token1"
        - "token2"
  mcp_server:
    command: /usr/bin/mcp
    args:
      - --config
      - /etc/mcp/config.json
    env:
      DEBUG: "true"
    dir: /var/run/mcp
    graceful_shutdown_timeout: 60s
    restart_on_failure: true
    max_restarts: 10
    restart_delay: 10s
  session:
    enabled: true
    timeout: 1h
    max_sessions: 500
    cleanup_interval: 10m
log:
  level: debug
  format: json
  output: /var/log/mcp.log
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load complete server config: %v", err)
	}

	// Verify all fields are set correctly
	if !cfg.Server.TLS.Enabled {
		t.Error("expected TLS to be enabled")
	}
	if cfg.Server.TLS.MinVersion != "1.2" {
		t.Errorf("expected TLS min_version '1.2', got '%s'", cfg.Server.TLS.MinVersion)
	}
	if cfg.Server.TLS.ClientAuth != "require" {
		t.Errorf("expected TLS client_auth 'require', got '%s'", cfg.Server.TLS.ClientAuth)
	}
	if !cfg.Server.CORS.AllowCredentials {
		t.Error("expected CORS allow_credentials to be true")
	}
	if cfg.Server.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got '%s'", cfg.Server.Auth.Type)
	}
	if len(cfg.Server.Auth.Bearer.ValidTokens) != 2 {
		t.Errorf("expected 2 valid tokens, got %d", len(cfg.Server.Auth.Bearer.ValidTokens))
	}
}

func TestLoad_CompleteClientConfig(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(caFile, []byte("ca content"), 0644)
	os.WriteFile(certFile, []byte("cert content"), 0644)
	os.WriteFile(keyFile, []byte("key content"), 0644)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `mode: client
client:
  url: https://mcp.example.com:8443/mcp
  timeout: 60s
  max_idle_conns: 20
  idle_conn_timeout: 120s
  tls:
    ca_cert: ` + caFile + `
    cert_file: ` + certFile + `
    key_file: ` + keyFile + `
    server_name: mcp.example.com
  auth:
    type: oauth
    oauth:
      client_id: my-client
      client_secret: my-secret
      token_url: https://auth.example.com/oauth/token
      scopes:
        - read
        - write
      use_pkce: true
  retry:
    enabled: true
    max_retries: 5
    initial_delay: 200ms
    max_delay: 10s
    multiplier: 2.5
log:
  level: info
  format: text
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load complete client config: %v", err)
	}

	// Verify all fields are set correctly
	if cfg.Client.TLS.ServerName != "mcp.example.com" {
		t.Errorf("expected server_name 'mcp.example.com', got '%s'", cfg.Client.TLS.ServerName)
	}
	if cfg.Client.Auth.Type != "oauth" {
		t.Errorf("expected auth type 'oauth', got '%s'", cfg.Client.Auth.Type)
	}
	if !cfg.Client.Auth.OAuth.UsePKCE {
		t.Error("expected use_pkce to be true")
	}
	if len(cfg.Client.Auth.OAuth.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(cfg.Client.Auth.OAuth.Scopes))
	}
	if cfg.Client.Retry.Multiplier != 2.5 {
		t.Errorf("expected multiplier 2.5, got %f", cfg.Client.Retry.Multiplier)
	}
}

func TestLoad_YAMLParseErrors(t *testing.T) {
	testCases := []struct {
		name    string
		content string
	}{
		{
			name:    "invalid indentation",
			content: "mode: server\n  server:\nlisten: :8080",
		},
		{
			name:    "unclosed quote",
			content: "mode: \"server",
		},
		{
			name:    "tab character issues",
			content: "mode: server\n\tserver:\n\t\tlisten: :8080",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to write temp config: %v", err)
			}

			_, err := Load(configPath)
			if err == nil {
				t.Fatal("expected parsing error")
			}
		})
	}
}

func TestMode_Constants(t *testing.T) {
	if ModeServer != "server" {
		t.Errorf("expected ModeServer to be 'server', got '%s'", ModeServer)
	}
	if ModeClient != "client" {
		t.Errorf("expected ModeClient to be 'client', got '%s'", ModeClient)
	}
}

func TestConfigTypes_Initialization(t *testing.T) {
	// Test that all types can be initialized properly
	cfg := &Config{
		Mode: ModeServer,
		Server: &ServerConfig{
			Listen:       ":8080",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
			TLS: &TLSConfig{
				Enabled:    true,
				CertFile:   "/path/to/cert.pem",
				KeyFile:    "/path/to/key.pem",
				MinVersion: "1.2",
				MaxVersion: "1.3",
				ClientAuth: "require",
				ClientCA:   "/path/to/ca.pem",
			},
			CORS: &CORSConfig{
				Enabled:          true,
				AllowedOrigins:   []string{"*"},
				AllowedMethods:   []string{"GET", "POST"},
				AllowedHeaders:   []string{"Content-Type"},
				ExposedHeaders:   []string{"X-Custom-Header"},
				AllowCredentials: true,
				MaxAge:           86400,
			},
			Auth: &AuthConfig{
				Type: "bearer",
				Bearer: &BearerAuthConfig{
					ValidTokens:        []string{"token1"},
					ValidationEndpoint: "https://auth.example.com/validate",
					Token:              "my-token",
					TokenEnv:           "MY_TOKEN",
				},
			},
			MCPServer: MCPServerConfig{
				Command:                 "/usr/bin/mcp",
				Args:                    []string{"--config", "/etc/mcp.json"},
				Env:                     map[string]string{"DEBUG": "true"},
				Dir:                     "/var/run/mcp",
				GracefulShutdownTimeout: 30 * time.Second,
				RestartOnFailure:        true,
				MaxRestarts:             5,
				RestartDelay:            5 * time.Second,
			},
			Session: SessionConfig{
				Enabled:         true,
				Timeout:         30 * time.Minute,
				MaxSessions:     100,
				CleanupInterval: 5 * time.Minute,
			},
		},
		Client: &ClientConfig{
			URL:             "https://mcp.example.com",
			Timeout:         30 * time.Second,
			MaxIdleConns:    10,
			IdleConnTimeout: 90 * time.Second,
			TLS: &TLSClientConfig{
				CACert:             "/path/to/ca.pem",
				CertFile:           "/path/to/cert.pem",
				KeyFile:            "/path/to/key.pem",
				InsecureSkipVerify: false,
				ServerName:         "mcp.example.com",
			},
			Auth: &AuthConfig{
				Type: "oauth",
				OAuth: &OAuthConfig{
					DiscoveryURL:     "https://auth.example.com/.well-known/openid-configuration",
					AuthorizationURL: "https://auth.example.com/authorize",
					TokenURL:         "https://auth.example.com/token",
					ClientID:         "my-client",
					ClientSecret:     "my-secret",
					Scopes:           []string{"read", "write"},
					IntrospectionURL: "https://auth.example.com/introspect",
					JWKSURL:          "https://auth.example.com/jwks",
					Resource:         "https://api.example.com",
					UsePKCE:          true,
				},
			},
			Retry: RetryConfig{
				Enabled:      true,
				MaxRetries:   3,
				InitialDelay: 100 * time.Millisecond,
				MaxDelay:     5 * time.Second,
				Multiplier:   2.0,
			},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
			Output: "/var/log/mcp.log",
		},
	}

	// Just verify the struct was created properly
	if cfg.Server.TLS.MinVersion != "1.2" {
		t.Errorf("expected MinVersion '1.2', got '%s'", cfg.Server.TLS.MinVersion)
	}
	if cfg.Client.Auth.OAuth.ClientID != "my-client" {
		t.Errorf("expected ClientID 'my-client', got '%s'", cfg.Client.Auth.OAuth.ClientID)
	}
}
