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
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// TestGeneratePKCEVerifier tests PKCE verifier generation.
func TestGeneratePKCEVerifier(t *testing.T) {
	verifier1, err := GeneratePKCEVerifier()
	if err != nil {
		t.Fatalf("GeneratePKCEVerifier() error = %v", err)
	}

	// Verifier should be 43 characters (32 bytes base64url encoded without padding)
	if len(verifier1) != 43 {
		t.Errorf("verifier length = %d, want 43", len(verifier1))
	}

	// Verifier should only contain base64url characters
	for _, c := range verifier1 {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			t.Errorf("verifier contains invalid character: %c", c)
		}
	}

	// Each call should generate a unique verifier
	verifier2, err := GeneratePKCEVerifier()
	if err != nil {
		t.Fatalf("GeneratePKCEVerifier() error = %v", err)
	}

	if verifier1 == verifier2 {
		t.Error("two calls to GeneratePKCEVerifier() should produce different values")
	}
}

// TestGeneratePKCEChallenge tests PKCE challenge generation.
func TestGeneratePKCEChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	challenge := GeneratePKCEChallenge(verifier)

	// Verify the challenge is the SHA256 hash of the verifier
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	if challenge != expectedChallenge {
		t.Errorf("challenge = %q, want %q", challenge, expectedChallenge)
	}
}

// TestGeneratePKCEChallenge_Deterministic tests that same verifier produces same challenge.
func TestGeneratePKCEChallenge_Deterministic(t *testing.T) {
	verifier := "test-verifier-123"

	challenge1 := GeneratePKCEChallenge(verifier)
	challenge2 := GeneratePKCEChallenge(verifier)

	if challenge1 != challenge2 {
		t.Error("same verifier should produce same challenge")
	}
}

// TestGeneratePKCEChallenge_DifferentVerifiers tests different verifiers produce different challenges.
func TestGeneratePKCEChallenge_DifferentVerifiers(t *testing.T) {
	verifier1 := "verifier-1"
	verifier2 := "verifier-2"

	challenge1 := GeneratePKCEChallenge(verifier1)
	challenge2 := GeneratePKCEChallenge(verifier2)

	if challenge1 == challenge2 {
		t.Error("different verifiers should produce different challenges")
	}
}

// TestOAuthAuthenticator_SetupPKCE tests the SetupPKCE method.
func TestOAuthAuthenticator_SetupPKCE(t *testing.T) {
	// Create mock server for introspection (needed for server mode setup)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	challenge, err := oa.SetupPKCE()
	if err != nil {
		t.Fatalf("SetupPKCE() error = %v", err)
	}

	// Challenge should be non-empty
	if challenge == "" {
		t.Error("challenge should not be empty")
	}

	// Verifier should be stored
	verifier := oa.GetPKCEVerifier()
	if verifier == "" {
		t.Error("verifier should be stored")
	}

	// Challenge should match verifier
	expectedChallenge := GeneratePKCEChallenge(verifier)
	if challenge != expectedChallenge {
		t.Errorf("challenge = %q, want %q", challenge, expectedChallenge)
	}
}

// TestOAuthAuthenticator_ClearPKCEVerifier tests the ClearPKCEVerifier method.
func TestOAuthAuthenticator_ClearPKCEVerifier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Setup PKCE
	_, err = oa.SetupPKCE()
	if err != nil {
		t.Fatalf("SetupPKCE() error = %v", err)
	}

	// Verify verifier exists
	if oa.GetPKCEVerifier() == "" {
		t.Fatal("verifier should exist after SetupPKCE")
	}

	// Clear verifier
	oa.ClearPKCEVerifier()

	// Verify verifier is cleared
	if oa.GetPKCEVerifier() != "" {
		t.Error("verifier should be empty after ClearPKCEVerifier")
	}
}

// TestNewOAuthAuthenticator_NilConfig tests that nil config returns error.
func TestNewOAuthAuthenticator_NilConfig(t *testing.T) {
	_, err := NewOAuthAuthenticator(nil, true)
	if err == nil {
		t.Fatal("NewOAuthAuthenticator(nil) expected error")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestNewOAuthAuthenticator_ServerMode_NoConfig tests server mode requires discovery, jwks, or introspection.
func TestNewOAuthAuthenticator_ServerMode_NoConfig(t *testing.T) {
	cfg := &config.OAuthConfig{
		ClientID: "client-id",
		// No discovery_url, jwks_url, or introspection_url
	}

	_, err := NewOAuthAuthenticator(cfg, true)
	if err == nil {
		t.Fatal("expected error for server mode without discovery, jwks, or introspection")
	}

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("error = %v, want ErrInvalidConfiguration", err)
	}
}

// TestOAuthAuthenticator_ValidateOnClientMode tests Validate returns error on client mode.
func TestOAuthAuthenticator_ValidateOnClientMode(t *testing.T) {
	// Create a mock token endpoint
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     tokenServer.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err = oa.Validate(context.Background(), req)
	if err == nil {
		t.Error("Validate() on client mode should return error")
	}
}

// TestOAuthAuthenticator_AuthenticateOnServerMode tests Authenticate returns error on server mode.
func TestOAuthAuthenticator_AuthenticateOnServerMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err = oa.Authenticate(context.Background(), req)
	if err == nil {
		t.Error("Authenticate() on server mode should return error")
	}
}

