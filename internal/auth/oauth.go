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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// OAuthAuthenticator implements OAuth 2.0/2.1 authentication.
// In server mode, it validates JWT tokens using JWKS or introspection endpoint.
// In client mode, it obtains and refreshes access tokens using client credentials flow.
type OAuthAuthenticator struct {
	// isServer indicates whether this is server mode (validate) or client mode (authenticate)
	isServer bool

	// config holds the OAuth configuration
	config *config.OAuthConfig

	// oidcProvider is used for OIDC discovery and token verification (server mode)
	oidcProvider *oidc.Provider

	// oidcVerifier is used to verify JWT tokens (server mode)
	oidcVerifier *oidc.IDTokenVerifier

	// oauth2Config is used for client credentials flow (client mode)
	oauth2Config *clientcredentials.Config

	// tokenSource provides cached, auto-refreshing tokens (client mode)
	tokenSource oauth2.TokenSource

	// currentToken holds the current access token (client mode)
	currentToken *oauth2.Token

	// httpClient is used for HTTP requests
	httpClient *http.Client

	// mu protects token state
	mu sync.RWMutex

	// pkceVerifier holds the PKCE code verifier for the authorization code flow
	pkceVerifier string
}

// OAuthDiscovery represents the OAuth/OIDC discovery document.
type OAuthDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	IntrospectionEndpoint string `json:"introspection_endpoint,omitempty"`
	UserinfoEndpoint      string `json:"userinfo_endpoint,omitempty"`
}

// NewOAuthAuthenticator creates a new OAuth authenticator.
func NewOAuthAuthenticator(cfg *config.OAuthConfig, isServer bool) (*OAuthAuthenticator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: oauth config is nil", ErrInvalidConfiguration)
	}

	httpTimeout := cfg.HTTPTimeout
	if httpTimeout == 0 {
		httpTimeout = 30 * time.Second
	}

	oa := &OAuthAuthenticator{
		isServer: isServer,
		config:   cfg,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}

	ctx := context.Background()

	if isServer {
		// Server mode: set up token verification
		if err := oa.setupServerMode(ctx); err != nil {
			return nil, err
		}
	} else {
		// Client mode: set up client credentials flow
		if err := oa.setupClientMode(ctx); err != nil {
			return nil, err
		}
	}

	return oa, nil
}

// setupServerMode configures the authenticator for validating incoming tokens.
func (oa *OAuthAuthenticator) setupServerMode(ctx context.Context) error {
	// If discovery URL is provided, use OIDC provider
	if oa.config.DiscoveryURL != "" {
		provider, err := oidc.NewProvider(ctx, oa.config.DiscoveryURL)
		if err != nil {
			return fmt.Errorf("creating OIDC provider: %w", err)
		}
		oa.oidcProvider = provider

		// Create verifier with client ID as audience
		oa.oidcVerifier = provider.Verifier(&oidc.Config{
			ClientID: oa.config.ClientID,
		})

		return nil
	}

	// If JWKS URL is provided without discovery, we need manual setup
	if oa.config.JWKSURL != "" {
		// Create a key set from the JWKS URL
		keySet := oidc.NewRemoteKeySet(ctx, oa.config.JWKSURL)

		// Create a verifier config
		verifierConfig := &oidc.Config{
			ClientID:             oa.config.ClientID,
			SkipIssuerCheck:      true, // We don't have issuer from discovery
			SkipClientIDCheck:    oa.config.ClientID == "",
			SupportedSigningAlgs: []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"},
		}

		oa.oidcVerifier = oidc.NewVerifier("", keySet, verifierConfig)
		return nil
	}

	// Introspection-only mode
	if oa.config.IntrospectionURL != "" {
		return nil
	}

	return fmt.Errorf("%w: server mode requires discovery_url, jwks_url, or introspection_url", ErrInvalidConfiguration)
}

