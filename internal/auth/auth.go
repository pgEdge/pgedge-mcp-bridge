/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package auth provides authentication and authorization for the MCP HTTP bridge.
// It supports both server mode (validating incoming requests) and client mode
// (authenticating outgoing requests).
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// Common authentication errors
var (
	// ErrUnauthorized indicates the request lacks valid authentication credentials.
	ErrUnauthorized = errors.New("unauthorized: authentication required")

	// ErrForbidden indicates the authenticated principal lacks required permissions.
	ErrForbidden = errors.New("forbidden: insufficient permissions")

	// ErrTokenExpired indicates the provided token has expired.
	ErrTokenExpired = errors.New("token expired")

	// ErrInvalidToken indicates the provided token is malformed or invalid.
	ErrInvalidToken = errors.New("invalid token")

	// ErrMissingCredentials indicates no credentials were provided.
	ErrMissingCredentials = errors.New("missing credentials")

	// ErrInvalidConfiguration indicates the auth configuration is invalid.
	ErrInvalidConfiguration = errors.New("invalid authentication configuration")

	// ErrTokenRefreshFailed indicates token refresh failed.
	ErrTokenRefreshFailed = errors.New("token refresh failed")
)

// PrincipalType represents the type of authenticated principal.
type PrincipalType string

const (
	// PrincipalTypeUser represents a human user.
	PrincipalTypeUser PrincipalType = "user"

	// PrincipalTypeService represents a service or application.
	PrincipalTypeService PrincipalType = "service"

	// PrincipalTypeToken represents a static token without specific identity.
	PrincipalTypeToken PrincipalType = "token"
)

// Principal represents an authenticated entity.
type Principal struct {
	// ID is the unique identifier for this principal.
	ID string

	// Type indicates whether this is a user, service, or token.
	Type PrincipalType

	// Claims contains JWT claims or other identity assertions.
	Claims map[string]interface{}

	// Scopes contains the OAuth scopes or permissions granted.
	Scopes []string

	// Metadata contains additional authentication metadata.
	Metadata map[string]string
}