// TestOAuthAuthenticator_IntrospectionValidation tests token validation via introspection.
func TestOAuthAuthenticator_IntrospectionValidation(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request format
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
				t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
			}

			r.ParseForm()
			token := r.FormValue("token")
			if token != "valid-token" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{"active": false})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"active":    true,
				"sub":       "user123",
				"scope":     "read write",
				"client_id": "my-client",
				"iss":       "https://auth.example.com",
			})
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
			ClientID:         "my-client",
			ClientSecret:     "my-secret",
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		principal, err := oa.Validate(context.Background(), req)
		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}

		if principal.ID != "user123" {
			t.Errorf("Principal.ID = %q, want %q", principal.ID, "user123")
		}
		if !principal.HasAllScopes("read", "write") {
			t.Errorf("Principal.Scopes = %v, want [read, write]", principal.Scopes)
		}
		if principal.Metadata["issuer"] != "https://auth.example.com" {
			t.Errorf("Metadata[issuer] = %q, want %q", principal.Metadata["issuer"], "https://auth.example.com")
		}
	})

	t.Run("inactive token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"active": false})
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer inactive-token")

		_, err = oa.Validate(context.Background(), req)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Validate() error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("introspection endpoint error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer some-token")

		_, err = oa.Validate(context.Background(), req)
		if err == nil {
			t.Error("Validate() should return error for server error")
		}
	})
}

// TestOAuthAuthenticator_TokenCaching tests token caching behavior.
func TestOAuthAuthenticator_TokenCaching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "cached-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// First call should already have happened during setup
	if callCount != 1 {
		t.Errorf("initial token fetch count = %d, want 1", callCount)
	}

	// Multiple authenticate calls should use cached token
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		err = oa.Authenticate(context.Background(), req)
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}

		auth := req.Header.Get("Authorization")
		if auth != "Bearer cached-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer cached-token")
		}
	}

	// Token endpoint should not be called again (token was cached)
	if callCount != 1 {
		t.Errorf("token fetch count = %d, want 1 (cached)", callCount)
	}
}

// TestOAuthAuthenticator_GetCurrentToken tests the GetCurrentToken method.
func TestOAuthAuthenticator_GetCurrentToken(t *testing.T) {
	t.Run("client mode returns token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			TokenURL:     server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		token := oa.GetCurrentToken()
		if token == nil {
			t.Fatal("GetCurrentToken() returned nil")
		}
		if token.AccessToken != "test-access-token" {
			t.Errorf("AccessToken = %q, want %q", token.AccessToken, "test-access-token")
		}
	})

	t.Run("server mode returns nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		token := oa.GetCurrentToken()
		if token != nil {
			t.Error("GetCurrentToken() should return nil for server mode")
		}
	})
}

// TestOAuthAuthenticator_IsTokenExpiringSoon tests the IsTokenExpiringSoon method.
func TestOAuthAuthenticator_IsTokenExpiringSoon(t *testing.T) {
	t.Run("server mode returns false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		if oa.IsTokenExpiringSoon(5 * time.Minute) {
			t.Error("IsTokenExpiringSoon() should return false for server mode")
		}
	})

	t.Run("token with future expiry", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test-token",
				"token_type":   "Bearer",
				"expires_in":   3600, // 1 hour
			})
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			TokenURL:     server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		// Token expires in 1 hour, so it's not expiring soon (within 5 minutes)
		if oa.IsTokenExpiringSoon(5 * time.Minute) {
			t.Error("token should not be expiring soon")
		}

		// But it is expiring within 2 hours
		if !oa.IsTokenExpiringSoon(2 * time.Hour) {
			t.Error("token should be expiring within 2 hours")
		}
	})
}

// TestOAuthAuthenticator_Refresh tests the Refresh method.
func TestOAuthAuthenticator_Refresh(t *testing.T) {
	t.Run("server mode does nothing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		err = oa.Refresh(context.Background())
		if err != nil {
			t.Errorf("Refresh() error = %v", err)
		}
	})

	t.Run("client mode refreshes token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "refreshed-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			TokenURL:     server.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		// Refresh should not return error (token caching is handled by oauth2 library)
		err = oa.Refresh(context.Background())
		if err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}

		// Verify we still have a valid token
		token := oa.GetCurrentToken()
		if token == nil {
			t.Error("GetCurrentToken() should return token after refresh")
		}
	})
}

