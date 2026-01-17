package auth

import (
	"errors"
	"log/slog"
	"net/http"
)

// AuthMiddleware provides HTTP middleware for authentication.
// It extracts credentials from incoming requests, validates them using
// the configured Authenticator, and sets the authenticated Principal
// in the request context.
type AuthMiddleware struct {
	// auth is the authenticator used to validate requests
	auth Authenticator

	// realm is the authentication realm for WWW-Authenticate header
	realm string

	// logger is used for logging authentication events
	logger *slog.Logger

	// skipPaths contains paths that should skip authentication
	skipPaths map[string]bool

	// onUnauthorized is called when authentication fails (optional)
	onUnauthorized func(w http.ResponseWriter, r *http.Request, err error)
}

// AuthMiddlewareOption is a functional option for configuring AuthMiddleware.
type AuthMiddlewareOption func(*AuthMiddleware)

// WithRealm sets the authentication realm for WWW-Authenticate header.
func WithRealm(realm string) AuthMiddlewareOption {
	return func(m *AuthMiddleware) {
		m.realm = realm
	}
}

// WithLogger sets the logger for the middleware.
func WithLogger(logger *slog.Logger) AuthMiddlewareOption {
	return func(m *AuthMiddleware) {
		m.logger = logger
	}
}

// WithSkipPaths sets paths that should skip authentication.
func WithSkipPaths(paths ...string) AuthMiddlewareOption {
	return func(m *AuthMiddleware) {
		m.skipPaths = make(map[string]bool, len(paths))
		for _, p := range paths {
			m.skipPaths[p] = true
		}
	}
}

// WithUnauthorizedHandler sets a custom handler for authentication failures.
func WithUnauthorizedHandler(handler func(w http.ResponseWriter, r *http.Request, err error)) AuthMiddlewareOption {
	return func(m *AuthMiddleware) {
		m.onUnauthorized = handler
	}
}

// NewAuthMiddleware creates a new authentication middleware.
func NewAuthMiddleware(auth Authenticator, opts ...AuthMiddlewareOption) *AuthMiddleware {
	m := &AuthMiddleware{
		auth:      auth,
		realm:     "MCP Bridge",
		logger:    slog.Default(),
		skipPaths: make(map[string]bool),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Wrap returns an http.Handler that wraps the given handler with authentication.
// If authentication succeeds, the authenticated Principal is set in the request context.
// If authentication fails, a 401 Unauthorized response is returned.
func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this path should skip authentication
		if m.skipPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Validate the request
		principal, err := m.auth.Validate(r.Context(), r)
		if err != nil {
			m.handleUnauthorized(w, r, err)
			return
		}

		// Set principal in context
		ctx := ContextWithPrincipal(r.Context(), principal)

		// Log successful authentication
		m.logger.Debug("authentication successful",
			"principal_id", principal.ID,
			"principal_type", principal.Type,
			"path", r.URL.Path,
			"method", r.Method,
		)

		// Call the next handler with updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// WrapFunc returns an http.HandlerFunc that wraps the given function with authentication.
func (m *AuthMiddleware) WrapFunc(next http.HandlerFunc) http.HandlerFunc {
	return m.Wrap(next).ServeHTTP
}

// handleUnauthorized handles authentication failures.
func (m *AuthMiddleware) handleUnauthorized(w http.ResponseWriter, r *http.Request, err error) {
	// Log the authentication failure
	m.logger.Warn("authentication failed",
		"error", err,
		"path", r.URL.Path,
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	// If custom handler is set, use it
	if m.onUnauthorized != nil {
		m.onUnauthorized(w, r, err)
		return
	}

	// Determine appropriate response
	statusCode := http.StatusUnauthorized
	wwwAuth := "Bearer"

	// Check for specific error types
	var authErr *AuthError
	if errors.As(err, &authErr) {
		wwwAuth = authErr.WWWAuthenticate()

		// Check for forbidden vs unauthorized
		if errors.Is(authErr.Err, ErrForbidden) {
			statusCode = http.StatusForbidden
		}
	} else if errors.Is(err, ErrForbidden) {
		statusCode = http.StatusForbidden
	}

	// Add WWW-Authenticate header for 401 responses
	if statusCode == http.StatusUnauthorized {
		if m.realm != "" && wwwAuth == "Bearer" {
			wwwAuth = "Bearer realm=\"" + m.realm + "\""
		}
		w.Header().Set("WWW-Authenticate", wwwAuth)
	}

	// Set content type and write error response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Write JSON error response
	errMsg := "authentication required"
	if errors.Is(err, ErrInvalidToken) {
		errMsg = "invalid token"
	} else if errors.Is(err, ErrTokenExpired) {
		errMsg = "token expired"
	} else if errors.Is(err, ErrForbidden) {
		errMsg = "access forbidden"
	}

	// Write a simple JSON error response
	w.Write([]byte(`{"error":"` + errMsg + `"}`))
}

// Handler returns the middleware as an http.Handler for chaining.
func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return m.Wrap(next)
}

// RequireScopes returns middleware that checks for required scopes.
// The principal must have ALL specified scopes.
func RequireScopes(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := PrincipalFromContext(r.Context())
			if principal == nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			if !principal.HasAllScopes(scopes...) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"insufficient scopes"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyScope returns middleware that checks for any of the required scopes.
// The principal must have at least ONE of the specified scopes.
func RequireAnyScope(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := PrincipalFromContext(r.Context())
			if principal == nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			if !principal.HasAnyScope(scopes...) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"insufficient scopes"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePrincipalType returns middleware that checks for a specific principal type.
func RequirePrincipalType(principalType PrincipalType) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := PrincipalFromContext(r.Context())
			if principal == nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			if principal.Type != principalType {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"invalid principal type"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Chain combines multiple middleware functions into a single middleware.
func Chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// ConditionalAuth returns middleware that conditionally applies authentication.
// The condition function is called for each request to determine if authentication
// should be enforced.
func ConditionalAuth(auth *AuthMiddleware, condition func(r *http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if condition(r) {
				auth.Wrap(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// HealthCheckSkipper returns a condition function that skips authentication for health check paths.
func HealthCheckSkipper(paths ...string) func(r *http.Request) bool {
	pathSet := make(map[string]bool, len(paths))
	for _, p := range paths {
		pathSet[p] = true
	}

	return func(r *http.Request) bool {
		return !pathSet[r.URL.Path]
	}
}
