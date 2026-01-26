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
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// mockAuthenticator is a test authenticator that can be configured to succeed or fail.
type mockAuthenticator struct {
	validateFunc     func(ctx context.Context, req *http.Request) (*Principal, error)
	authenticateFunc func(ctx context.Context, req *http.Request) error
	refreshFunc      func(ctx context.Context) error
}

func (m *mockAuthenticator) Validate(ctx context.Context, req *http.Request) (*Principal, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, req)
	}
	return &Principal{ID: "mock-user", Type: PrincipalTypeUser}, nil
}

func (m *mockAuthenticator) Authenticate(ctx context.Context, req *http.Request) error {
	if m.authenticateFunc != nil {
		return m.authenticateFunc(ctx, req)
	}
	return nil
}

func (m *mockAuthenticator) Refresh(ctx context.Context) error {
	if m.refreshFunc != nil {
		return m.refreshFunc(ctx)
	}
	return nil
}

func (m *mockAuthenticator) Close() error {
	return nil
}

// TestNewAuthMiddleware tests the middleware constructor.
func TestNewAuthMiddleware(t *testing.T) {
	auth := &mockAuthenticator{}
	mw := NewAuthMiddleware(auth)

	if mw == nil {
		t.Fatal("NewAuthMiddleware() returned nil")
	}
	if mw.auth != auth {
		t.Error("middleware auth not set correctly")
	}
	if mw.realm != "MCP Bridge" {
		t.Errorf("default realm = %q, want %q", mw.realm, "MCP Bridge")
	}
}

// TestAuthMiddleware_WithRealm tests the WithRealm option.
func TestAuthMiddleware_WithRealm(t *testing.T) {
	auth := &mockAuthenticator{}
	mw := NewAuthMiddleware(auth, WithRealm("Custom Realm"))

	if mw.realm != "Custom Realm" {
		t.Errorf("realm = %q, want %q", mw.realm, "Custom Realm")
	}
}

// TestAuthMiddleware_WithLogger tests the WithLogger option.
func TestAuthMiddleware_WithLogger(t *testing.T) {
	auth := &mockAuthenticator{}
	logger := slog.Default()
	mw := NewAuthMiddleware(auth, WithLogger(logger))

	if mw.logger != logger {
		t.Error("logger not set correctly")
	}
}

// TestAuthMiddleware_WithSkipPaths tests the WithSkipPaths option.
func TestAuthMiddleware_WithSkipPaths(t *testing.T) {
	auth := &mockAuthenticator{}
	mw := NewAuthMiddleware(auth, WithSkipPaths("/health", "/ready", "/metrics"))

	if !mw.skipPaths["/health"] {
		t.Error("/health should be in skipPaths")
	}
	if !mw.skipPaths["/ready"] {
		t.Error("/ready should be in skipPaths")
	}
	if !mw.skipPaths["/metrics"] {
		t.Error("/metrics should be in skipPaths")
	}
	if mw.skipPaths["/api"] {
		t.Error("/api should not be in skipPaths")
	}
}

// TestAuthMiddleware_WithUnauthorizedHandler tests the custom unauthorized handler option.
func TestAuthMiddleware_WithUnauthorizedHandler(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, ErrUnauthorized
		},
	}

	customHandlerCalled := false
	customHandler := func(w http.ResponseWriter, r *http.Request, err error) {
		customHandlerCalled = true
		w.WriteHeader(http.StatusTeapot) // Unusual status to verify handler was called
	}

	mw := NewAuthMiddleware(auth, WithUnauthorizedHandler(customHandler))

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !customHandlerCalled {
		t.Error("custom unauthorized handler was not called")
	}
	if rr.Code != http.StatusTeapot {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusTeapot)
	}
}

// TestAuthMiddleware_Wrap_Success tests successful authentication.
func TestAuthMiddleware_Wrap_Success(t *testing.T) {
	principal := &Principal{
		ID:     "test-user",
		Type:   PrincipalTypeUser,
		Scopes: []string{"read", "write"},
	}

	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return principal, nil
		},
	}

	mw := NewAuthMiddleware(auth)

	var capturedPrincipal *Principal
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPrincipal = PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	if capturedPrincipal == nil {
		t.Fatal("principal not set in context")
	}
	if capturedPrincipal.ID != "test-user" {
		t.Errorf("principal ID = %q, want %q", capturedPrincipal.ID, "test-user")
	}
}