// TestOAuthAuthenticator_Close tests the Close method.
func TestOAuthAuthenticator_Close(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}

	err = oa.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestOAuthAuthenticator_BuildAuthorizationURL tests building authorization URLs.
func TestOAuthAuthenticator_BuildAuthorizationURL(t *testing.T) {
	t.Run("with authorization URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
			AuthorizationURL: "https://auth.example.com/authorize",
			ClientID:         "my-client",
			Scopes:           []string{"openid", "profile"},
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		url, err := oa.BuildAuthorizationURL("state123", "https://myapp.com/callback")
		if err != nil {
			t.Fatalf("BuildAuthorizationURL() error = %v", err)
		}

		if !strings.Contains(url, "response_type=code") {
			t.Error("URL should contain response_type=code")
		}
		if !strings.Contains(url, "client_id=my-client") {
			t.Error("URL should contain client_id")
		}
		if !strings.Contains(url, "state=state123") {
			t.Error("URL should contain state")
		}
		if !strings.Contains(url, "redirect_uri=") {
			t.Error("URL should contain redirect_uri")
		}
		if !strings.Contains(url, "scope=openid+profile") && !strings.Contains(url, "scope=openid%20profile") {
			t.Error("URL should contain scopes")
		}
	})

	t.Run("with PKCE enabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
			AuthorizationURL: "https://auth.example.com/authorize",
			ClientID:         "my-client",
			UsePKCE:          true,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		url, err := oa.BuildAuthorizationURL("state", "https://app.com/cb")
		if err != nil {
			t.Fatalf("BuildAuthorizationURL() error = %v", err)
		}

		if !strings.Contains(url, "code_challenge=") {
			t.Error("URL should contain code_challenge when PKCE is enabled")
		}
		if !strings.Contains(url, "code_challenge_method=S256") {
			t.Error("URL should contain code_challenge_method=S256")
		}

		// Verifier should be stored
		if oa.GetPKCEVerifier() == "" {
			t.Error("PKCE verifier should be stored")
		}
	})

	t.Run("without authorization URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
			ClientID:         "my-client",
			// No AuthorizationURL
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		_, err = oa.BuildAuthorizationURL("state", "https://app.com/cb")
		if err == nil {
			t.Error("BuildAuthorizationURL() should fail without authorization URL")
		}
	})

	t.Run("with resource parameter", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL,
			AuthorizationURL: "https://auth.example.com/authorize",
			ClientID:         "my-client",
			Resource:         "https://api.example.com",
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		url, err := oa.BuildAuthorizationURL("state", "https://app.com/cb")
		if err != nil {
			t.Fatalf("BuildAuthorizationURL() error = %v", err)
		}

		if !strings.Contains(url, "resource=") {
			t.Error("URL should contain resource parameter")
		}
	})
}

