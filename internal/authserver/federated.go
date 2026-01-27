/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package authserver

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// FederatedAuthenticator handles authentication via upstream IdP
type FederatedAuthenticator struct {
	cfg        *config.FederatedAuthConfig
	oidcClient *OIDCClient
	httpClient *http.Client
}

// FederatedUserInfo represents the authenticated user from upstream
type FederatedUserInfo struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Scopes        []string

	// Upstream tokens for potential passthrough
	UpstreamAccessToken  string
	UpstreamRefreshToken string
	UpstreamIDToken      string
}

// NewFederatedAuthenticator creates a new federated authenticator
func NewFederatedAuthenticator(cfg *config.FederatedAuthConfig) (*FederatedAuthenticator, error) {
	if cfg.UpstreamIssuer == "" {
		return nil, fmt.Errorf("upstream_issuer is required for federated mode")
	}

	if cfg.ClientID == "" {
		return nil, fmt.Errorf("client_id is required for federated mode")
	}

	// Get client secret from config or environment
	clientSecret := cfg.ClientSecret
	if clientSecret == "" && cfg.ClientSecretEnv != "" {
		clientSecret = os.Getenv(cfg.ClientSecretEnv)
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("client_secret or client_secret_env is required for federated mode")
	}

	// Store the resolved secret back (in memory only)
	resolvedCfg := *cfg
	resolvedCfg.ClientSecret = clientSecret

	return &FederatedAuthenticator{
		cfg:        &resolvedCfg,
		oidcClient: NewOIDCClient(cfg.UpstreamIssuer),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// GetAuthorizationURL returns the URL to redirect the user to for authentication
func (f *FederatedAuthenticator) GetAuthorizationURL(ctx context.Context, state, nonce, redirectURI string) (string, error) {
	disc, err := f.oidcClient.Discover(ctx)
	if err != nil {
		return "", fmt.Errorf("discovering upstream IdP: %w", err)
	}

	params := url.Values{}
	params.Set("client_id", f.cfg.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	params.Set("nonce", nonce)

	// Request scopes
	scopes := f.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	params.Set("scope", strings.Join(scopes, " "))

	return disc.AuthorizationEndpoint + "?" + params.Encode(), nil
}

// ExchangeCode exchanges an authorization code for tokens
func (f *FederatedAuthenticator) ExchangeCode(ctx context.Context, code, redirectURI string) (*UpstreamTokenResponse, error) {
	disc, err := f.oidcClient.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering upstream IdP: %w", err)
	}

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", f.cfg.ClientID)
	data.Set("client_secret", f.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp TokenErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("token exchange failed: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	var tokenResp UpstreamTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}

	return &tokenResp, nil
}

// ValidateIDToken validates an ID token and returns the claims
func (f *FederatedAuthenticator) ValidateIDToken(ctx context.Context, idToken, nonce string) (*IDTokenClaims, error) {
	// Split the token
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid ID token format")
	}

	// Decode header to get kid
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decoding token header: %w", err)
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parsing token header: %w", err)
	}

	// Decode claims
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding token claims: %w", err)
	}

	var claims IDTokenClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("parsing token claims: %w", err)
	}

	// Validate issuer
	if claims.Issuer != f.cfg.UpstreamIssuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", f.cfg.UpstreamIssuer, claims.Issuer)
	}

	// Validate audience
	audiences := claims.GetAudience()
	if !slices.Contains(audiences, f.cfg.ClientID) {
		return nil, fmt.Errorf("token audience does not include client_id")
	}

	// Validate expiration
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token has expired")
	}

	// Validate nonce if provided
	if nonce != "" && claims.Nonce != nonce {
		return nil, fmt.Errorf("nonce mismatch")
	}

	// Verify signature
	if err := f.verifySignature(ctx, idToken, header.Kid, header.Alg); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	return &claims, nil
}

// verifySignature verifies the token signature using the upstream JWKS
func (f *FederatedAuthenticator) verifySignature(ctx context.Context, token, kid, alg string) error {
	jwks, err := f.oidcClient.GetJWKS(ctx)
	if err != nil {
		return fmt.Errorf("fetching JWKS: %w", err)
	}

	// Find the key
	var key *JWK
	for i := range jwks.Keys {
		if jwks.Keys[i].Kid == kid {
			key = &jwks.Keys[i]
			break
		}
	}
	if key == nil {
		return fmt.Errorf("key %s not found in JWKS", kid)
	}

	// Verify based on algorithm
	switch alg {
	case "RS256", "RS384", "RS512":
		pubKey, err := key.GetRSAPublicKey()
		if err != nil {
			return err
		}
		return f.verifyRSASignature(token, pubKey, alg)
	default:
		return fmt.Errorf("unsupported algorithm: %s", alg)
	}
}

// verifyRSASignature verifies an RSA signature
func (f *FederatedAuthenticator) verifyRSASignature(token string, pubKey *rsa.PublicKey, alg string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid token format")
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}

	// Hash the signed portion
	signedData := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(signedData))

	// Verify signature
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)
}

// Authenticate processes the callback from the upstream IdP
func (f *FederatedAuthenticator) Authenticate(ctx context.Context, code, nonce, redirectURI string) (*FederatedUserInfo, error) {
	// Exchange code for tokens
	tokenResp, err := f.ExchangeCode(ctx, code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("exchanging authorization code: %w", err)
	}

	if tokenResp.IDToken == "" {
		return nil, fmt.Errorf("no ID token in response")
	}

	// Validate the ID token
	claims, err := f.ValidateIDToken(ctx, tokenResp.IDToken, nonce)
	if err != nil {
		return nil, fmt.Errorf("validating ID token: %w", err)
	}

	// Check domain restrictions
	if len(f.cfg.AllowedDomains) > 0 {
		if !f.isAllowedDomain(claims.Email) {
			return nil, fmt.Errorf("email domain not allowed")
		}
	}

	// Determine scopes based on user
	scopes := f.determineScopes(claims)

	return &FederatedUserInfo{
		Subject:              claims.Subject,
		Email:                claims.Email,
		EmailVerified:        claims.EmailVerified,
		Name:                 claims.Name,
		Scopes:               scopes,
		UpstreamAccessToken:  tokenResp.AccessToken,
		UpstreamRefreshToken: tokenResp.RefreshToken,
		UpstreamIDToken:      tokenResp.IDToken,
	}, nil
}

// isAllowedDomain checks if the email domain is allowed
func (f *FederatedAuthenticator) isAllowedDomain(email string) bool {
	if email == "" {
		return false
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	domain := strings.ToLower(parts[1])

	for _, allowed := range f.cfg.AllowedDomains {
		if strings.ToLower(allowed) == domain {
			return true
		}
	}

	return false
}

// determineScopes determines which scopes to grant based on user
func (f *FederatedAuthenticator) determineScopes(claims *IDTokenClaims) []string {
	scopes := make([]string, len(f.cfg.DefaultScopes))
	copy(scopes, f.cfg.DefaultScopes)

	// Check if user is an admin
	identifier := claims.Email
	if identifier == "" {
		identifier = claims.Subject
	}

	for _, admin := range f.cfg.AdminUsers {
		if admin == identifier || admin == claims.Subject {
			// Add admin scopes
			for _, scope := range f.cfg.AdminScopes {
				if !slices.Contains(scopes, scope) {
					scopes = append(scopes, scope)
				}
			}
			break
		}
	}

	return scopes
}
