/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package authserver provides OAuth 2.0 Authorization Server functionality.
package authserver

import (
	"context"
	"time"
)

// Storage combines all storage interfaces for the OAuth server.
type Storage interface {
	AuthorizationCodeStorage
	RefreshTokenStorage
	ClientStorage
	Close() error
}

// AuthorizationCodeStorage manages OAuth authorization codes.
type AuthorizationCodeStorage interface {
	// StoreAuthorizationCode stores a new authorization code.
	StoreAuthorizationCode(ctx context.Context, code *AuthorizationCode) error

	// GetAuthorizationCode retrieves an authorization code by its value.
	// Returns nil if not found or expired.
	GetAuthorizationCode(ctx context.Context, code string) (*AuthorizationCode, error)

	// DeleteAuthorizationCode removes an authorization code (after use).
	DeleteAuthorizationCode(ctx context.Context, code string) error
}

// RefreshTokenStorage manages OAuth refresh tokens.
type RefreshTokenStorage interface {
	// StoreRefreshToken stores a new refresh token.
	StoreRefreshToken(ctx context.Context, token *RefreshToken) error

	// GetRefreshToken retrieves a refresh token by its value.
	// Returns nil if not found or expired.
	GetRefreshToken(ctx context.Context, token string) (*RefreshToken, error)

	// DeleteRefreshToken removes a refresh token (on revocation or rotation).
	DeleteRefreshToken(ctx context.Context, token string) error

	// DeleteRefreshTokensForUser removes all refresh tokens for a user.
	DeleteRefreshTokensForUser(ctx context.Context, userID string) error
}

// ClientStorage manages OAuth clients (for dynamic registration).
type ClientStorage interface {
	// StoreClient stores a new OAuth client.
	StoreClient(ctx context.Context, client *Client) error

	// GetClient retrieves a client by ID.
	// Returns nil if not found.
	GetClient(ctx context.Context, clientID string) (*Client, error)

	// DeleteClient removes a client registration.
	DeleteClient(ctx context.Context, clientID string) error
}

// AuthorizationCode represents a pending authorization code.
type AuthorizationCode struct {
	// Code is the authorization code value.
	Code string

	// ClientID is the client that requested the code.
	ClientID string

	// UserID is the authenticated user.
	UserID string

	// Username for display purposes.
	Username string

	// RedirectURI is where to send the response.
	RedirectURI string

	// Scope is the granted scope (space-separated).
	Scope string

	// CodeChallenge is the PKCE challenge.
	CodeChallenge string

	// CodeChallengeMethod is the PKCE method (S256).
	CodeChallengeMethod string

	// State is the client's state parameter.
	State string

	// Nonce is the OpenID Connect nonce (if provided).
	Nonce string

	// ExpiresAt is when this code expires.
	ExpiresAt time.Time

	// CreatedAt is when this code was created.
	CreatedAt time.Time

	// For federated mode: upstream tokens to associate with this code
	UpstreamAccessToken  string
	UpstreamRefreshToken string
	UpstreamIDToken      string
}

// IsExpired returns true if the authorization code has expired.
func (ac *AuthorizationCode) IsExpired() bool {
	return time.Now().After(ac.ExpiresAt)
}

// RefreshToken represents an issued refresh token.
type RefreshToken struct {
	// Token is the refresh token value.
	Token string

	// ClientID is the client this token was issued to.
	ClientID string

	// UserID is the user this token belongs to.
	UserID string

	// Username for display purposes.
	Username string

	// Scope is the granted scope.
	Scope string

	// ExpiresAt is when this token expires.
	ExpiresAt time.Time

	// CreatedAt is when this token was created.
	CreatedAt time.Time

	// For federated mode: upstream refresh token
	UpstreamRefreshToken string
}

// IsExpired returns true if the refresh token has expired.
func (rt *RefreshToken) IsExpired() bool {
	return time.Now().After(rt.ExpiresAt)
}

// Client represents a registered OAuth client.
type Client struct {
	// ClientID is the unique client identifier.
	ClientID string

	// ClientSecret is the client secret (hashed for confidential clients).
	ClientSecret string

	// ClientName is a human-readable name.
	ClientName string

	// RedirectURIs are the allowed redirect URIs.
	RedirectURIs []string

	// GrantTypes are the allowed grant types.
	GrantTypes []string

	// TokenEndpointAuthMethod is how the client authenticates at the token endpoint.
	TokenEndpointAuthMethod string

	// CreatedAt is when this client was registered.
	CreatedAt time.Time
}

// IsPublicClient returns true if this is a public client (no secret).
func (c *Client) IsPublicClient() bool {
	return c.TokenEndpointAuthMethod == "none" || c.ClientSecret == ""
}

// HasRedirectURI checks if the given URI is in the allowed list.
func (c *Client) HasRedirectURI(uri string) bool {
	for _, allowed := range c.RedirectURIs {
		if allowed == uri {
			return true
		}
	}
	return false
}

// HasGrantType checks if the given grant type is allowed.
func (c *Client) HasGrantType(grantType string) bool {
	for _, allowed := range c.GrantTypes {
		if allowed == grantType {
			return true
		}
	}
	return false
}