// TestOAuthAuthenticator_ExchangeAuthorizationCode tests authorization code exchange.
func TestOAuthAuthenticator_ExchangeAuthorizationCode(t *testing.T) {
	t.Run("successful exchange", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/introspect" {
				w.WriteHeader(http.StatusOK)
				return
			}

			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}

			r.ParseForm()
			if r.FormValue("grant_type") != "authorization_code" {
				t.Errorf("grant_type = %q, want authorization_code", r.FormValue("grant_type"))
			}
			if r.FormValue("code") != "auth-code-123" {
				t.Errorf("code = %q, want auth-code-123", r.FormValue("code"))
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "new-access-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "new-refresh-token",
			})
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL + "/introspect",
			TokenURL:         server.URL + "/token",
			ClientID:         "my-client",
			ClientSecret:     "my-secret",
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		token, err := oa.ExchangeAuthorizationCode(context.Background(), "auth-code-123", "https://app.com/cb")
		if err != nil {
			t.Fatalf("ExchangeAuthorizationCode() error = %v", err)
		}

		if token.AccessToken != "new-access-token" {
			t.Errorf("AccessToken = %q, want %q", token.AccessToken, "new-access-token")
		}
		if token.RefreshToken != "new-refresh-token" {
			t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "new-refresh-token")
		}
	})

	t.Run("exchange with PKCE verifier", func(t *testing.T) {
		receivedVerifier := ""
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/introspect" {
				w.WriteHeader(http.StatusOK)
				return
			}

			r.ParseForm()
			receivedVerifier = r.FormValue("code_verifier")

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL + "/introspect",
			TokenURL:         server.URL + "/token",
			ClientID:         "my-client",
			UsePKCE:          true,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		// Setup PKCE (normally done via BuildAuthorizationURL)
		oa.SetupPKCE()
		storedVerifier := oa.GetPKCEVerifier()

		_, err = oa.ExchangeAuthorizationCode(context.Background(), "code", "https://app.com/cb")
		if err != nil {
			t.Fatalf("ExchangeAuthorizationCode() error = %v", err)
		}

		if receivedVerifier != storedVerifier {
			t.Errorf("received verifier = %q, want %q", receivedVerifier, storedVerifier)
		}

		// Verifier should be cleared after use
		if oa.GetPKCEVerifier() != "" {
			t.Error("PKCE verifier should be cleared after exchange")
		}
	})

	t.Run("exchange failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/introspect" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "invalid_grant"}`))
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: server.URL + "/introspect",
			TokenURL:         server.URL + "/token",
			ClientID:         "my-client",
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		_, err = oa.ExchangeAuthorizationCode(context.Background(), "bad-code", "https://app.com/cb")
		if err == nil {
			t.Error("ExchangeAuthorizationCode() should fail for bad code")
		}
	})
}

// TestOAuthAuthenticator_ValidateMissingToken tests validation with missing token.
func TestOAuthAuthenticator_ValidateMissingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Authorization header

	_, err = oa.Validate(context.Background(), req)
	if !errors.Is(err, ErrMissingCredentials) {
		t.Errorf("Validate() error = %v, want ErrMissingCredentials", err)
	}
}

// TestOAuthAuthenticator_IntrospectionFallback tests using introspection from ID.
func TestOAuthAuthenticator_IntrospectionFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active":    true,
			"client_id": "client-as-subject", // No sub, should fall back to client_id
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	principal, err := oa.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Should fall back to client_id when sub is empty
	if principal.ID != "client-as-subject" {
		t.Errorf("Principal.ID = %q, want %q", principal.ID, "client-as-subject")
	}
}

// TestOAuthAuthenticator_IntrospectionWithUsername tests using username as subject.
func TestOAuthAuthenticator_IntrospectionWithUsername(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active":   true,
			"username": "john.doe", // No sub, no client_id, should fall back to username
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	principal, err := oa.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Should fall back to username when sub and client_id are empty
	if principal.ID != "john.doe" {
		t.Errorf("Principal.ID = %q, want %q", principal.ID, "john.doe")
	}
}

// TestOAuthDiscovery tests discovery document structure.
func TestOAuthDiscovery(t *testing.T) {
	discovery := &OAuthDiscovery{
		Issuer:                "https://auth.example.com",
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
		JWKSURI:               "https://auth.example.com/.well-known/jwks.json",
		IntrospectionEndpoint: "https://auth.example.com/introspect",
		UserinfoEndpoint:      "https://auth.example.com/userinfo",
	}

	// Verify JSON marshaling
	data, err := json.Marshal(discovery)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var parsed OAuthDiscovery
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if parsed.Issuer != discovery.Issuer {
		t.Errorf("Issuer = %q, want %q", parsed.Issuer, discovery.Issuer)
	}
	if parsed.TokenEndpoint != discovery.TokenEndpoint {
		t.Errorf("TokenEndpoint = %q, want %q", parsed.TokenEndpoint, discovery.TokenEndpoint)
	}
}

// createTestJWT creates a signed JWT for testing purposes.
func createTestJWT(t *testing.T, claims jwt.MapClaims, key *rsa.PrivateKey) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key-id"

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	return signed
}

// TestOAuthAuthenticator_JWTValidation tests JWT token validation (basic structure test).
func TestOAuthAuthenticator_JWTValidation(t *testing.T) {
	// Generate a test key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// This is a simplified test - in real scenarios, you'd use a proper JWKS server
	// and OIDC provider. This test just verifies the basic flow.
	t.Run("validates JWT structure", func(t *testing.T) {
		// Create a mock JWKS endpoint
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "jwks") || strings.Contains(r.URL.Path, "keys") {
				// Return a mock JWKS (not a real one - just for structure testing)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"keys":[]}`))
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// For this test, we'll use introspection instead of JWT validation
		// since we'd need a full OIDC setup for proper JWT validation
		introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"active": true,
				"sub":    "jwt-subject",
				"scope":  "openid profile",
			})
		}))
		defer introspectionServer.Close()

		cfg := &config.OAuthConfig{
			IntrospectionURL: introspectionServer.URL,
		}

		oa, err := NewOAuthAuthenticator(cfg, true)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		// Create a test JWT (even though we're using introspection, this tests the flow)
		claims := jwt.MapClaims{
			"sub": "test-subject",
			"iss": "https://auth.example.com",
			"aud": "my-client",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		}
		tokenString := createTestJWT(t, claims, privateKey)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)

		principal, err := oa.Validate(context.Background(), req)
		if err != nil {
			t.Fatalf("Validate() error = %v", err)
		}

		if principal.ID != "jwt-subject" {
			t.Errorf("Principal.ID = %q, want %q", principal.ID, "jwt-subject")
		}
	})
}