// TestAuthMiddleware_Wrap_MissingToken tests 401 for missing token.
func TestAuthMiddleware_Wrap_MissingToken(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, NewAuthError(ErrMissingCredentials, "no token", "Test", "Bearer")
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// TestAuthMiddleware_Wrap_InvalidToken tests 401 for invalid token.
func TestAuthMiddleware_Wrap_InvalidToken(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, NewAuthError(ErrInvalidToken, "bad token", "Test", "Bearer")
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	// Check error message in response
	var resp map[string]string
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["error"] != "invalid token" {
		t.Errorf("error message = %q, want %q", resp["error"], "invalid token")
	}
}

// TestAuthMiddleware_Wrap_WWWAuthenticate tests WWW-Authenticate header is set.
func TestAuthMiddleware_Wrap_WWWAuthenticate(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, ErrUnauthorized
		},
	}

	mw := NewAuthMiddleware(auth, WithRealm("My API"))

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	wwwAuth := rr.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("WWW-Authenticate header not set")
	}
	if !strings.Contains(wwwAuth, "My API") {
		t.Errorf("WWW-Authenticate = %q, want to contain realm", wwwAuth)
	}
}

// TestAuthMiddleware_Wrap_SkipPaths tests that skip paths are not authenticated.
func TestAuthMiddleware_Wrap_SkipPaths(t *testing.T) {
	validateCalled := false
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			validateCalled = true
			return nil, ErrUnauthorized
		},
	}

	mw := NewAuthMiddleware(auth, WithSkipPaths("/health", "/ready"))

	handlerCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// Test skip path
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if validateCalled {
		t.Error("Validate should not be called for skip paths")
	}
	if !handlerCalled {
		t.Error("handler should be called for skip paths")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
}

// TestAuthMiddleware_Wrap_NonSkipPath tests that non-skip paths are authenticated.
func TestAuthMiddleware_Wrap_NonSkipPath(t *testing.T) {
	validateCalled := false
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			validateCalled = true
			return nil, ErrUnauthorized
		},
	}

	mw := NewAuthMiddleware(auth, WithSkipPaths("/health"))

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Test non-skip path
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !validateCalled {
		t.Error("Validate should be called for non-skip paths")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// TestAuthMiddleware_WrapFunc tests the WrapFunc method.
func TestAuthMiddleware_WrapFunc(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return &Principal{ID: "user"}, nil
		},
	}

	mw := NewAuthMiddleware(auth)

	handlerCalled := false
	handlerFunc := mw.WrapFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handlerFunc(rr, req)

	if !handlerCalled {
		t.Error("handler function was not called")
	}
}

// TestAuthMiddleware_Handler tests the Handler method.
func TestAuthMiddleware_Handler(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return &Principal{ID: "user"}, nil
		},
	}

	mw := NewAuthMiddleware(auth)

	handlerCalled := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	wrappedHandler := mw.Handler(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rr, req)

	if !handlerCalled {
		t.Error("inner handler was not called")
	}
}

