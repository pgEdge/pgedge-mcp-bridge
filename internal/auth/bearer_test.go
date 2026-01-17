package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// TestBearerAuthenticator_ServerMode_ValidToken tests server mode with a valid token.
func TestBearerAuthenticator_ServerMode_ValidToken(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token-1", "valid-token-2"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token-1")

	principal, err := auth.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if principal == nil {
		t.Fatal("Validate() returned nil principal")
	}
	if principal.Type != PrincipalTypeToken {
		t.Errorf("Principal.Type = %v, want %v", principal.Type, PrincipalTypeToken)
	}
}

// TestBearerAuthenticator_ServerMode_InvalidToken tests server mode with an invalid token.
func TestBearerAuthenticator_ServerMode_InvalidToken(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	_, err = auth.Validate(context.Background(), req)
	if err == nil {
		t.Fatal("Validate() expected error for invalid token")
	}

	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Validate() error = %v, want ErrInvalidToken", err)
	}
}

// TestBearerAuthenticator_ServerMode_MissingToken tests server mode with missing token.
func TestBearerAuthenticator_ServerMode_MissingToken(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	// No Authorization header

	_, err = auth.Validate(context.Background(), req)
	if err == nil {
		t.Fatal("Validate() expected error for missing token")
	}

	if !errors.Is(err, ErrMissingCredentials) {
		t.Errorf("Validate() error = %v, want ErrMissingCredentials", err)
	}
}

// TestBearerAuthenticator_ServerMode_EmptyToken tests server mode with empty bearer token.
func TestBearerAuthenticator_ServerMode_EmptyToken(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer ")

	_, err = auth.Validate(context.Background(), req)
	if err == nil {
		t.Fatal("Validate() expected error for empty token")
	}

	if !errors.Is(err, ErrMissingCredentials) {
		t.Errorf("Validate() error = %v, want ErrMissingCredentials", err)
	}
}

// TestBearerAuthenticator_ServerMode_WrongScheme tests server mode with wrong auth scheme.
func TestBearerAuthenticator_ServerMode_WrongScheme(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	_, err = auth.Validate(context.Background(), req)
	if err == nil {
		t.Fatal("Validate() expected error for wrong scheme")
	}

	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Validate() error = %v, want ErrInvalidToken", err)
	}
}

// TestBearerAuthenticator_ServerMode_CaseInsensitiveBearer tests case-insensitive Bearer scheme.
func TestBearerAuthenticator_ServerMode_CaseInsensitiveBearer(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	testCases := []string{"bearer valid-token", "BEARER valid-token", "Bearer valid-token", "BeArEr valid-token"}

	for _, authHeader := range testCases {
		t.Run(authHeader, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.Header.Set("Authorization", authHeader)

			principal, err := auth.Validate(context.Background(), req)
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if principal == nil {
				t.Error("Validate() returned nil principal")
			}
		})
	}
}