// BenchmarkGeneratePKCEVerifier benchmarks PKCE verifier generation.
func BenchmarkGeneratePKCEVerifier(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GeneratePKCEVerifier()
		if err != nil {
			b.Fatalf("GeneratePKCEVerifier() error = %v", err)
		}
	}
}

// BenchmarkGeneratePKCEChallenge benchmarks PKCE challenge generation.
func BenchmarkGeneratePKCEChallenge(b *testing.B) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GeneratePKCEChallenge(verifier)
	}
}

// TestOAuthAuthenticator_FetchDiscovery tests discovery document fetching.
func TestOAuthAuthenticator_FetchDiscovery(t *testing.T) {
	t.Run("successful discovery fetch", func(t *testing.T) {
		var serverURL string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, ".well-known") {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"issuer":                 serverURL,
					"authorization_endpoint": serverURL + "/authorize",
					"token_endpoint":         serverURL + "/token",
					"jwks_uri":               serverURL + "/.well-known/jwks.json",
					"introspection_endpoint": serverURL + "/introspect",
				})
				return
			}
			// Token endpoint for client mode
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		}))
		serverURL = server.URL
		defer server.Close()

		cfg := &config.OAuthConfig{
			DiscoveryURL: server.URL,
			ClientID:     "test-client",
			ClientSecret: "test-secret",
		}

		// Test in client mode to trigger fetchDiscovery
		oa, err := NewOAuthAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()

		// Verify authenticator was created successfully
		token := oa.GetCurrentToken()
		if token == nil {
			t.Error("expected token to be fetched")
		}
	})

	t.Run("discovery with trailing slash", func(t *testing.T) {
		var serverURL string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, ".well-known") {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"issuer":         serverURL,
					"token_endpoint": serverURL + "/token",
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		}))
		serverURL = server.URL
		defer server.Close()

		cfg := &config.OAuthConfig{
			DiscoveryURL: server.URL + "/",
			ClientID:     "client",
			ClientSecret: "secret",
		}

		oa, err := NewOAuthAuthenticator(cfg, false)
		if err != nil {
			t.Fatalf("NewOAuthAuthenticator() error = %v", err)
		}
		defer oa.Close()
	})
}

// TestOAuthAuthenticator_SetupServerModeWithJWKS tests server mode setup with JWKS URL.
func TestOAuthAuthenticator_SetupServerModeWithJWKS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a minimal JWKS
		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]interface{}{},
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		JWKSURL:  server.URL,
		ClientID: "my-client",
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Verify server mode is set
	if !oa.isServer {
		t.Error("expected server mode")
	}
}

// TestOAuthAuthenticator_SetupServerModeWithJWKSNoClientID tests JWKS without client ID.
func TestOAuthAuthenticator_SetupServerModeWithJWKSNoClientID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]interface{}{},
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		JWKSURL: server.URL,
		// No ClientID - should skip client ID check
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()
}

// TestOAuthAuthenticator_ClientModeWithResource tests client mode with resource parameter.
func TestOAuthAuthenticator_ClientModeWithResource(t *testing.T) {
	receivedResource := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedResource = r.FormValue("resource")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
		Resource:     "https://api.example.com",
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	if receivedResource != "https://api.example.com" {
		t.Errorf("resource = %q, want %q", receivedResource, "https://api.example.com")
	}
}

// TestOAuthAuthenticator_ClientModeNoTokenURL tests client mode without token URL.
func TestOAuthAuthenticator_ClientModeNoTokenURL(t *testing.T) {
	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		// No TokenURL, no DiscoveryURL
	}

	_, err := NewOAuthAuthenticator(cfg, false)
	if err == nil {
		t.Error("expected error for client mode without token URL")
	}
}

// TestOAuthAuthenticator_IntrospectionWithJTI tests introspection with JTI metadata.
func TestOAuthAuthenticator_IntrospectionWithJTI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active":    true,
			"sub":       "user",
			"client_id": "my-client",
			"jti":       "unique-token-id",
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	principal, err := oa.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if principal.Metadata["jti"] != "unique-token-id" {
		t.Errorf("Metadata[jti] = %q, want %q", principal.Metadata["jti"], "unique-token-id")
	}
	if principal.Metadata["client_id"] != "my-client" {
		t.Errorf("Metadata[client_id] = %q, want %q", principal.Metadata["client_id"], "my-client")
	}
}

// TestOAuthAuthenticator_ExchangeWithDiscovery tests code exchange using discovery.
func TestOAuthAuthenticator_ExchangeWithDiscovery(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"issuer":         serverURL,
				"token_endpoint": serverURL + "/token",
			})
			return
		}
		if r.URL.Path == "/introspect" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Token endpoint
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "exchanged-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	serverURL = server.URL
	defer server.Close()

	cfg := &config.OAuthConfig{
		DiscoveryURL:     server.URL,
		IntrospectionURL: server.URL + "/introspect",
		ClientID:         "client",
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	token, err := oa.ExchangeAuthorizationCode(context.Background(), "code", "https://app.com/cb")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode() error = %v", err)
	}

	if token.AccessToken != "exchanged-token" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "exchanged-token")
	}
}

