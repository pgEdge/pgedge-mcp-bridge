/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// BearerAuthenticator implements bearer token authentication.
// In server mode, it validates incoming tokens against a list of valid tokens
// or by calling a validation endpoint.
// In client mode, it adds a configured bearer token to outgoing requests.
type BearerAuthenticator struct {
	// isServer indicates whether this is server mode (validate) or client mode (authenticate)
	isServer bool

	// validTokens is a map of valid tokens for quick lookup (server mode)
	validTokens map[string]bool

	// validationEndpoint is an optional URL to validate tokens (server mode)
	validationEndpoint string

	// token is the bearer token to use for authentication (client mode)
	token string

	// tokenEnv is the environment variable name containing the token
	tokenEnv string

	// httpClient is used for validation endpoint calls
	httpClient *http.Client

	// mu protects token field for potential refresh
	mu sync.RWMutex
}

// NewBearerAuthenticator creates a new bearer token authenticator.
// In server mode, tokens are validated against validTokens list or validation endpoint.
// In client mode, the configured token is added to outgoing requests.
func NewBearerAuthenticator(cfg *config.BearerAuthConfig, isServer bool) (*BearerAuthenticator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: bearer config is nil", ErrInvalidConfiguration)
	}

	ba := &BearerAuthenticator{
		isServer:           isServer,
		validationEndpoint: cfg.ValidationEndpoint,
		tokenEnv:           cfg.TokenEnv,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	if isServer {
		// Server mode: set up token validation
		if len(cfg.ValidTokens) == 0 && cfg.ValidationEndpoint == "" {
			return nil, fmt.Errorf("%w: server mode requires valid_tokens or validation_endpoint", ErrInvalidConfiguration)
		}

		// Build token lookup map for O(1) validation
		ba.validTokens = make(map[string]bool, len(cfg.ValidTokens))
		for _, t := range cfg.ValidTokens {
			ba.validTokens[t] = true
		}
	} else {
		// Client mode: get the token to use
		token := cfg.Token

		// If tokenEnv is specified, try to get token from environment
		if cfg.TokenEnv != "" {
			envToken := os.Getenv(cfg.TokenEnv)
			if envToken != "" {
				token = envToken
			}
		}

		if token == "" {
			return nil, fmt.Errorf("%w: client mode requires token or token_env", ErrInvalidConfiguration)
		}

		ba.token = token
	}

	return ba, nil
}

// Validate checks the bearer token in an incoming request (server mode).
// Returns the authenticated principal on success.
func (ba *BearerAuthenticator) Validate(ctx context.Context, req *http.Request) (*Principal, error) {
	if !ba.isServer {
		return nil, fmt.Errorf("Validate called on client-mode authenticator")
	}

	// Extract token from Authorization header
	token, err := extractBearerToken(req)
	if err != nil {
		return nil, err
	}

	// First, check against local valid tokens list
	ba.mu.RLock()
	tokens := ba.validTokens
	ba.mu.RUnlock()

	if len(tokens) > 0 {
		if ba.validateTokenLocally(token, tokens) {
			return ba.createPrincipal(token, nil), nil
		}
	}

	// If validation endpoint is configured, check there
	if ba.validationEndpoint != "" {
		principal, err := ba.validateTokenRemotely(ctx, token)
		if err != nil {
			return nil, err
		}
		return principal, nil
	}

	// Token not found in valid tokens and no validation endpoint
	return nil, NewAuthError(ErrInvalidToken, "token not recognized", "MCP Bridge", "Bearer")
}

// validateTokenLocally checks if the token is in the valid tokens list.
// Uses constant-time comparison to prevent timing attacks.
func (ba *BearerAuthenticator) validateTokenLocally(token string, tokens map[string]bool) bool {
	for validToken := range tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(validToken)) == 1 {
			return true
		}
	}
	return false
}