// TestRequireScopes tests the RequireScopes middleware.
func TestRequireScopes(t *testing.T) {
	tests := []struct {
		name            string
		principalScopes []string
		requiredScopes  []string
		wantStatus      int
	}{
		{
			name:            "has all required scopes",
			principalScopes: []string{"read", "write", "admin"},
			requiredScopes:  []string{"read", "write"},
			wantStatus:      http.StatusOK,
		},
		{
			name:            "missing one scope",
			principalScopes: []string{"read"},
			requiredScopes:  []string{"read", "write"},
			wantStatus:      http.StatusForbidden,
		},
		{
			name:            "no scopes required",
			principalScopes: []string{},
			requiredScopes:  []string{},
			wantStatus:      http.StatusOK,
		},
		{
			name:            "has exact scopes",
			principalScopes: []string{"admin"},
			requiredScopes:  []string{"admin"},
			wantStatus:      http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal := &Principal{
				ID:     "user",
				Scopes: tt.principalScopes,
			}

			handler := RequireScopes(tt.requiredScopes...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := ContextWithPrincipal(req.Context(), principal)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

// TestRequireScopes_NoPrincipal tests RequireScopes when no principal in context.
func TestRequireScopes_NoPrincipal(t *testing.T) {
	handler := RequireScopes("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No principal in context

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// TestRequireAnyScope tests the RequireAnyScope middleware.
func TestRequireAnyScope(t *testing.T) {
	tests := []struct {
		name            string
		principalScopes []string
		requiredScopes  []string
		wantStatus      int
	}{
		{
			name:            "has one of required scopes",
			principalScopes: []string{"read"},
			requiredScopes:  []string{"read", "write", "admin"},
			wantStatus:      http.StatusOK,
		},
		{
			name:            "has none of required scopes",
			principalScopes: []string{"execute"},
			requiredScopes:  []string{"read", "write"},
			wantStatus:      http.StatusForbidden,
		},
		{
			name:            "has all required scopes",
			principalScopes: []string{"read", "write"},
			requiredScopes:  []string{"read", "write"},
			wantStatus:      http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal := &Principal{
				ID:     "user",
				Scopes: tt.principalScopes,
			}

			handler := RequireAnyScope(tt.requiredScopes...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := ContextWithPrincipal(req.Context(), principal)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

// TestRequireAnyScope_NoPrincipal tests RequireAnyScope when no principal in context.
func TestRequireAnyScope_NoPrincipal(t *testing.T) {
	handler := RequireAnyScope("read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// TestRequirePrincipalType tests the RequirePrincipalType middleware.
func TestRequirePrincipalType(t *testing.T) {
	tests := []struct {
		name          string
		principalType PrincipalType
		requiredType  PrincipalType
		wantStatus    int
	}{
		{
			name:          "matching type",
			principalType: PrincipalTypeUser,
			requiredType:  PrincipalTypeUser,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "non-matching type",
			principalType: PrincipalTypeService,
			requiredType:  PrincipalTypeUser,
			wantStatus:    http.StatusForbidden,
		},
		{
			name:          "service type match",
			principalType: PrincipalTypeService,
			requiredType:  PrincipalTypeService,
			wantStatus:    http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal := &Principal{
				ID:   "test",
				Type: tt.principalType,
			}

			handler := RequirePrincipalType(tt.requiredType)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := ContextWithPrincipal(req.Context(), principal)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

// TestRequirePrincipalType_NoPrincipal tests RequirePrincipalType when no principal.
func TestRequirePrincipalType_NoPrincipal(t *testing.T) {
	handler := RequirePrincipalType(PrincipalTypeUser)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// TestChain tests the Chain function combines middleware correctly.
func TestChain(t *testing.T) {
	callOrder := []string{}

	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, "mw1-before")
			next.ServeHTTP(w, r)
			callOrder = append(callOrder, "mw1-after")
		})
	}

	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, "mw2-before")
			next.ServeHTTP(w, r)
			callOrder = append(callOrder, "mw2-after")
		})
	}

	mw3 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, "mw3-before")
			next.ServeHTTP(w, r)
			callOrder = append(callOrder, "mw3-after")
		})
	}

	chained := Chain(mw1, mw2, mw3)

	handler := chained(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callOrder = append(callOrder, "handler")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	expectedOrder := []string{
		"mw1-before",
		"mw2-before",
		"mw3-before",
		"handler",
		"mw3-after",
		"mw2-after",
		"mw1-after",
	}

	if len(callOrder) != len(expectedOrder) {
		t.Fatalf("call order length = %d, want %d", len(callOrder), len(expectedOrder))
	}

	for i, expected := range expectedOrder {
		if callOrder[i] != expected {
			t.Errorf("callOrder[%d] = %q, want %q", i, callOrder[i], expected)
		}
	}
}

// TestChain_Empty tests Chain with no middleware.
func TestChain_Empty(t *testing.T) {
	chained := Chain()

	handlerCalled := false
	handler := chained(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !handlerCalled {
		t.Error("handler was not called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// TestChain_SingleMiddleware tests Chain with single middleware.
func TestChain_SingleMiddleware(t *testing.T) {
	mwCalled := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwCalled = true
			next.ServeHTTP(w, r)
		})
	}

	chained := Chain(mw)

	handler := chained(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !mwCalled {
		t.Error("middleware was not called")
	}
}

// TestConditionalAuth tests the ConditionalAuth middleware.
func TestConditionalAuth(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, ErrUnauthorized
		},
	}
	mw := NewAuthMiddleware(auth)

	// Condition: only authenticate requests with /api prefix
	condition := func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/api")
	}

	handler := ConditionalAuth(mw, condition)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("path requiring auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
		}
	})

	t.Run("path not requiring auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/public/test", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}
	})
}