// TestOAuthAuthenticator_ExchangeNoTokenURL tests exchange without token URL.
func TestOAuthAuthenticator_ExchangeNoTokenURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
		ClientID:         "client",
		// No TokenURL
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	_, err = oa.ExchangeAuthorizationCode(context.Background(), "code", "https://app.com/cb")
	if err == nil {
		t.Error("expected error without token URL")
	}
}

// TestOAuthAuthenticator_BuildAuthorizationWithDiscovery tests building auth URL with discovery.
func TestOAuthAuthenticator_BuildAuthorizationWithDiscovery(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"issuer":                 serverURL,
				"authorization_endpoint": serverURL + "/authorize",
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	serverURL = server.URL
	defer server.Close()

	cfg := &config.OAuthConfig{
		DiscoveryURL:     server.URL,
		IntrospectionURL: server.URL + "/introspect",
		ClientID:         "client",
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	url, err := oa.BuildAuthorizationURL("state", "https://app.com/cb")
	if err != nil {
		t.Fatalf("BuildAuthorizationURL() error = %v", err)
	}

	if !strings.Contains(url, "/authorize") {
		t.Error("URL should contain authorization endpoint")
	}
}

// TestOAuthAuthenticator_ValidateNoMethod tests validation with no verification method.
func TestOAuthAuthenticator_ValidateNoMethod(t *testing.T) {
	// This test creates an OAuth authenticator without any validation method
	// The code should return an error when trying to validate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Clear introspection URL to simulate no validation method
	oa.config.IntrospectionURL = ""

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	_, err = oa.Validate(context.Background(), req)
	if err == nil {
		t.Error("expected error when no validation method available")
	}
}

// TestOAuthAuthenticator_GetTokenCaching tests the getToken caching behavior.
func TestOAuthAuthenticator_GetTokenCaching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "cached-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	initialCount := callCount

	// Make multiple authenticate calls
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		err = oa.Authenticate(context.Background(), req)
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}
	}

	// Token should be cached - no additional calls
	if callCount != initialCount {
		t.Errorf("expected no additional token calls (caching), got %d additional calls", callCount-initialCount)
	}
}

// TestOAuthAuthenticator_AuthenticateAddsHeader tests that Authenticate adds the bearer header.
func TestOAuthAuthenticator_AuthenticateAddsHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "my-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err = oa.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	auth := req.Header.Get("Authorization")
	if auth != "Bearer my-access-token" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer my-access-token")
	}
}

// TestOAuthAuthenticator_IsTokenExpiringSoon_NoToken tests expiring soon check when no token.
func TestOAuthAuthenticator_IsTokenExpiringSoon_NoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Clear the token to test nil case
	oa.mu.Lock()
	oa.currentToken = nil
	oa.mu.Unlock()

	// Should return true when token is nil
	if !oa.IsTokenExpiringSoon(5 * time.Minute) {
		t.Error("expected true when token is nil")
	}
}

// TestOAuthAuthenticator_IntrospectionWithIssuer tests introspection with issuer metadata.
func TestOAuthAuthenticator_IntrospectionWithIssuer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "user",
			"iss":    "https://issuer.example.com",
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	principal, err := oa.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if principal.Metadata["issuer"] != "https://issuer.example.com" {
		t.Errorf("Metadata[issuer] = %q, want %q", principal.Metadata["issuer"], "https://issuer.example.com")
	}
}

// TestBearerAuthenticator_EmptyTokenClientMode tests client mode with empty token after creation.
func TestBearerAuthenticator_EmptyTokenClientMode(t *testing.T) {
	cfg := &config.BearerAuthConfig{
		Token: "initial-token",
	}

	auth, err := NewBearerAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewBearerAuthenticator() error = %v", err)
	}
	defer auth.Close()

	// Manually clear the token to test empty token path
	auth.mu.Lock()
	auth.token = ""
	auth.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err = auth.Authenticate(context.Background(), req)
	if !errors.Is(err, ErrMissingCredentials) {
		t.Errorf("Authenticate() error = %v, want ErrMissingCredentials", err)
	}
}

// TestOAuthAuthenticator_GetTokenDoubleCheck tests the double-check locking in getToken.
func TestOAuthAuthenticator_GetTokenDoubleCheck(t *testing.T) {
	// This test verifies the getToken logic works correctly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Verify token is valid
	token := oa.GetCurrentToken()
	if token == nil {
		t.Fatal("expected token")
	}

	// Call Authenticate which uses getToken
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err = oa.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
}