// setupClientMode configures the authenticator for obtaining access tokens.
func (oa *OAuthAuthenticator) setupClientMode(ctx context.Context) error {
	tokenURL := oa.config.TokenURL

	// If discovery URL is provided, fetch token endpoint
	if oa.config.DiscoveryURL != "" {
		discovery, err := oa.fetchDiscovery(ctx, oa.config.DiscoveryURL)
		if err != nil {
			return fmt.Errorf("fetching OAuth discovery: %w", err)
		}
		tokenURL = discovery.TokenEndpoint
	}

	if tokenURL == "" {
		return fmt.Errorf("%w: client mode requires token_url or discovery_url", ErrInvalidConfiguration)
	}

	// Set up client credentials config
	oa.oauth2Config = &clientcredentials.Config{
		ClientID:     oa.config.ClientID,
		ClientSecret: oa.config.ClientSecret,
		TokenURL:     tokenURL,
		Scopes:       oa.config.Scopes,
	}

	// Add resource parameter if specified (for RFC 8707 resource indicators)
	if oa.config.Resource != "" {
		oa.oauth2Config.EndpointParams = url.Values{
			"resource": {oa.config.Resource},
		}
	}

	// Create a token source that caches and auto-refreshes tokens
	oa.tokenSource = oa.oauth2Config.TokenSource(ctx)

	// Get initial token
	token, err := oa.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("obtaining initial access token: %w", err)
	}
	oa.currentToken = token

	return nil
}

// fetchDiscovery fetches the OAuth/OIDC discovery document.
func (oa *OAuthAuthenticator) fetchDiscovery(ctx context.Context, discoveryURL string) (*OAuthDiscovery, error) {
	// Normalize discovery URL
	if !strings.HasSuffix(discoveryURL, "/.well-known/openid-configuration") &&
		!strings.HasSuffix(discoveryURL, "/.well-known/oauth-authorization-server") {
		// Try standard OIDC discovery path first
		discoveryURL = strings.TrimSuffix(discoveryURL, "/") + "/.well-known/openid-configuration"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating discovery request: %w", err)
	}

	resp, err := oa.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("reading discovery document: %w", err)
	}

	var discovery OAuthDiscovery
	if err := json.Unmarshal(body, &discovery); err != nil {
		return nil, fmt.Errorf("parsing discovery document: %w", err)
	}

	return &discovery, nil
}

// Validate checks the OAuth token in an incoming request (server mode).
func (oa *OAuthAuthenticator) Validate(ctx context.Context, req *http.Request) (*Principal, error) {
	if !oa.isServer {
		return nil, fmt.Errorf("Validate called on client-mode authenticator")
	}

	// Extract token from Authorization header
	token, err := extractBearerToken(req)
	if err != nil {
		return nil, err
	}

	// Try JWT verification first if we have a verifier
	if oa.oidcVerifier != nil {
		principal, err := oa.validateJWT(ctx, token)
		if err == nil {
			return principal, nil
		}
		// If JWT validation fails and we have introspection, try that
		if oa.config.IntrospectionURL == "" {
			return nil, err
		}
	}

	// Try introspection endpoint
	if oa.config.IntrospectionURL != "" {
		return oa.validateByIntrospection(ctx, token)
	}

	return nil, NewAuthError(ErrInvalidToken, "no validation method available", "MCP Bridge", "Bearer")
}

// validateJWT validates a JWT token using the OIDC verifier.
func (oa *OAuthAuthenticator) validateJWT(ctx context.Context, token string) (*Principal, error) {
	idToken, err := oa.oidcVerifier.Verify(ctx, token)
	if err != nil {
		return nil, NewAuthError(ErrInvalidToken, "JWT verification failed", "MCP Bridge", "Bearer")
	}

	// Extract claims
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extracting claims: %w", err)
	}

	// Build principal
	principal := &Principal{
		ID:       idToken.Subject,
		Type:     PrincipalTypeService,
		Claims:   claims,
		Scopes:   []string{},
		Metadata: make(map[string]string),
	}

	// Extract scopes from scope claim
	if scopeClaim, ok := claims["scope"].(string); ok {
		principal.Scopes = strings.Split(scopeClaim, " ")
	} else if scopesClaim, ok := claims["scopes"].([]interface{}); ok {
		for _, s := range scopesClaim {
			if str, ok := s.(string); ok {
				principal.Scopes = append(principal.Scopes, str)
			}
		}
	}

	// Set metadata
	principal.Metadata["issuer"] = idToken.Issuer
	principal.Metadata["audience"] = strings.Join(idToken.Audience, ",")

	return principal, nil
}