// TestHealthCheckSkipper tests the HealthCheckSkipper function.
func TestHealthCheckSkipper(t *testing.T) {
	skipper := HealthCheckSkipper("/health", "/ready", "/live")

	tests := []struct {
		path     string
		wantAuth bool
	}{
		{"/health", false},
		{"/ready", false},
		{"/live", false},
		{"/api/test", true},
		{"/", true},
		{"/healthcheck", true}, // Not exact match
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			got := skipper(req)
			if got != tt.wantAuth {
				t.Errorf("skipper(%q) = %v, want %v", tt.path, got, tt.wantAuth)
			}
		})
	}
}

// TestAuthMiddleware_ForbiddenError tests handling of forbidden errors.
func TestAuthMiddleware_ForbiddenError(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, NewAuthError(ErrForbidden, "not allowed", "Test", "Bearer")
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusForbidden)
	}

	// WWW-Authenticate should NOT be set for 403
	if rr.Header().Get("WWW-Authenticate") != "" {
		t.Error("WWW-Authenticate should not be set for 403")
	}
}

// TestAuthMiddleware_TokenExpiredError tests handling of token expired errors.
func TestAuthMiddleware_TokenExpiredError(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, NewAuthError(ErrTokenExpired, "token expired", "Test", "Bearer")
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	// Check response contains "token expired"
	var resp map[string]string
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["error"] != "token expired" {
		t.Errorf("error message = %q, want %q", resp["error"], "token expired")
	}
}

// TestAuthMiddleware_PlainForbiddenError tests handling of plain ErrForbidden.
func TestAuthMiddleware_PlainForbiddenError(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, ErrForbidden
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

// TestAuthMiddleware_ContentTypeJSON tests that JSON content type is set.
func TestAuthMiddleware_ContentTypeJSON(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, ErrUnauthorized
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
}

// TestAuthMiddleware_IntegrationWithBearer tests middleware with real bearer authenticator.
func TestAuthMiddleware_IntegrationWithBearer(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	mw := NewAuthMiddleware(auth, WithSkipPaths("/health"))

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only check for principal on authenticated paths (not skip paths)
		if r.URL.Path != "/health" {
			principal := PrincipalFromContext(r.Context())
			if principal == nil {
				t.Error("principal should be set for authenticated paths")
			}
		}
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
		}
	})

	t.Run("skip path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		// Health endpoint has no principal but should succeed
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}
	})
}

// TestAuthMiddleware_WWWAuthenticateWithAuthError tests WWW-Authenticate from AuthError.
func TestAuthMiddleware_WWWAuthenticateWithAuthError(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, &AuthError{
				Err:    ErrInvalidToken,
				Scheme: "Bearer",
				Realm:  "Custom Realm",
			}
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	wwwAuth := rr.Header().Get("WWW-Authenticate")
	if !strings.Contains(wwwAuth, "Custom Realm") {
		t.Errorf("WWW-Authenticate = %q, want to contain 'Custom Realm'", wwwAuth)
	}
}

// TestChain_WithAuthAndScopes tests chaining auth middleware with scope middleware.
func TestChain_WithAuthAndScopes(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return &Principal{
				ID:     "user",
				Scopes: []string{"read"},
			}, nil
		},
	}

	mw := NewAuthMiddleware(auth)

	chained := Chain(
		mw.Wrap,
		RequireScopes("read", "write"),
	)

	handler := chained(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should fail because principal has "read" but not "write"
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

// BenchmarkAuthMiddleware_Validate benchmarks the authentication middleware.
func BenchmarkAuthMiddleware_Validate(b *testing.B) {
	principal := &Principal{ID: "user", Type: PrincipalTypeUser}
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return principal, nil
		},
	}

	mw := NewAuthMiddleware(auth)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// BenchmarkRequireScopes benchmarks the scope checking middleware.
func BenchmarkRequireScopes(b *testing.B) {
	principal := &Principal{
		ID:     "user",
		Scopes: []string{"read", "write", "admin", "delete"},
	}

	handler := RequireScopes("read", "write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := ContextWithPrincipal(req.Context(), principal)
	req = req.WithContext(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// TestAuthMiddleware_GenericError tests handling of generic errors.
func TestAuthMiddleware_GenericError(t *testing.T) {
	auth := &mockAuthenticator{
		validateFunc: func(ctx context.Context, req *http.Request) (*Principal, error) {
			return nil, errors.New("unexpected error")
		},
	}

	mw := NewAuthMiddleware(auth)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	// Check response contains generic "authentication required"
	var resp map[string]string
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["error"] != "authentication required" {
		t.Errorf("error message = %q, want %q", resp["error"], "authentication required")
	}
}