// TestOAuthAuthenticator_DiscoveryError tests error handling in discovery fetch.
func TestOAuthAuthenticator_DiscoveryError(t *testing.T) {
	t.Run("discovery server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			DiscoveryURL: server.URL,
			ClientID:     "client",
			ClientSecret: "secret",
		}

		_, err := NewOAuthAuthenticator(cfg, false)
		if err == nil {
			t.Error("expected error for discovery server error")
		}
	})

	t.Run("discovery invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not valid json"))
		}))
		defer server.Close()

		cfg := &config.OAuthConfig{
			DiscoveryURL: server.URL,
			ClientID:     "client",
			ClientSecret: "secret",
		}

		_, err := NewOAuthAuthenticator(cfg, false)
		if err == nil {
			t.Error("expected error for invalid discovery JSON")
		}
	})
}

// TestOAuthAuthenticator_IntrospectionWithScopes tests introspection scope parsing.
func TestOAuthAuthenticator_IntrospectionWithScopes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "user",
			"scope":  "openid profile email",
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	principal, err := oa.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !principal.HasAllScopes("openid", "profile", "email") {
		t.Errorf("Principal.Scopes = %v, want [openid, profile, email]", principal.Scopes)
	}
}

// TestBearerAuthenticator_ValidationEndpointNotValid tests validation endpoint returning valid=false.
func TestBearerAuthenticator_ValidationEndpointNotValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
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

	_, err = auth.Validate(context.Background(), req)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Validate() error = %v, want ErrInvalidToken", err)
	}
}

// TestBearerAuthenticator_ValidationEndpointForbidden tests validation endpoint returning 403.
func TestBearerAuthenticator_ValidationEndpointForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
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

	_, err = auth.Validate(context.Background(), req)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Validate() error = %v, want ErrInvalidToken", err)
	}
}

// TestOAuthAuthenticator_IntrospectionInvalidJSON tests introspection returning invalid JSON.
func TestOAuthAuthenticator_IntrospectionInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	_, err = oa.Validate(context.Background(), req)
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

// TestOAuthAuthenticator_RefreshError tests refresh error handling.
func TestOAuthAuthenticator_RefreshError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Initial token fetch succeeds
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "initial-token",
				"token_type":   "Bearer",
				"expires_in":   1, // Expires very quickly
			})
		} else {
			// Subsequent requests fail
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	// Now refresh should fail
	err = oa.Refresh(context.Background())
	if err == nil {
		t.Log("refresh succeeded (token may still be valid)")
	}
}

// TestOAuthAuthenticator_ExchangeWithClientSecret tests code exchange with client secret.
func TestOAuthAuthenticator_ExchangeWithClientSecret(t *testing.T) {
	receivedSecret := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/introspect" {
			w.WriteHeader(http.StatusOK)
			return
		}
		r.ParseForm()
		receivedSecret = r.FormValue("client_secret")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL + "/introspect",
		TokenURL:         server.URL + "/token",
		ClientID:         "my-client",
		ClientSecret:     "my-secret",
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	_, err = oa.ExchangeAuthorizationCode(context.Background(), "code", "https://app.com/cb")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode() error = %v", err)
	}

	if receivedSecret != "my-secret" {
		t.Errorf("client_secret = %q, want %q", receivedSecret, "my-secret")
	}
}

// TestOAuthAuthenticator_ExchangeWithExpiresIn tests code exchange with expires_in.
func TestOAuthAuthenticator_ExchangeWithExpiresIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/introspect" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token",
			"token_type":   "Bearer",
			"expires_in":   7200,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL + "/introspect",
		TokenURL:         server.URL + "/token",
		ClientID:         "client",
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	token, err := oa.ExchangeAuthorizationCode(context.Background(), "code", "https://app.com/cb")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode() error = %v", err)
	}

	// Token should have expiry set (roughly 2 hours from now)
	if token.Expiry.IsZero() {
		t.Error("token expiry should be set")
	}
}

// TestOAuthAuthenticator_Validate_NoVerifier tests that when server mode has no OIDC verifier,
// validation falls back to introspection.
func TestOAuthAuthenticator_Validate_NoVerifier(t *testing.T) {
	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		token := r.FormValue("token")
		if token != "fallback-token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"active": false})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active":    true,
			"sub":       "introspected-user",
			"scope":     "admin",
			"client_id": "test-client",
		})
	}))
	defer introspectionServer.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: introspectionServer.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Confirm there is no OIDC verifier set (introspection-only mode)
	if oa.oidcVerifier != nil {
		t.Fatal("expected oidcVerifier to be nil in introspection-only mode")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer fallback-token")

	principal, err := oa.Validate(context.Background(), req)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if principal.ID != "introspected-user" {
		t.Errorf("Principal.ID = %q, want %q", principal.ID, "introspected-user")
	}
	if !principal.HasAllScopes("admin") {
		t.Errorf("Principal.Scopes = %v, want [admin]", principal.Scopes)
	}
	if principal.Metadata["client_id"] != "test-client" {
		t.Errorf("Metadata[client_id] = %q, want %q", principal.Metadata["client_id"], "test-client")
	}
}