// validateByIntrospection validates a token using the introspection endpoint.
func (oa *OAuthAuthenticator) validateByIntrospection(ctx context.Context, token string) (*Principal, error) {
	// Build introspection request
	data := url.Values{
		"token": {token},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oa.config.IntrospectionURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating introspection request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Add client credentials for introspection
	if oa.config.ClientID != "" && oa.config.ClientSecret != "" {
		req.SetBasicAuth(oa.config.ClientID, oa.config.ClientSecret)
	}

	resp, err := oa.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling introspection endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("introspection endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("reading introspection response: %w", err)
	}

	// Parse introspection response (RFC 7662)
	var introspectionResp struct {
		Active    bool                   `json:"active"`
		Scope     string                 `json:"scope,omitempty"`
		ClientID  string                 `json:"client_id,omitempty"`
		Username  string                 `json:"username,omitempty"`
		TokenType string                 `json:"token_type,omitempty"`
		Exp       int64                  `json:"exp,omitempty"`
		Iat       int64                  `json:"iat,omitempty"`
		Nbf       int64                  `json:"nbf,omitempty"`
		Sub       string                 `json:"sub,omitempty"`
		Aud       interface{}            `json:"aud,omitempty"` // Can be string or []string
		Iss       string                 `json:"iss,omitempty"`
		Jti       string                 `json:"jti,omitempty"`
		Extra     map[string]interface{} `json:"-"`
	}

	// First unmarshal to get standard fields
	if err := json.Unmarshal(body, &introspectionResp); err != nil {
		return nil, fmt.Errorf("parsing introspection response: %w", err)
	}

	// Also unmarshal to get extra fields as claims
	var claims map[string]interface{}
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("parsing introspection claims: %w", err)
	}

	if !introspectionResp.Active {
		return nil, NewAuthError(ErrInvalidToken, "token is not active", "MCP Bridge", "Bearer")
	}

	// Build principal
	principal := &Principal{
		ID:       introspectionResp.Sub,
		Type:     PrincipalTypeService,
		Claims:   claims,
		Scopes:   []string{},
		Metadata: make(map[string]string),
	}

	// Use username if subject is empty
	if principal.ID == "" {
		principal.ID = introspectionResp.Username
	}
	if principal.ID == "" {
		principal.ID = introspectionResp.ClientID
	}

	// Parse scopes
	if introspectionResp.Scope != "" {
		principal.Scopes = strings.Split(introspectionResp.Scope, " ")
	}

	// Set metadata
	if introspectionResp.Iss != "" {
		principal.Metadata["issuer"] = introspectionResp.Iss
	}
	if introspectionResp.ClientID != "" {
		principal.Metadata["client_id"] = introspectionResp.ClientID
	}
	if introspectionResp.Jti != "" {
		principal.Metadata["jti"] = introspectionResp.Jti
	}

	return principal, nil
}

// Authenticate adds the OAuth access token to an outgoing request (client mode).
func (oa *OAuthAuthenticator) Authenticate(ctx context.Context, req *http.Request) error {
	if oa.isServer {
		return fmt.Errorf("Authenticate called on server-mode authenticator")
	}

	// Get token (possibly refreshed)
	token, err := oa.getToken(ctx)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	return nil
}

// getToken returns the current access token, refreshing if necessary.
func (oa *OAuthAuthenticator) getToken(ctx context.Context) (*oauth2.Token, error) {
	oa.mu.RLock()
	token := oa.currentToken
	oa.mu.RUnlock()

	// Check if token is still valid
	if token != nil && token.Valid() {
		return token, nil
	}

	// Need to refresh
	oa.mu.Lock()
	defer oa.mu.Unlock()

	// Double-check after acquiring write lock
	if oa.currentToken != nil && oa.currentToken.Valid() {
		return oa.currentToken, nil
	}

	// Get new token
	newToken, err := oa.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}

	oa.currentToken = newToken
	return newToken, nil
}

// Refresh proactively refreshes the access token (client mode).
func (oa *OAuthAuthenticator) Refresh(ctx context.Context) error {
	if oa.isServer {
		return nil // Nothing to refresh in server mode
	}

	oa.mu.Lock()
	defer oa.mu.Unlock()

	newToken, err := oa.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTokenRefreshFailed, err)
	}

	oa.currentToken = newToken
	return nil
}

// Close releases resources held by the authenticator.
func (oa *OAuthAuthenticator) Close() error {
	oa.httpClient.CloseIdleConnections()
	return nil
}

// PKCE support functions

// GeneratePKCEVerifier generates a cryptographically random PKCE code verifier.
func GeneratePKCEVerifier() (string, error) {
	// Generate 32 random bytes (256 bits)
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}

	// Encode as URL-safe base64 without padding (RFC 7636)
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GeneratePKCEChallenge generates the PKCE code challenge from a verifier.
// Uses S256 method (SHA256 hash of the verifier).
func GeneratePKCEChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// SetupPKCE generates and stores a PKCE verifier for the authorization code flow.
// Returns the code challenge to include in the authorization request.
func (oa *OAuthAuthenticator) SetupPKCE() (challenge string, err error) {
	verifier, err := GeneratePKCEVerifier()
	if err != nil {
		return "", err
	}

	oa.mu.Lock()
	oa.pkceVerifier = verifier
	oa.mu.Unlock()

	return GeneratePKCEChallenge(verifier), nil
}