// HasScope returns true if the principal has the specified scope.
func (p *Principal) HasScope(scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasAnyScope returns true if the principal has any of the specified scopes.
func (p *Principal) HasAnyScope(scopes ...string) bool {
	for _, scope := range scopes {
		if p.HasScope(scope) {
			return true
		}
	}
	return false
}

// HasAllScopes returns true if the principal has all of the specified scopes.
func (p *Principal) HasAllScopes(scopes ...string) bool {
	for _, scope := range scopes {
		if !p.HasScope(scope) {
			return false
		}
	}
	return true
}

// GetClaim returns a claim value by key, or nil if not found.
func (p *Principal) GetClaim(key string) interface{} {
	if p.Claims == nil {
		return nil
	}
	return p.Claims[key]
}

// GetStringClaim returns a claim value as a string, or empty string if not found or wrong type.
func (p *Principal) GetStringClaim(key string) string {
	v := p.GetClaim(key)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Authenticator defines the interface for authentication providers.
// Implementations support either server mode (validating incoming requests)
// or client mode (authenticating outgoing requests), or both.
type Authenticator interface {
	// Validate checks the authentication credentials in an incoming request (server mode).
	// Returns the authenticated principal on success, or an error.
	// Common errors: ErrUnauthorized, ErrInvalidToken, ErrTokenExpired, ErrMissingCredentials
	Validate(ctx context.Context, req *http.Request) (*Principal, error)

	// Authenticate adds authentication credentials to an outgoing request (client mode).
	// Modifies the request in place by adding Authorization header or other credentials.
	Authenticate(ctx context.Context, req *http.Request) error

	// Refresh refreshes any cached credentials (e.g., OAuth access tokens).
	// This is primarily used in client mode to proactively refresh tokens before expiry.
	Refresh(ctx context.Context) error

	// Close releases any resources held by the authenticator.
	Close() error
}

// AuthType represents the type of authentication.
type AuthType string

const (
	// AuthTypeNone indicates no authentication.
	AuthTypeNone AuthType = "none"

	// AuthTypeBearer indicates bearer token authentication.
	AuthTypeBearer AuthType = "bearer"

	// AuthTypeOAuth indicates OAuth 2.0/2.1 authentication.
	AuthTypeOAuth AuthType = "oauth"
)

// NewAuthenticator creates an authenticator based on the provided configuration.
// The isServer parameter indicates whether this is for server mode (validating requests)
// or client mode (authenticating requests).
func NewAuthenticator(cfg *config.AuthConfig, isServer bool) (Authenticator, error) {
	if cfg == nil {
		return &noopAuthenticator{}, nil
	}

	switch AuthType(cfg.Type) {
	case AuthTypeNone, "":
		return &noopAuthenticator{}, nil

	case AuthTypeBearer:
		if cfg.Bearer == nil {
			return nil, fmt.Errorf("%w: bearer config is nil", ErrInvalidConfiguration)
		}
		return NewBearerAuthenticator(cfg.Bearer, isServer)

	case AuthTypeOAuth:
		if cfg.OAuth == nil {
			return nil, fmt.Errorf("%w: oauth config is nil", ErrInvalidConfiguration)
		}
		return NewOAuthAuthenticator(cfg.OAuth, isServer)

	default:
		return nil, fmt.Errorf("%w: unknown auth type %q", ErrInvalidConfiguration, cfg.Type)
	}
}

// noopAuthenticator is a no-op authenticator for when authentication is disabled.
type noopAuthenticator struct{}

func (n *noopAuthenticator) Validate(ctx context.Context, req *http.Request) (*Principal, error) {
	// No authentication required, return anonymous principal
	return &Principal{
		ID:       "anonymous",
		Type:     PrincipalTypeUser,
		Claims:   make(map[string]interface{}),
		Scopes:   []string{},
		Metadata: make(map[string]string),
	}, nil
}

func (n *noopAuthenticator) Authenticate(ctx context.Context, req *http.Request) error {
	// No authentication needed
	return nil
}

func (n *noopAuthenticator) Refresh(ctx context.Context) error {
	// Nothing to refresh
	return nil
}

func (n *noopAuthenticator) Close() error {
	return nil
}

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// principalContextKey is the context key for storing the authenticated principal.
	principalContextKey contextKey = "auth.principal"
)

// PrincipalFromContext retrieves the authenticated principal from the request context.
// Returns nil if no principal is set.
func PrincipalFromContext(ctx context.Context) *Principal {
	if p, ok := ctx.Value(principalContextKey).(*Principal); ok {
		return p
	}
	return nil
}

// ContextWithPrincipal returns a new context with the principal set.
func ContextWithPrincipal(ctx context.Context, principal *Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, principal)
}

// RequirePrincipal retrieves the principal from context and returns an error if not present.
func RequirePrincipal(ctx context.Context) (*Principal, error) {
	p := PrincipalFromContext(ctx)
	if p == nil {
		return nil, ErrUnauthorized
	}
	return p, nil
}

// AuthError wraps an authentication error with additional context.
type AuthError struct {
	// Err is the underlying error.
	Err error

	// Message provides additional context.
	Message string

	// Realm is the authentication realm for WWW-Authenticate header.
	Realm string

	// Scheme is the authentication scheme (e.g., "Bearer").
	Scheme string
}

// Error implements the error interface.
func (e *AuthError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *AuthError) Unwrap() error {
	return e.Err
}

// WWWAuthenticate returns the WWW-Authenticate header value for this error.
func (e *AuthError) WWWAuthenticate() string {
	scheme := e.Scheme
	if scheme == "" {
		scheme = "Bearer"
	}
	if e.Realm != "" {
		return fmt.Sprintf(`%s realm="%s"`, scheme, e.Realm)
	}
	return scheme
}

// NewAuthError creates a new AuthError.
func NewAuthError(err error, message, realm, scheme string) *AuthError {
	return &AuthError{
		Err:     err,
		Message: message,
		Realm:   realm,
		Scheme:  scheme,
	}
}

// IsAuthError returns true if the error is an authentication error.
func IsAuthError(err error) bool {
	var authErr *AuthError
	return errors.As(err, &authErr)
}