// TestBearerAuthenticator_ClientMode_AddsHeader tests client mode adds correct header.
func TestBearerAuthenticator_ClientMode_AddsHeader(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		Token: "my-client-token",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)

	err = auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	got := req.Header.Get("Authorization")
	want := "Bearer my-client-token"
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// TestBearerAuthenticator_ClientMode_TokenFromEnv tests client mode with token from env.
func TestBearerAuthenticator_ClientMode_TokenFromEnv(t *testing.T) {
	t.Setenv("TEST_AUTH_TOKEN", "env-token-value")

	cfg := &config.BearerAuthConfig{
		TokenEnv: "TEST_AUTH_TOKEN",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)

	err = auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	got := req.Header.Get("Authorization")
	want := "Bearer env-token-value"
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// TestBearerAuthenticator_ClientMode_EnvOverridesToken tests env takes precedence.
func TestBearerAuthenticator_ClientMode_EnvOverridesToken(t *testing.T) {
	t.Setenv("TEST_AUTH_TOKEN", "env-token")

	cfg := &config.BearerAuthConfig{
		Token:    "config-token",
		TokenEnv: "TEST_AUTH_TOKEN",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	err = auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	got := req.Header.Get("Authorization")
	want := "Bearer env-token"
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// TestBearerAuthenticator_ClientMode_FallbackToToken tests fallback to token when env empty.
func TestBearerAuthenticator_ClientMode_FallbackToToken(t *testing.T) {
	// Ensure env var is not set
	t.Setenv("NONEXISTENT_TOKEN_VAR", "")

	cfg := &config.BearerAuthConfig{
		Token:    "fallback-token",
		TokenEnv: "NONEXISTENT_TOKEN_VAR",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	err = auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	got := req.Header.Get("Authorization")
	want := "Bearer fallback-token"
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// TestBearerAuthenticator_UpdateValidTokens tests updating valid tokens.
func TestBearerAuthenticator_UpdateValidTokens(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"old-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	// Verify old token works
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer old-token")
	_, err = auth.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("old token should be valid: %v", err)
	}

	// Update tokens
	auth.UpdateValidTokens([]string{"new-token-1", "new-token-2"})

	// Verify old token no longer works
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer old-token")
	_, err = auth.Validate(context.Background(), req)
	if err == nil {
		t.Error("old token should no longer be valid")
	}

	// Verify new tokens work
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer new-token-1")
	_, err = auth.Validate(context.Background(), req)
	if err != nil {
		t.Errorf("new-token-1 should be valid: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer new-token-2")
	_, err = auth.Validate(context.Background(), req)
	if err != nil {
		t.Errorf("new-token-2 should be valid: %v", err)
	}
}

// TestBearerAuthenticator_UpdateValidTokens_ClientMode tests UpdateValidTokens is no-op in client mode.
func TestBearerAuthenticator_UpdateValidTokens_ClientMode(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		Token: "client-token",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	// This should be a no-op
	auth.UpdateValidTokens([]string{"some-token"})

	// Client mode should still work
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err = auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Errorf("Authenticate() error = %v", err)
	}
}

// TestBearerAuthenticator_SetToken tests the SetToken method.
func TestBearerAuthenticator_SetToken(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		Token: "initial-token",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	// Verify initial token
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	auth.Authenticate(context.Background(), req)
	if got := req.Header.Get("Authorization"); got != "Bearer initial-token" {
		t.Errorf("initial Authorization = %q, want %q", got, "Bearer initial-token")
	}

	// Update token
	auth.SetToken("updated-token")

	// Verify updated token
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	auth.Authenticate(context.Background(), req)
	if got := req.Header.Get("Authorization"); got != "Bearer updated-token" {
		t.Errorf("updated Authorization = %q, want %q", got, "Bearer updated-token")
	}
}

// TestBearerAuthenticator_SetToken_ServerMode tests SetToken is no-op in server mode.
func TestBearerAuthenticator_SetToken_ServerMode(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"valid-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	// This should be a no-op
	auth.SetToken("new-token")

	// Server mode should still validate the original token
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	_, err = auth.Validate(context.Background(), req)
	if err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

// TestBearerAuthenticator_ConstantTimeComparison tests that token comparison works.
func TestBearerAuthenticator_ConstantTimeComparison(t *testing.T) {
	// This test verifies that the constant-time comparison function works correctly
	// We can't easily test timing characteristics, but we verify correctness
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"secret-token-abc123"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	testCases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"exact match", "secret-token-abc123", false},
		{"wrong token", "wrong-token", true},
		{"prefix match", "secret-token", true},
		{"suffix match", "token-abc123", true},
		{"similar length", "secret-token-abc124", true},
		{"empty token", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.token == "" {
				// Don't set header for empty token test
			} else {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}

			_, err := auth.Validate(context.Background(), req)
			if tc.wantErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestBearerAuthenticator_Refresh tests the Refresh method.
func TestBearerAuthenticator_Refresh(t *testing.T) {
	t.Run("server mode does nothing", func(t *testing.T) {
		cfg := &config.BearerAuthConfig{
			ValidTokens: []string{"token"},
		}
		auth, err := NewBearerAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		err = auth.Refresh(context.Background())
		if err != nil {
			t.Errorf("Refresh() error = %v", err)
		}
	})

	t.Run("client mode without tokenEnv does nothing", func(t *testing.T) {
		cfg := &config.BearerAuthConfig{
			Token: "static-token",
		}
		auth, err := NewBearerAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		err = auth.Refresh(context.Background())
		if err != nil {
			t.Errorf("Refresh() error = %v", err)
		}
	})

	t.Run("client mode refreshes from env", func(t *testing.T) {
		t.Setenv("REFRESH_TOKEN_TEST", "initial-token")

		cfg := &config.BearerAuthConfig{
			TokenEnv: "REFRESH_TOKEN_TEST",
		}
		auth, err := NewBearerAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		// Change env var
		t.Setenv("REFRESH_TOKEN_TEST", "refreshed-token")

		err = auth.Refresh(context.Background())
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}

		// Verify new token is used
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		auth.Authenticate(context.Background(), req)
		got := req.Header.Get("Authorization")
		want := "Bearer refreshed-token"
		if got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
	})

	t.Run("client mode refresh fails with empty env", func(t *testing.T) {
		t.Setenv("EMPTY_TOKEN_TEST", "initial")

		cfg := &config.BearerAuthConfig{
			TokenEnv: "EMPTY_TOKEN_TEST",
		}
		auth, err := NewBearerAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		// Clear the env var
		t.Setenv("EMPTY_TOKEN_TEST", "")

		err = auth.Refresh(context.Background())
		if !errors.Is(err, ErrTokenRefreshFailed) {
			t.Errorf("Refresh() error = %v, want ErrTokenRefreshFailed", err)
		}
	})
}

// TestBearerAuthenticator_ValidationEndpoint tests remote token validation.
func TestBearerAuthenticator_ValidationEndpoint(t *testing.T) {
	t.Run("valid token via endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var reqBody struct {
				Token string `json:"token"`
			}
			json.NewDecoder(r.Body).Decode(&reqBody)

			if reqBody.Token == "valid-remote-token" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"valid":   true,
					"subject": "user123",
					"scopes":  []string{"read", "write"},
				})
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{"valid": false})
			}
		}))
		defer server.Close()

		cfg := &config.BearerAuthConfig{
			ValidationEndpoint: server.URL,
		}

		auth, err := NewBearerAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer valid-remote-token")

		principal, err := auth.Validate(context.Background(), req)
		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}

		if principal.ID != "user123" {
			t.Errorf("Principal.ID = %q, want %q", principal.ID, "user123")
		}
		if principal.Type != PrincipalTypeService {
			t.Errorf("Principal.Type = %v, want %v", principal.Type, PrincipalTypeService)
		}
		if !principal.HasScope("read") || !principal.HasScope("write") {
			t.Errorf("Principal.Scopes = %v, want [read, write]", principal.Scopes)
		}
	})

	t.Run("invalid token via endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{"valid": false})
		}))
		defer server.Close()

		cfg := &config.BearerAuthConfig{
			ValidationEndpoint: server.URL,
		}

		auth, err := NewBearerAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")

		_, err = auth.Validate(context.Background(), req)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Validate() error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("validation endpoint returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := &config.BearerAuthConfig{
			ValidationEndpoint: server.URL,
		}

		auth, err := NewBearerAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer some-token")

		_, err = auth.Validate(context.Background(), req)
		if err == nil {
			t.Error("Validate() expected error for server error")
		}
	})

	t.Run("local tokens checked before endpoint", func(t *testing.T) {
		endpointCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			endpointCalled = true
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		cfg := &config.BearerAuthConfig{
			ValidTokens:        []string{"local-token"},
			ValidationEndpoint: server.URL,
		}

		auth, err := NewBearerAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewBearerAuthenticator() error = %v", err)
		}
		defer auth.Close()

		// Use local token - endpoint should not be called
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer local-token")

		_, err = auth.Validate(context.Background(), req)
		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}

		if endpointCalled {
			t.Error("validation endpoint was called but should not be for local token")
		}
	})
}

