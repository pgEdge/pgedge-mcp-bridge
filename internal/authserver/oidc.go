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
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// OIDCDiscovery represents the OpenID Connect discovery document
type OIDCDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint,omitempty"`
	JwksURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

// OIDCClient handles OIDC discovery and token operations
type OIDCClient struct {
	httpClient *http.Client
	issuer     string

	mu        sync.RWMutex
	discovery *OIDCDiscovery
	jwks      *JWKS
	jwksTime  time.Time
}

// JWKS represents a JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use,omitempty"`
	Kid string `json:"kid,omitempty"`
	Alg string `json:"alg,omitempty"`
	N   string `json:"n,omitempty"` // RSA modulus
	E   string `json:"e,omitempty"` // RSA exponent
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

// NewOIDCClient creates a new OIDC client for the given issuer.
// If httpTimeout is zero, a default of 30 seconds is used.
func NewOIDCClient(issuer string, httpTimeout time.Duration) *OIDCClient {
	if httpTimeout == 0 {
		httpTimeout = 30 * time.Second
	}
	return &OIDCClient{
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		issuer: issuer,
	}
}

// Discover fetches the OIDC discovery document
func (c *OIDCClient) Discover(ctx context.Context) (*OIDCDiscovery, error) {
	c.mu.RLock()
	if c.discovery != nil {
		disc := c.discovery
		c.mu.RUnlock()
		return disc, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.discovery != nil {
		return c.discovery, nil
	}

	discoveryURL := c.issuer + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating discovery request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery endpoint returned status %d", resp.StatusCode)
	}

	var disc OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return nil, fmt.Errorf("decoding discovery document: %w", err)
	}

	// Validate issuer matches
	if disc.Issuer != c.issuer {
		return nil, fmt.Errorf("issuer mismatch: expected %s, got %s", c.issuer, disc.Issuer)
	}

	c.discovery = &disc
	return &disc, nil
}

// GetJWKS fetches the JSON Web Key Set from the upstream IdP
func (c *OIDCClient) GetJWKS(ctx context.Context) (*JWKS, error) {
	c.mu.RLock()
	// Cache JWKS for 1 hour
	if c.jwks != nil && time.Since(c.jwksTime) < time.Hour {
		jwks := c.jwks
		c.mu.RUnlock()
		return jwks, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check
	if c.jwks != nil && time.Since(c.jwksTime) < time.Hour {
		return c.jwks, nil
	}

	// Need discovery first - release lock to avoid deadlock since
	// Discover() also acquires the lock.
	if c.discovery == nil {
		c.mu.Unlock()
		disc, err := c.Discover(ctx)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		// Re-check cache after reacquiring lock
		if c.jwks != nil && time.Since(c.jwksTime) < time.Hour {
			return c.jwks, nil
		}
		c.discovery = disc
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.discovery.JwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("creating JWKS request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decoding JWKS: %w", err)
	}

	c.jwks = &jwks
	c.jwksTime = time.Now()
	return &jwks, nil
}

// GetRSAPublicKey extracts an RSA public key from a JWK
func (j *JWK) GetRSAPublicKey() (*rsa.PublicKey, error) {
	if j.Kty != "RSA" {
		return nil, fmt.Errorf("key type is not RSA: %s", j.Kty)
	}

	// Decode modulus
	nBytes, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil {
		return nil, fmt.Errorf("decoding modulus: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)

	// Decode exponent
	eBytes, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil {
		return nil, fmt.Errorf("decoding exponent: %w", err)
	}
	e := int(new(big.Int).SetBytes(eBytes).Int64())

	return &rsa.PublicKey{N: n, E: e}, nil
}

// IDTokenClaims represents the claims in an OIDC ID token
type IDTokenClaims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  any    `json:"aud"` // Can be string or []string
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
	Nonce     string `json:"nonce,omitempty"`

	// Standard claims
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
	Name          string `json:"name,omitempty"`

	// For domain filtering
	HD string `json:"hd,omitempty"` // Google hosted domain
}

// GetAudience returns the audience as a slice of strings
func (c *IDTokenClaims) GetAudience() []string {
	switch v := c.Audience.(type) {
	case string:
		return []string{v}
	case []interface{}:
		result := make([]string, len(v))
		for i, aud := range v {
			result[i], _ = aud.(string)
		}
		return result
	default:
		return nil
	}
}

// UpstreamTokenResponse represents the response from the upstream token endpoint
type UpstreamTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// TokenErrorResponse represents an error from the token endpoint
type TokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}