// validateTokenRemotely validates the token by calling the validation endpoint.
func (ba *BearerAuthenticator) validateTokenRemotely(ctx context.Context, token string) (*Principal, error) {
	// Build validation request
	reqBody := struct {
		Token string `json:"token"`
	}{
		Token: token,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling validation request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ba.validationEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating validation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ba.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling validation endpoint: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("reading validation response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, NewAuthError(ErrInvalidToken, "token validation failed", "MCP Bridge", "Bearer")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("validation endpoint returned status %d", resp.StatusCode)
	}

	// Parse validation response
	var validationResp struct {
		Valid    bool                   `json:"valid"`
		Subject  string                 `json:"subject,omitempty"`
		Scopes   []string               `json:"scopes,omitempty"`
		Claims   map[string]interface{} `json:"claims,omitempty"`
		Metadata map[string]string      `json:"metadata,omitempty"`
	}

	if err := json.Unmarshal(respBody, &validationResp); err != nil {
		return nil, fmt.Errorf("parsing validation response: %w", err)
	}

	if !validationResp.Valid {
		return nil, NewAuthError(ErrInvalidToken, "token validation failed", "MCP Bridge", "Bearer")
	}

	return ba.createPrincipal(token, &validationResp), nil
}

// createPrincipal creates a Principal from a validated token.
func (ba *BearerAuthenticator) createPrincipal(token string, validationResp interface{}) *Principal {
	p := &Principal{
		ID:       "token",
		Type:     PrincipalTypeToken,
		Claims:   make(map[string]interface{}),
		Scopes:   []string{},
		Metadata: make(map[string]string),
	}

	// If we have validation response data, use it
	if resp, ok := validationResp.(*struct {
		Valid    bool                   `json:"valid"`
		Subject  string                 `json:"subject,omitempty"`
		Scopes   []string               `json:"scopes,omitempty"`
		Claims   map[string]interface{} `json:"claims,omitempty"`
		Metadata map[string]string      `json:"metadata,omitempty"`
	}); ok && resp != nil {
		if resp.Subject != "" {
			p.ID = resp.Subject
			p.Type = PrincipalTypeService
		}
		if len(resp.Scopes) > 0 {
			p.Scopes = resp.Scopes
		}
		if resp.Claims != nil {
			p.Claims = resp.Claims
		}
		if resp.Metadata != nil {
			p.Metadata = resp.Metadata
		}
	}

	return p
}

// Authenticate adds the bearer token to an outgoing request (client mode).
func (ba *BearerAuthenticator) Authenticate(ctx context.Context, req *http.Request) error {
	if ba.isServer {
		return fmt.Errorf("Authenticate called on server-mode authenticator")
	}

	ba.mu.RLock()
	token := ba.token
	ba.mu.RUnlock()

	if token == "" {
		return ErrMissingCredentials
	}

	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// Refresh reloads the token from the environment variable if configured.
func (ba *BearerAuthenticator) Refresh(ctx context.Context) error {
	if ba.isServer {
		return nil // Nothing to refresh in server mode
	}

	if ba.tokenEnv == "" {
		return nil // No env var configured, nothing to refresh
	}

	envToken := os.Getenv(ba.tokenEnv)
	if envToken == "" {
		return ErrTokenRefreshFailed
	}

	ba.mu.Lock()
	ba.token = envToken
	ba.mu.Unlock()

	return nil
}

// Close releases resources held by the authenticator.
func (ba *BearerAuthenticator) Close() error {
	ba.httpClient.CloseIdleConnections()
	return nil
}

// extractBearerToken extracts the bearer token from the Authorization header.
func extractBearerToken(req *http.Request) (string, error) {
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		return "", NewAuthError(ErrMissingCredentials, "missing Authorization header", "MCP Bridge", "Bearer")
	}

	// Check for Bearer scheme (case-insensitive per RFC 7235)
	const bearerPrefix = "bearer "
	if len(authHeader) < len(bearerPrefix) {
		return "", NewAuthError(ErrInvalidToken, "invalid Authorization header format", "MCP Bridge", "Bearer")
	}

	if !strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
		return "", NewAuthError(ErrInvalidToken, "expected Bearer authentication scheme", "MCP Bridge", "Bearer")
	}

	token := strings.TrimSpace(authHeader[len(bearerPrefix):])
	if token == "" {
		return "", NewAuthError(ErrMissingCredentials, "empty bearer token", "MCP Bridge", "Bearer")
	}

	return token, nil
}

// UpdateValidTokens updates the list of valid tokens (server mode).
// This can be used to dynamically update tokens without restarting.
func (ba *BearerAuthenticator) UpdateValidTokens(tokens []string) {
	if !ba.isServer {
		return
	}

	newTokens := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		newTokens[t] = true
	}

	ba.mu.Lock()
	ba.validTokens = newTokens
	ba.mu.Unlock()
}

// SetToken updates the token for client mode authentication.
// This can be used to update the token without restarting.
func (ba *BearerAuthenticator) SetToken(token string) {
	if ba.isServer {
		return
	}

	ba.mu.Lock()
	ba.token = token
	ba.mu.Unlock()
}
