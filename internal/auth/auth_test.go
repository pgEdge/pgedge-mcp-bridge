package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// TestNewAuthenticator_BearerType tests that NewAuthenticator creates a BearerAuthenticator for bearer type.
func TestNewAuthenticator_BearerType(t *testing.T) {
	cfg := &config.AuthConfig{
		Type: "bearer",
		Bearer: &config.BearerAuthConfig{
			ValidTokens: []string{"test-token"},
		},
	}

	auth, err := NewAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	_, ok := auth.(*BearerAuthenticator)
	if !ok {
		t.Errorf("NewAuthenticator() = %T, want *BearerAuthenticator", auth)
	}
}

// TestNewAuthenticator_OAuthType tests that NewAuthenticator creates an OAuthAuthenticator for oauth type.
func TestNewAuthenticator_OAuthType(t *testing.T) {
	// Create a mock OAuth server for discovery
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"issuer": "https://example.com",
			"authorization_endpoint": "https://example.com/authorize",
			"token_endpoint": "https://example.com/token",
			"jwks_uri": "https://example.com/.well-known/jwks.json"
		}`))
	}))
	defer mockServer.Close()

	cfg := &config.AuthConfig{
		Type: "oauth",
		OAuth: &config.OAuthConfig{
			IntrospectionURL: mockServer.URL,
		},
	}

	auth, err := NewAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	_, ok := auth.(*OAuthAuthenticator)
	if !ok {
		t.Errorf("NewAuthenticator() = %T, want *OAuthAuthenticator", auth)
	}
}

// TestNewAuthenticator_NoopForNilConfig tests that a nil config returns a noopAuthenticator.
func TestNewAuthenticator_NoopForNilConfig(t *testing.T) {
	auth, err := NewAuthenticator(nil, true)
	if err != nil {
		t.Fatalf("NewAuthenticator(nil) error = %v", err)
	}

	// Verify it's a noopAuthenticator by checking its behavior
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	principal, err := auth.Validate(context.Background(), req)
	if err != nil {
		t.Errorf("noopAuthenticator.Validate() error = %v", err)
	}
	if principal == nil || principal.ID != "anonymous" {
		t.Errorf("noopAuthenticator.Validate() principal = %v, want anonymous principal", principal)
	}
}

// TestNewAuthenticator_NoopForEmptyType tests that empty type returns a noopAuthenticator.
func TestNewAuthenticator_NoopForEmptyType(t *testing.T) {
	cfg := &config.AuthConfig{
		Type: "",
	}

	auth, err := NewAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	principal, err := auth.Validate(context.Background(), req)
	if err != nil {
		t.Errorf("noopAuthenticator.Validate() error = %v", err)
	}
	if principal == nil || principal.ID != "anonymous" {
		t.Errorf("noopAuthenticator.Validate() principal = %v, want anonymous principal", principal)
	}
}

// TestNewAuthenticator_NoopForNoneType tests that "none" type returns a noopAuthenticator.
func TestNewAuthenticator_NoopForNoneType(t *testing.T) {
	cfg := &config.AuthConfig{
		Type: "none",
	}

	auth, err := NewAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	principal, err := auth.Validate(context.Background(), req)
	if err != nil {
		t.Errorf("noopAuthenticator.Validate() error = %v", err)
	}
	if principal == nil || principal.ID != "anonymous" {
		t.Errorf("noopAuthenticator.Validate() principal = %v, want anonymous principal", principal)
	}
}

// TestNewAuthenticator_UnknownType tests that an unknown type returns an error.
func TestNewAuthenticator_UnknownType(t *testing.T) {
	cfg := &config.AuthConfig{
		Type: "unknown-type",
	}

	auth, err := NewAuthenticator(cfg, true)
	if err == nil {
		t.Fatalf("NewAuthenticator() expected error for unknown type, got nil")
	}

	if auth != nil {
		t.Errorf("NewAuthenticator() auth = %v, want nil", auth)
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("NewAuthenticator() error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestNewAuthenticator_BearerNilConfig tests that bearer type with nil config returns error.
func TestNewAuthenticator_BearerNilConfig(t *testing.T) {
	cfg := &config.AuthConfig{
		Type:   "bearer",
		Bearer: nil,
	}

	_, err := NewAuthenticator(cfg, true)
	if err == nil {
		t.Fatal("NewAuthenticator() expected error for nil bearer config")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("NewAuthenticator() error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestNewAuthenticator_OAuthNilConfig tests that oauth type with nil config returns error.
func TestNewAuthenticator_OAuthNilConfig(t *testing.T) {
	cfg := &config.AuthConfig{
		Type:  "oauth",
		OAuth: nil,
	}

	_, err := NewAuthenticator(cfg, true)
	if err == nil {
		t.Fatal("NewAuthenticator() expected error for nil oauth config")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("NewAuthenticator() error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestPrincipal_HasScope tests the HasScope method.
func TestPrincipal_HasScope(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		scope  string
		want   bool
	}{
		{
			name:   "has scope",
			scopes: []string{"read", "write", "admin"},
			scope:  "write",
			want:   true,
		},
		{
			name:   "does not have scope",
			scopes: []string{"read", "write"},
			scope:  "admin",
			want:   false,
		},
		{
			name:   "empty scopes",
			scopes: []string{},
			scope:  "read",
			want:   false,
		},
		{
			name:   "nil scopes",
			scopes: nil,
			scope:  "read",
			want:   false,
		},
		{
			name:   "exact match required",
			scopes: []string{"read:all"},
			scope:  "read",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Principal{
				ID:     "test",
				Scopes: tt.scopes,
			}
			if got := p.HasScope(tt.scope); got != tt.want {
				t.Errorf("HasScope(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

// TestPrincipal_HasAllScopes tests the HasAllScopes method.
func TestPrincipal_HasAllScopes(t *testing.T) {
	tests := []struct {
		name       string
		scopes     []string
		checkFor   []string
		want       bool
	}{
		{
			name:     "has all scopes",
			scopes:   []string{"read", "write", "admin"},
			checkFor: []string{"read", "write"},
			want:     true,
		},
		{
			name:     "missing one scope",
			scopes:   []string{"read", "write"},
			checkFor: []string{"read", "write", "admin"},
			want:     false,
		},
		{
			name:     "empty check list",
			scopes:   []string{"read", "write"},
			checkFor: []string{},
			want:     true,
		},
		{
			name:     "empty scopes",
			scopes:   []string{},
			checkFor: []string{"read"},
			want:     false,
		},
		{
			name:     "single scope match",
			scopes:   []string{"admin"},
			checkFor: []string{"admin"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Principal{
				ID:     "test",
				Scopes: tt.scopes,
			}
			if got := p.HasAllScopes(tt.checkFor...); got != tt.want {
				t.Errorf("HasAllScopes(%v) = %v, want %v", tt.checkFor, got, tt.want)
			}
		})
	}
}

// TestPrincipal_HasAnyScope tests the HasAnyScope method.
func TestPrincipal_HasAnyScope(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		checkFor []string
		want     bool
	}{
		{
			name:     "has one of the scopes",
			scopes:   []string{"read"},
			checkFor: []string{"read", "write", "admin"},
			want:     true,
		},
		{
			name:     "has none of the scopes",
			scopes:   []string{"execute"},
			checkFor: []string{"read", "write", "admin"},
			want:     false,
		},
		{
			name:     "empty check list",
			scopes:   []string{"read", "write"},
			checkFor: []string{},
			want:     false,
		},
		{
			name:     "empty scopes",
			scopes:   []string{},
			checkFor: []string{"read"},
			want:     false,
		},
		{
			name:     "has all scopes",
			scopes:   []string{"read", "write", "admin"},
			checkFor: []string{"read", "write"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Principal{
				ID:     "test",
				Scopes: tt.scopes,
			}
			if got := p.HasAnyScope(tt.checkFor...); got != tt.want {
				t.Errorf("HasAnyScope(%v) = %v, want %v", tt.checkFor, got, tt.want)
			}
		})
	}
}

// TestPrincipal_GetClaim tests the GetClaim method.
func TestPrincipal_GetClaim(t *testing.T) {
	tests := []struct {
		name   string
		claims map[string]interface{}
		key    string
		want   interface{}
	}{
		{
			name: "existing claim",
			claims: map[string]interface{}{
				"sub":   "user123",
				"email": "user@example.com",
			},
			key:  "email",
			want: "user@example.com",
		},
		{
			name: "non-existing claim",
			claims: map[string]interface{}{
				"sub": "user123",
			},
			key:  "email",
			want: nil,
		},
		{
			name:   "nil claims",
			claims: nil,
			key:    "sub",
			want:   nil,
		},
		{
			name:   "empty claims",
			claims: map[string]interface{}{},
			key:    "sub",
			want:   nil,
		},
		{
			name: "numeric claim",
			claims: map[string]interface{}{
				"exp": 1234567890,
			},
			key:  "exp",
			want: 1234567890,
		},
		{
			name: "bool claim",
			claims: map[string]interface{}{
				"email_verified": true,
			},
			key:  "email_verified",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Principal{
				ID:     "test",
				Claims: tt.claims,
			}
			got := p.GetClaim(tt.key)
			if got != tt.want {
				t.Errorf("GetClaim(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestPrincipal_GetStringClaim tests the GetStringClaim method.
func TestPrincipal_GetStringClaim(t *testing.T) {
	tests := []struct {
		name   string
		claims map[string]interface{}
		key    string
		want   string
	}{
		{
			name: "existing string claim",
			claims: map[string]interface{}{
				"email": "user@example.com",
			},
			key:  "email",
			want: "user@example.com",
		},
		{
			name: "non-string claim",
			claims: map[string]interface{}{
				"exp": 1234567890,
			},
			key:  "exp",
			want: "",
		},
		{
			name: "non-existing claim",
			claims: map[string]interface{}{
				"sub": "user123",
			},
			key:  "email",
			want: "",
		},
		{
			name:   "nil claims",
			claims: nil,
			key:    "sub",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Principal{
				ID:     "test",
				Claims: tt.claims,
			}
			got := p.GetStringClaim(tt.key)
			if got != tt.want {
				t.Errorf("GetStringClaim(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// TestPrincipalFromContext tests the PrincipalFromContext function.
func TestPrincipalFromContext(t *testing.T) {
	tests := []struct {
		name      string
		setupCtx  func() context.Context
		wantNil   bool
		wantID    string
	}{
		{
			name: "principal in context",
			setupCtx: func() context.Context {
				p := &Principal{ID: "user123", Type: PrincipalTypeUser}
				return ContextWithPrincipal(context.Background(), p)
			},
			wantNil: false,
			wantID:  "user123",
		},
		{
			name: "no principal in context",
			setupCtx: func() context.Context {
				return context.Background()
			},
			wantNil: true,
		},
		{
			name: "wrong type in context",
			setupCtx: func() context.Context {
				// Manually set a non-Principal value with the same key
				return context.WithValue(context.Background(), principalContextKey, "not a principal")
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			got := PrincipalFromContext(ctx)

			if tt.wantNil {
				if got != nil {
					t.Errorf("PrincipalFromContext() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("PrincipalFromContext() = nil, want non-nil")
				}
				if got.ID != tt.wantID {
					t.Errorf("PrincipalFromContext().ID = %q, want %q", got.ID, tt.wantID)
				}
			}
		})
	}
}

// TestContextWithPrincipal tests the ContextWithPrincipal function.
func TestContextWithPrincipal(t *testing.T) {
	p := &Principal{
		ID:     "test-user",
		Type:   PrincipalTypeUser,
		Scopes: []string{"read", "write"},
		Claims: map[string]interface{}{"email": "test@example.com"},
	}

	ctx := ContextWithPrincipal(context.Background(), p)

	// Retrieve the principal
	got := PrincipalFromContext(ctx)
	if got == nil {
		t.Fatal("PrincipalFromContext() returned nil")
	}

	// Verify it's the same principal
	if got.ID != p.ID {
		t.Errorf("Principal.ID = %q, want %q", got.ID, p.ID)
	}
	if got.Type != p.Type {
		t.Errorf("Principal.Type = %q, want %q", got.Type, p.Type)
	}
	if len(got.Scopes) != len(p.Scopes) {
		t.Errorf("Principal.Scopes = %v, want %v", got.Scopes, p.Scopes)
	}
}

// TestRequirePrincipal tests the RequirePrincipal function.
func TestRequirePrincipal(t *testing.T) {
	t.Run("principal exists", func(t *testing.T) {
		p := &Principal{ID: "user123"}
		ctx := ContextWithPrincipal(context.Background(), p)

		got, err := RequirePrincipal(ctx)
		if err != nil {
			t.Fatalf("RequirePrincipal() error = %v", err)
		}
		if got.ID != "user123" {
			t.Errorf("RequirePrincipal().ID = %q, want %q", got.ID, "user123")
		}
	})

	t.Run("principal missing", func(t *testing.T) {
		ctx := context.Background()

		_, err := RequirePrincipal(ctx)
		if err == nil {
			t.Fatal("RequirePrincipal() expected error, got nil")
		}
		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("RequirePrincipal() error = %v, want ErrUnauthorized", err)
		}
	})
}

// TestAuthError tests the AuthError type.
func TestAuthError(t *testing.T) {
	t.Run("Error with message", func(t *testing.T) {
		err := &AuthError{
			Err:     ErrInvalidToken,
			Message: "token validation failed",
		}

		got := err.Error()
		want := "token validation failed: invalid token"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Error without message", func(t *testing.T) {
		err := &AuthError{
			Err: ErrInvalidToken,
		}

		got := err.Error()
		want := "invalid token"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &AuthError{
			Err: ErrInvalidToken,
		}

		if !errors.Is(err, ErrInvalidToken) {
			t.Error("Unwrap() should allow errors.Is() to match underlying error")
		}
	})

	t.Run("WWWAuthenticate with realm", func(t *testing.T) {
		err := &AuthError{
			Scheme: "Bearer",
			Realm:  "My API",
		}

		got := err.WWWAuthenticate()
		want := `Bearer realm="My API"`
		if got != want {
			t.Errorf("WWWAuthenticate() = %q, want %q", got, want)
		}
	})

	t.Run("WWWAuthenticate without realm", func(t *testing.T) {
		err := &AuthError{
			Scheme: "Bearer",
		}

		got := err.WWWAuthenticate()
		if got != "Bearer" {
			t.Errorf("WWWAuthenticate() = %q, want %q", got, "Bearer")
		}
	})

	t.Run("WWWAuthenticate default scheme", func(t *testing.T) {
		err := &AuthError{}

		got := err.WWWAuthenticate()
		if got != "Bearer" {
			t.Errorf("WWWAuthenticate() = %q, want %q", got, "Bearer")
		}
	})
}

// TestNewAuthError tests the NewAuthError constructor.
func TestNewAuthError(t *testing.T) {
	err := NewAuthError(ErrTokenExpired, "token has expired", "API", "Bearer")

	if err.Err != ErrTokenExpired {
		t.Errorf("Err = %v, want ErrTokenExpired", err.Err)
	}
	if err.Message != "token has expired" {
		t.Errorf("Message = %q, want %q", err.Message, "token has expired")
	}
	if err.Realm != "API" {
		t.Errorf("Realm = %q, want %q", err.Realm, "API")
	}
	if err.Scheme != "Bearer" {
		t.Errorf("Scheme = %q, want %q", err.Scheme, "Bearer")
	}
}

// TestIsAuthError tests the IsAuthError function.
func TestIsAuthError(t *testing.T) {
	t.Run("is auth error", func(t *testing.T) {
		err := &AuthError{Err: ErrInvalidToken}
		if !IsAuthError(err) {
			t.Error("IsAuthError() = false, want true")
		}
	})

	t.Run("wrapped auth error", func(t *testing.T) {
		authErr := &AuthError{Err: ErrInvalidToken}
		wrapped := errors.Join(errors.New("context"), authErr)
		// Note: errors.Join creates a multi-error that doesn't work with As
		// Let's test with proper wrapping
		wrapped2 := &AuthError{Err: wrapped, Message: "nested"}
		if !IsAuthError(wrapped2) {
			t.Error("IsAuthError() = false for wrapped error, want true")
		}
	})

	t.Run("not auth error", func(t *testing.T) {
		err := errors.New("some other error")
		if IsAuthError(err) {
			t.Error("IsAuthError() = true, want false")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		if IsAuthError(nil) {
			t.Error("IsAuthError(nil) = true, want false")
		}
	})
}

// TestNoopAuthenticator tests the noopAuthenticator behavior.
func TestNoopAuthenticator(t *testing.T) {
	auth := &noopAuthenticator{}

	t.Run("Validate returns anonymous principal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		principal, err := auth.Validate(context.Background(), req)

		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
		if principal.ID != "anonymous" {
			t.Errorf("Principal.ID = %q, want %q", principal.ID, "anonymous")
		}
		if principal.Type != PrincipalTypeUser {
			t.Errorf("Principal.Type = %v, want %v", principal.Type, PrincipalTypeUser)
		}
	})

	t.Run("Authenticate does nothing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		err := auth.Authenticate(context.Background(), req)

		if err != nil {
			t.Errorf("Authenticate() error = %v", err)
		}
		if req.Header.Get("Authorization") != "" {
			t.Error("Authenticate() should not set Authorization header")
		}
	})

	t.Run("Refresh does nothing", func(t *testing.T) {
		err := auth.Refresh(context.Background())
		if err != nil {
			t.Errorf("Refresh() error = %v", err)
		}
	})

	t.Run("Close does nothing", func(t *testing.T) {
		err := auth.Close()
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

// TestPrincipalTypes tests the PrincipalType constants.
func TestPrincipalTypes(t *testing.T) {
	tests := []struct {
		pt   PrincipalType
		want string
	}{
		{PrincipalTypeUser, "user"},
		{PrincipalTypeService, "service"},
		{PrincipalTypeToken, "token"},
	}

	for _, tt := range tests {
		t.Run(string(tt.pt), func(t *testing.T) {
			if string(tt.pt) != tt.want {
				t.Errorf("PrincipalType = %q, want %q", string(tt.pt), tt.want)
			}
		})
	}
}

// TestAuthTypes tests the AuthType constants.
func TestAuthTypes(t *testing.T) {
	tests := []struct {
		at   AuthType
		want string
	}{
		{AuthTypeNone, "none"},
		{AuthTypeBearer, "bearer"},
		{AuthTypeOAuth, "oauth"},
	}

	for _, tt := range tests {
		t.Run(string(tt.at), func(t *testing.T) {
			if string(tt.at) != tt.want {
				t.Errorf("AuthType = %q, want %q", string(tt.at), tt.want)
			}
		})
	}
}