// TestBearerAuthenticator_ServerMode_ValidateCalledOnClientMode tests error when Validate called on client mode.
func TestBearerAuthenticator_ServerMode_ValidateCalledOnClientMode(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		Token: "client-token",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")

	_, err = auth.Validate(context.Background(), req)
	if err == nil {
		t.Error("Validate() on client mode should return error")
	}
}

// TestBearerAuthenticator_ClientMode_AuthenticateCalledOnServerMode tests error when Authenticate called on server mode.
func TestBearerAuthenticator_ClientMode_AuthenticateCalledOnServerMode(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"server-token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	err = auth.Authenticate(context.Background(), req)
	if err == nil {
		t.Error("Authenticate() on server mode should return error")
	}
}

// TestNewBearerAuthenticator_NilConfig tests that nil config returns error.
func TestNewBearerAuthenticator_NilConfig(t *testing.T) {
	_, err := NewBearerAuthenticator(nil, true)
	if err == nil {
		t.Fatal("NewBearerAuthenticator(nil) expected error")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestNewBearerAuthenticator_ServerMode_NoTokensOrEndpoint tests server mode requires tokens or endpoint.
func TestNewBearerAuthenticator_ServerMode_NoTokensOrEndpoint(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		// No ValidTokens, no ValidationEndpoint
	}

	_, err := NewBearerAuthenticator(cfg, true)
	if err == nil {
		t.Fatal("expected error for server mode without tokens or endpoint")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestNewBearerAuthenticator_ClientMode_NoToken tests client mode requires token.
func TestNewBearerAuthenticator_ClientMode_NoToken(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		// No Token, no TokenEnv
	}

	_, err := NewBearerAuthenticator(cfg, false)
	if err == nil {
		t.Fatal("expected error for client mode without token")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestExtractBearerToken tests the extractBearerToken helper function.
func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantToken  string
		wantErr    error
	}{
		{
			name:       "valid bearer token",
			authHeader: "Bearer abc123",
			wantToken:  "abc123",
		},
		{
			name:       "lowercase bearer",
			authHeader: "bearer abc123",
			wantToken:  "abc123",
		},
		{
			name:       "mixed case bearer",
			authHeader: "BEARER abc123",
			wantToken:  "abc123",
		},
		{
			name:       "token with extra spaces",
			authHeader: "Bearer   abc123  ",
			wantToken:  "abc123",
		},
		{
			name:       "missing header",
			authHeader: "",
			wantErr:    ErrMissingCredentials,
		},
		{
			name:       "wrong scheme",
			authHeader: "Basic abc123",
			wantErr:    ErrInvalidToken,
		},
		{
			name:       "empty token",
			authHeader: "Bearer ",
			wantErr:    ErrMissingCredentials,
		},
		{
			name:       "header too short",
			authHeader: "Be",
			wantErr:    ErrInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			got, err := extractBearerToken(req)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("extractBearerToken() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("extractBearerToken() error = %v", err)
			}

			if got != tt.wantToken {
				t.Errorf("extractBearerToken() = %q, want %q", got, tt.wantToken)
			}
		})
	}
}

// TestBearerAuthenticator_Close tests the Close method.
func TestBearerAuthenticator_Close(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		ValidTokens: []string{"token"},
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}

	err = auth.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestBearerAuthenticator_ValidationEndpointWithMetadata tests that metadata from validation response is captured.
func TestBearerAuthenticator_ValidationEndpointWithMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":   true,
			"subject": "service-abc",
			"scopes":  []string{"api:read", "api:write"},
			"claims": map[string]interface{}{
				"iss": "https://auth.example.com",
				"aud": "my-api",
			},
			"metadata": map[string]string{
				"environment": "production",
				"region":      "us-west-2",
			},
		})
	}))
	defer server.Close()

	cfg := &config.BearerAuthConfig{
		ValidationEndpoint: server.URL,
	}

	auth, err := NewBearerAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	principal, err := auth.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Check subject/ID
	if principal.ID != "service-abc" {
		t.Errorf("Principal.ID = %q, want %q", principal.ID, "service-abc")
	}

	// Check scopes
	if !principal.HasAllScopes("api:read", "api:write") {
		t.Errorf("Principal.Scopes = %v, want [api:read, api:write]", principal.Scopes)
	}

	// Check claims
	if iss := principal.GetStringClaim("iss"); iss != "https://auth.example.com" {
		t.Errorf("claim 'iss' = %q, want %q", iss, "https://auth.example.com")
	}

	// Check metadata
	if env := principal.Metadata["environment"]; env != "production" {
		t.Errorf("metadata 'environment' = %q, want %q", env, "production")
	}
	if region := principal.Metadata["region"]; region != "us-west-2" {
		t.Errorf("metadata 'region' = %q, want %q", region, "us-west-2")
	}
}