// GetPKCEVerifier returns the stored PKCE verifier for token exchange.
func (oa *OAuthAuthenticator) GetPKCEVerifier() string {
	oa.mu.RLock()
	defer oa.mu.RUnlock()
	return oa.pkceVerifier
}

// ClearPKCEVerifier clears the stored PKCE verifier after use.
func (oa *OAuthAuthenticator) ClearPKCEVerifier() {
	oa.mu.Lock()
	oa.pkceVerifier = ""
	oa.mu.Unlock()
}

// GetCurrentToken returns the current access token (for client mode).
// Returns nil if no token is available.
func (oa *OAuthAuthenticator) GetCurrentToken() *oauth2.Token {
	if oa.isServer {
		return nil
	}

	oa.mu.RLock()
	defer oa.mu.RUnlock()
	return oa.currentToken
}

// IsTokenExpiringSoon checks if the current token will expire within the given duration.
func (oa *OAuthAuthenticator) IsTokenExpiringSoon(within time.Duration) bool {
	if oa.isServer {
		return false
	}

	oa.mu.RLock()
	token := oa.currentToken
	oa.mu.RUnlock()

	if token == nil {
		return true
	}

	// Check if token expires within the given duration
	if token.Expiry.IsZero() {
		return false // No expiry set, assume valid
	}

	return time.Until(token.Expiry) < within
}

// BuildAuthorizationURL builds an OAuth authorization URL for the authorization code flow.
// This is useful for interactive authentication flows.
func (oa *OAuthAuthenticator) BuildAuthorizationURL(state, redirectURI string) (string, error) {
	authURL := oa.config.AuthorizationURL

	// If discovery URL is provided, fetch authorization endpoint
	if authURL == "" && oa.config.DiscoveryURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		discovery, err := oa.fetchDiscovery(ctx, oa.config.DiscoveryURL)
		if err != nil {
			return "", fmt.Errorf("fetching discovery: %w", err)
		}
		authURL = discovery.AuthorizationEndpoint
	}

	if authURL == "" {
		return "", fmt.Errorf("authorization URL not configured")
	}

	u, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("parsing authorization URL: %w", err)
	}

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", oa.config.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)

	if len(oa.config.Scopes) > 0 {
		q.Set("scope", strings.Join(oa.config.Scopes, " "))
	}

	if oa.config.Resource != "" {
		q.Set("resource", oa.config.Resource)
	}

	// Add PKCE challenge if enabled
	if oa.config.UsePKCE {
		challenge, err := oa.SetupPKCE()
		if err != nil {
			return "", fmt.Errorf("setting up PKCE: %w", err)
		}
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ExchangeAuthorizationCode exchanges an authorization code for tokens.
func (oa *OAuthAuthenticator) ExchangeAuthorizationCode(ctx context.Context, code, redirectURI string) (*oauth2.Token, error) {
	tokenURL := oa.config.TokenURL

	// If discovery URL is provided, fetch token endpoint
	if tokenURL == "" && oa.config.DiscoveryURL != "" {
		discovery, err := oa.fetchDiscovery(ctx, oa.config.DiscoveryURL)
		if err != nil {
			return nil, fmt.Errorf("fetching discovery: %w", err)
		}
		tokenURL = discovery.TokenEndpoint
	}

	if tokenURL == "" {
		return nil, fmt.Errorf("token URL not configured")
	}

	// Build token request
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {oa.config.ClientID},
	}

	// Add client secret if configured
	if oa.config.ClientSecret != "" {
		data.Set("client_secret", oa.config.ClientSecret)
	}

	// Add PKCE verifier if we have one
	if verifier := oa.GetPKCEVerifier(); verifier != "" {
		data.Set("code_verifier", verifier)
		oa.ClearPKCEVerifier() // Clear after use
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := oa.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse token response
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in,omitempty"`
		RefreshToken string `json:"refresh_token,omitempty"`
		Scope        string `json:"scope,omitempty"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	token := &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		RefreshToken: tokenResp.RefreshToken,
	}

	if tokenResp.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// Store the token for future use
	oa.mu.Lock()
	oa.currentToken = token
	oa.mu.Unlock()

	return token, nil
}