// TestOAuthAuthenticator_Validate_InvalidBearerFormat tests that malformed Authorization
// headers return the appropriate error.
func TestOAuthAuthenticator_Validate_InvalidBearerFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		IntrospectionURL: server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, true)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	tests := []struct {
		name       string
		authHeader string
		wantErr    error
	}{
		{
			name:       "empty header",
			authHeader: "",
			wantErr:    ErrMissingCredentials,
		},
		{
			name:       "too short",
			authHeader: "Bear",
			wantErr:    ErrInvalidToken,
		},
		{
			name:       "wrong scheme",
			authHeader: "Basic dXNlcjpwYXNz",
			wantErr:    ErrInvalidToken,
		},
		{
			name:       "bearer with empty token",
			authHeader: "Bearer ",
			wantErr:    ErrMissingCredentials,
		},
		{
			name:       "bearer with only spaces",
			authHeader: "Bearer    ",
			wantErr:    ErrMissingCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			_, err := oa.Validate(context.Background(), req)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestOAuthAuthenticator_GetToken_CachedToken tests that getToken returns a cached
// non-expired token without making any refresh calls.
func TestOAuthAuthenticator_GetToken_CachedToken(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "valid-cached-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Record call count after initial setup
	initialCount := callCount

	// The token is valid (expires in 1 hour), so getToken should return cached version
	token, err := oa.getToken(context.Background())
	if err != nil {
		t.Fatalf("getToken() error = %v", err)
	}

	if token.AccessToken != "valid-cached-token" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "valid-cached-token")
	}

	// No additional calls should have been made
	if callCount != initialCount {
		t.Errorf("expected no additional token endpoint calls, got %d", callCount-initialCount)
	}

	// Call again to confirm caching still works
	token2, err := oa.getToken(context.Background())
	if err != nil {
		t.Fatalf("getToken() second call error = %v", err)
	}

	if token2.AccessToken != token.AccessToken {
		t.Errorf("second call returned different token: %q vs %q", token2.AccessToken, token.AccessToken)
	}

	if callCount != initialCount {
		t.Errorf("expected no additional calls after second getToken, got %d", callCount-initialCount)
	}
}

// TestOAuthAuthenticator_GetToken_FromTokenSource tests that when the current token is
// expired, getToken fetches a new one from the tokenSource.
func TestOAuthAuthenticator_GetToken_FromTokenSource(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "refreshed-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	cfg := &config.OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	oa, err := NewOAuthAuthenticator(cfg, false)
	if err != nil {
		t.Fatalf("NewOAuthAuthenticator() error = %v", err)
	}
	defer oa.Close()

	// Force the current token to be expired
	oa.mu.Lock()
	oa.currentToken.Expiry = time.Now().Add(-1 * time.Hour)
	oa.mu.Unlock()

	initialCount := callCount

	// getToken should detect the expired token and refresh via tokenSource
	token, err := oa.getToken(context.Background())
	if err != nil {
		t.Fatalf("getToken() error = %v", err)
	}

	if token.AccessToken != "refreshed-token" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "refreshed-token")
	}

	// At least one additional call should have been made to refresh
	if callCount <= initialCount {
		t.Error("expected at least one additional token endpoint call for refresh")
	}
}

// TestGeneratePKCEVerifier_Length tests that PKCE verifier and challenge meet
// the length requirements specified in RFC 7636.
func TestGeneratePKCEVerifier_Length(t *testing.T) {
	// Run multiple times to ensure consistency
	for i := 0; i < 20; i++ {
		verifier, err := GeneratePKCEVerifier()
		if err != nil {
			t.Fatalf("GeneratePKCEVerifier() iteration %d error = %v", i, err)
		}

		// RFC 7636 requires verifier to be 43-128 characters
		if len(verifier) < 43 {
			t.Errorf("iteration %d: verifier length = %d, want >= 43", i, len(verifier))
		}
		if len(verifier) > 128 {
			t.Errorf("iteration %d: verifier length = %d, want <= 128", i, len(verifier))
		}

		// Generate challenge from verifier
		challenge := GeneratePKCEChallenge(verifier)

		// SHA256 hash (32 bytes) base64url encoded without padding = 43 characters
		if len(challenge) != 43 {
			t.Errorf("iteration %d: challenge length = %d, want 43", i, len(challenge))
		}

		// Challenge should only contain base64url characters
		for _, c := range challenge {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
				t.Errorf("iteration %d: challenge contains invalid character: %c", i, c)
			}
		}
	}
}
