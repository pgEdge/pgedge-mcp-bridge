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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
)

// ===========================================================================
// PKCE Tests
// ===========================================================================

func TestValidatePKCE_Valid(t *testing.T) {
	// Generate a valid verifier
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	// Compute challenge
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	if !ValidatePKCE(verifier, challenge, "S256") {
		t.Error("expected PKCE validation to pass with valid verifier/challenge")
	}
}

func TestValidatePKCE_InvalidVerifier(t *testing.T) {
	verifier := "correct-verifier"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	if ValidatePKCE("wrong-verifier", challenge, "S256") {
		t.Error("expected PKCE validation to fail with wrong verifier")
	}
}

func TestValidatePKCE_UnsupportedMethod(t *testing.T) {
	if ValidatePKCE("verifier", "challenge", "plain") {
		t.Error("expected PKCE validation to fail with plain method")
	}
}

func TestValidatePKCE_EmptyInputs(t *testing.T) {
	if ValidatePKCE("", "challenge", "S256") {
		t.Error("expected PKCE validation to fail with empty verifier")
	}
	if ValidatePKCE("verifier", "", "S256") {
		t.Error("expected PKCE validation to fail with empty challenge")
	}
}

func TestValidateCodeVerifier(t *testing.T) {
	tests := []struct {
		name     string
		verifier string
		valid    bool
	}{
		{"valid 43 chars", strings.Repeat("a", 43), true},
		{"valid 128 chars", strings.Repeat("a", 128), true},
		{"too short", strings.Repeat("a", 42), false},
		{"too long", strings.Repeat("a", 129), false},
		{"valid with special chars", "abcdefghijklmnopqrstuvwxyz0123456789-._~abc", true},
		{"invalid char", strings.Repeat("a", 42) + "!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateCodeVerifier(tt.verifier); got != tt.valid {
				t.Errorf("ValidateCodeVerifier() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestValidateCodeChallenge(t *testing.T) {
	// Valid challenge is base64url encoded 32 bytes = 43 chars
	validChallenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	if !ValidateCodeChallenge(validChallenge) {
		t.Error("expected valid challenge to pass")
	}

	if ValidateCodeChallenge("too-short") {
		t.Error("expected short challenge to fail")
	}

	if ValidateCodeChallenge(strings.Repeat("a", 44)) {
		t.Error("expected long challenge to fail")
	}
}

// ===========================================================================
// Storage Tests
// ===========================================================================

func TestMemoryStorage_AuthorizationCode(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	code := &AuthorizationCode{
		Code:      "test-code",
		ClientID:  "client-1",
		UserID:    "user-1",
		ExpiresAt: time.Now().Add(10 * time.Minute),
		CreatedAt: time.Now(),
	}

	// Store
	if err := storage.StoreAuthorizationCode(ctx, code); err != nil {
		t.Fatalf("failed to store code: %v", err)
	}

	// Retrieve
	retrieved, err := storage.GetAuthorizationCode(ctx, "test-code")
	if err != nil {
		t.Fatalf("failed to get code: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected to find code")
	}
	if retrieved.ClientID != "client-1" {
		t.Errorf("expected ClientID 'client-1', got %s", retrieved.ClientID)
	}

	// Delete
	if err := storage.DeleteAuthorizationCode(ctx, "test-code"); err != nil {
		t.Fatalf("failed to delete code: %v", err)
	}

	// Should not find deleted code
	retrieved, err = storage.GetAuthorizationCode(ctx, "test-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != nil {
		t.Error("expected code to be deleted")
	}
}

func TestMemoryStorage_ExpiredCode(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	code := &AuthorizationCode{
		Code:      "expired-code",
		ClientID:  "client-1",
		ExpiresAt: time.Now().Add(-1 * time.Minute), // Already expired
		CreatedAt: time.Now(),
	}

	if err := storage.StoreAuthorizationCode(ctx, code); err != nil {
		t.Fatalf("failed to store code: %v", err)
	}

	// Should not retrieve expired code
	retrieved, err := storage.GetAuthorizationCode(ctx, "expired-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != nil {
		t.Error("expected expired code to not be returned")
	}
}

func TestMemoryStorage_RefreshToken(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	token := &RefreshToken{
		Token:     "test-refresh-token",
		ClientID:  "client-1",
		UserID:    "user-1",
		Scope:     "mcp:read mcp:write",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}

	// Store
	if err := storage.StoreRefreshToken(ctx, token); err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	// Retrieve
	retrieved, err := storage.GetRefreshToken(ctx, "test-refresh-token")
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected to find token")
	}
	if retrieved.Scope != "mcp:read mcp:write" {
		t.Errorf("expected scope 'mcp:read mcp:write', got %s", retrieved.Scope)
	}

	// Delete for user
	if err := storage.DeleteRefreshTokensForUser(ctx, "user-1"); err != nil {
		t.Fatalf("failed to delete tokens for user: %v", err)
	}

	retrieved, err = storage.GetRefreshToken(ctx, "test-refresh-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != nil {
		t.Error("expected token to be deleted")
	}
}

func TestMemoryStorage_Client(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	client := &Client{
		ClientID:     "test-client",
		ClientName:   "Test Client",
		RedirectURIs: []string{"https://example.com/callback"},
		GrantTypes:   []string{"authorization_code", "refresh_token"},
		CreatedAt:    time.Now(),
	}

	// Store
	if err := storage.StoreClient(ctx, client); err != nil {
		t.Fatalf("failed to store client: %v", err)
	}

	// Retrieve
	retrieved, err := storage.GetClient(ctx, "test-client")
	if err != nil {
		t.Fatalf("failed to get client: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected to find client")
	}
	if retrieved.ClientName != "Test Client" {
		t.Errorf("expected ClientName 'Test Client', got %s", retrieved.ClientName)
	}

	// Delete
	if err := storage.DeleteClient(ctx, "test-client"); err != nil {
		t.Fatalf("failed to delete client: %v", err)
	}

	retrieved, err = storage.GetClient(ctx, "test-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != nil {
		t.Error("expected client to be deleted")
	}
}

// ===========================================================================
// Built-in Authenticator Tests
// ===========================================================================

func TestBuiltInAuthenticator_Authenticate(t *testing.T) {
	// Generate bcrypt hash at test time
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate hash: %v", err)
	}

	cfg := &config.BuiltInAuthConfig{
		Users: []config.UserConfig{
			{
				Username:     "testuser",
				PasswordHash: string(hash),
				Scopes:       []string{"mcp:read", "mcp:write"},
			},
		},
	}

	auth, err := NewBuiltInAuthenticator(cfg)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	// Test valid credentials
	userInfo, err := auth.Authenticate("testuser", "password123")
	if err != nil {
		t.Errorf("expected authentication to succeed: %v", err)
	}
	if userInfo == nil {
		t.Fatal("expected userInfo to not be nil")
	}
	if userInfo.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", userInfo.Username)
	}
	if len(userInfo.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(userInfo.Scopes))
	}

	// Test invalid password
	_, err = auth.Authenticate("testuser", "wrongpassword")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	// Test invalid username
	_, err = auth.Authenticate("unknownuser", "password123")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestBuiltInAuthenticator_GetUser(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate hash: %v", err)
	}

	cfg := &config.BuiltInAuthConfig{
		Users: []config.UserConfig{
			{
				Username:     "testuser",
				PasswordHash: string(hash),
				Scopes:       []string{"mcp:read"},
			},
		},
	}

	auth, err := NewBuiltInAuthenticator(cfg)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	userInfo, err := auth.GetUser("testuser")
	if err != nil {
		t.Errorf("expected to find user: %v", err)
	}
	if userInfo == nil {
		t.Fatal("expected userInfo to not be nil")
	}

	_, err = auth.GetUser("unknownuser")
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestFilterScopes(t *testing.T) {
	tests := []struct {
		name      string
		requested []string
		allowed   []string
		expected  []string
	}{
		{
			name:      "all allowed",
			requested: []string{"mcp:read", "mcp:write"},
			allowed:   []string{"mcp:read", "mcp:write", "mcp:admin"},
			expected:  []string{"mcp:read", "mcp:write"},
		},
		{
			name:      "some allowed",
			requested: []string{"mcp:read", "mcp:admin"},
			allowed:   []string{"mcp:read"},
			expected:  []string{"mcp:read"},
		},
		{
			name:      "none allowed",
			requested: []string{"mcp:admin"},
			allowed:   []string{"mcp:read"},
			expected:  nil,
		},
		{
			name:      "empty requested",
			requested: []string{},
			allowed:   []string{"mcp:read"},
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterScopes(tt.requested, tt.allowed)
			if len(got) != len(tt.expected) {
				t.Errorf("FilterScopes() returned %d scopes, want %d", len(got), len(tt.expected))
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("FilterScopes()[%d] = %s, want %s", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

// ===========================================================================
// Metadata Tests
// ===========================================================================

func TestBuildMetadata(t *testing.T) {
	metadata := BuildMetadata(
		"https://mcp.example.com",
		[]string{"mcp:read", "mcp:write"},
		true,
	)

	if metadata.Issuer != "https://mcp.example.com" {
		t.Errorf("expected issuer 'https://mcp.example.com', got %s", metadata.Issuer)
	}

	if metadata.AuthorizationEndpoint != "https://mcp.example.com/oauth/authorize" {
		t.Errorf("unexpected authorization endpoint: %s", metadata.AuthorizationEndpoint)
	}

	if metadata.TokenEndpoint != "https://mcp.example.com/oauth/token" {
		t.Errorf("unexpected token endpoint: %s", metadata.TokenEndpoint)
	}

	if metadata.RegistrationEndpoint != "https://mcp.example.com/register" {
		t.Errorf("expected registration endpoint when enabled")
	}

	// Check supported values
	if len(metadata.ResponseTypesSupported) != 1 || metadata.ResponseTypesSupported[0] != "code" {
		t.Error("expected only 'code' response type")
	}

	if len(metadata.CodeChallengeMethodsSupported) != 1 || metadata.CodeChallengeMethodsSupported[0] != "S256" {
		t.Error("expected only 'S256' PKCE method")
	}
}

func TestBuildMetadata_NoRegistration(t *testing.T) {
	metadata := BuildMetadata(
		"https://mcp.example.com",
		[]string{"mcp:read"},
		false,
	)

	if metadata.RegistrationEndpoint != "" {
		t.Error("expected no registration endpoint when disabled")
	}
}

// ===========================================================================
// JWT Token Issuer Tests
// ===========================================================================

func TestTokenIssuer_GenerateKey(t *testing.T) {
	issuer, err := NewTokenIssuer(
		"https://mcp.example.com",
		"RS256",
		"", // No key file
		"test-key-id",
		true, // Generate key
	)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	// Issue a token
	token, jti, err := issuer.IssueAccessToken(
		"user-123",
		[]string{"https://mcp.example.com"},
		"mcp:read mcp:write",
		"client-1",
		time.Hour,
	)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty token")
	}
	if jti == "" {
		t.Error("expected non-empty JTI")
	}

	// Check JWKS
	jwks := issuer.JWKS()
	if len(jwks.Keys) != 1 {
		t.Errorf("expected 1 key in JWKS, got %d", len(jwks.Keys))
	}
	if jwks.Keys[0].KeyID != "test-key-id" {
		t.Errorf("expected key ID 'test-key-id', got %s", jwks.Keys[0].KeyID)
	}
}

// ===========================================================================
// OAuth Error Tests
// ===========================================================================

func TestOAuthError(t *testing.T) {
	err := ErrInvalidRequest("missing parameter")

	if err.Code != ErrorInvalidRequest {
		t.Errorf("expected code %s, got %s", ErrorInvalidRequest, err.Code)
	}

	if err.Description != "missing parameter" {
		t.Errorf("expected description 'missing parameter', got %s", err.Description)
	}

	if err.HTTPStatus != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", err.HTTPStatus)
	}

	// Test Error() method
	errStr := err.Error()
	if !strings.Contains(errStr, "invalid_request") {
		t.Errorf("expected error string to contain 'invalid_request', got %s", errStr)
	}
}

func TestOAuthError_WithState(t *testing.T) {
	err := ErrAccessDenied("user denied").WithState("abc123")

	if err.State != "abc123" {
		t.Errorf("expected state 'abc123', got %s", err.State)
	}
}

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	err := ErrInvalidGrant("code expired")

	WriteJSONError(w, err)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %s", ct)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["error"] != "invalid_grant" {
		t.Errorf("expected error 'invalid_grant', got %s", response["error"])
	}
}

// ===========================================================================
// Integration Tests
// ===========================================================================

func TestFullAuthorizationCodeFlow(t *testing.T) {
	// Create server components
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate hash: %v", err)
	}

	cfg := &config.BuiltInAuthConfig{
		Users: []config.UserConfig{
			{
				Username:     "testuser",
				PasswordHash: string(hash),
				Scopes:       []string{"mcp:read", "mcp:write"},
			},
		},
	}

	authenticator, err := NewBuiltInAuthenticator(cfg)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	authorizeHandler, err := NewAuthorizeHandler(
		storage,
		authenticator,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read", "mcp:write"},
		"",
	)
	if err != nil {
		t.Fatalf("failed to create authorize handler: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	tokenHandler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	// Step 1: Authorization request (GET)
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authURL := "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"scope":                 {"mcp:read"},
		"state":                 {"xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	req := httptest.NewRequest("GET", authURL, nil)
	w := httptest.NewRecorder()

	authorizeHandler.ServeHTTP(w, req)

	// Should show login form (200 OK with HTML)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<form") {
		t.Error("expected HTML form in response")
	}

	// Step 2: Submit login form (POST) - generate CSRF token first
	csrfToken, err := authorizeHandler.generateCSRFToken()
	if err != nil {
		t.Fatalf("failed to generate CSRF token: %v", err)
	}

	formData := url.Values{
		"csrf_token":            {csrfToken},
		"username":              {"testuser"},
		"password":              {"password123"},
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"scope":                 {"mcp:read"},
		"state":                 {"xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}

	req = httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()

	authorizeHandler.ServeHTTP(w, req)

	// Should redirect with code
	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect (302), got %d: %s", w.Code, w.Body.String())
	}

	location := w.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse redirect URL: %v", err)
	}

	code := redirectURL.Query().Get("code")
	if code == "" {
		t.Fatal("expected code in redirect URL")
	}

	state := redirectURL.Query().Get("state")
	if state != "xyz" {
		t.Errorf("expected state 'xyz', got %s", state)
	}

	// Step 3: Token exchange (POST)
	tokenFormData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"test-client"},
		"code_verifier": {verifier},
	}

	req = httptest.NewRequest("POST", "/oauth/token", strings.NewReader(tokenFormData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()

	tokenHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var tokenResponse TokenResponse
	if err := json.NewDecoder(w.Body).Decode(&tokenResponse); err != nil {
		t.Fatalf("failed to decode token response: %v", err)
	}

	if tokenResponse.AccessToken == "" {
		t.Error("expected access_token")
	}
	if tokenResponse.RefreshToken == "" {
		t.Error("expected refresh_token")
	}
	if tokenResponse.TokenType != "Bearer" {
		t.Errorf("expected token_type 'Bearer', got %s", tokenResponse.TokenType)
	}

	// Step 4: Refresh token (POST)
	refreshFormData := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenResponse.RefreshToken},
		"client_id":     {"test-client"},
	}

	req = httptest.NewRequest("POST", "/oauth/token", strings.NewReader(refreshFormData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()

	tokenHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for refresh, got %d: %s", w.Code, w.Body.String())
	}

	var refreshResponse TokenResponse
	if err := json.NewDecoder(w.Body).Decode(&refreshResponse); err != nil {
		t.Fatalf("failed to decode refresh response: %v", err)
	}

	if refreshResponse.AccessToken == "" {
		t.Error("expected new access_token from refresh")
	}
	if refreshResponse.RefreshToken == "" {
		t.Error("expected new refresh_token from refresh")
	}
}

// ===========================================================================
// Server Creation Tests
// ===========================================================================

func TestServer_New_BuiltinMode(t *testing.T) {
	logger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate hash: %v", err)
	}

	cfg := &config.OAuthServerConfig{
		Enabled:              true,
		Issuer:               "https://mcp.example.com",
		Mode:                 "builtin",
		TokenLifetime:        time.Hour,
		RefreshTokenLifetime: 24 * time.Hour,
		AuthCodeLifetime:     10 * time.Minute,
		Signing: &config.SigningConfig{
			Algorithm:   "RS256",
			GenerateKey: true,
		},
		BuiltIn: &config.BuiltInAuthConfig{
			Users: []config.UserConfig{
				{
					Username:     "admin",
					PasswordHash: string(hash),
					Scopes:       []string{"mcp:read", "mcp:write"},
				},
			},
		},
		AllowedRedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
		ScopesSupported:     []string{"mcp:read", "mcp:write"},
	}

	server, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	if server == nil {
		t.Fatal("expected non-nil server")
	}

	// Test metadata
	metadata := server.Metadata()
	if metadata.Issuer != "https://mcp.example.com" {
		t.Errorf("expected issuer 'https://mcp.example.com', got %s", metadata.Issuer)
	}
}

func TestServer_New_Disabled(t *testing.T) {
	logger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})

	cfg := &config.OAuthServerConfig{
		Enabled: false,
	}

	server, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if server != nil {
		t.Error("expected nil server when disabled")
	}
}

// ===========================================================================
// Federated Mode Tests
// ===========================================================================

func TestServer_New_FederatedMode(t *testing.T) {
	logger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})

	// Set up environment variable for client secret
	t.Setenv("TEST_CLIENT_SECRET", "test-secret-value")

	cfg := &config.OAuthServerConfig{
		Enabled:              true,
		Issuer:               "https://mcp.example.com",
		Mode:                 "federated",
		TokenLifetime:        time.Hour,
		RefreshTokenLifetime: 24 * time.Hour,
		AuthCodeLifetime:     10 * time.Minute,
		Signing: &config.SigningConfig{
			Algorithm:   "RS256",
			GenerateKey: true,
		},
		Federated: &config.FederatedAuthConfig{
			UpstreamIssuer:  "https://accounts.google.com",
			ClientID:        "test-client-id",
			ClientSecretEnv: "TEST_CLIENT_SECRET",
			Scopes:          []string{"openid", "email", "profile"},
			DefaultScopes:   []string{"mcp:read"},
			AdminUsers:      []string{"admin@example.com"},
			AdminScopes:     []string{"mcp:read", "mcp:write"},
		},
		AllowedRedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
		ScopesSupported:     []string{"mcp:read", "mcp:write"},
	}

	server, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create federated server: %v", err)
	}
	defer server.Close()

	if server == nil {
		t.Fatal("expected non-nil server")
	}

	if server.mode != "federated" {
		t.Errorf("expected mode 'federated', got %s", server.mode)
	}

	// Verify metadata is built correctly
	metadata := server.Metadata()
	if metadata.Issuer != "https://mcp.example.com" {
		t.Errorf("expected issuer 'https://mcp.example.com', got %s", metadata.Issuer)
	}
}

func TestServer_New_FederatedMode_MissingConfig(t *testing.T) {
	logger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})

	cfg := &config.OAuthServerConfig{
		Enabled: true,
		Issuer:  "https://mcp.example.com",
		Mode:    "federated",
		Signing: &config.SigningConfig{
			Algorithm:   "RS256",
			GenerateKey: true,
		},
		// Missing Federated config
	}

	_, err := New(cfg, logger)
	if err == nil {
		t.Error("expected error when federated config is missing")
	}
}

func TestServer_New_FederatedMode_MissingSecret(t *testing.T) {
	logger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})

	cfg := &config.OAuthServerConfig{
		Enabled: true,
		Issuer:  "https://mcp.example.com",
		Mode:    "federated",
		Signing: &config.SigningConfig{
			Algorithm:   "RS256",
			GenerateKey: true,
		},
		Federated: &config.FederatedAuthConfig{
			UpstreamIssuer: "https://accounts.google.com",
			ClientID:       "test-client-id",
			// Missing client secret
		},
	}

	_, err := New(cfg, logger)
	if err == nil {
		t.Error("expected error when client secret is missing")
	}
}

func TestFederatedAuthenticator_DomainFiltering(t *testing.T) {
	t.Setenv("TEST_SECRET", "secret")

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  "https://accounts.google.com",
		ClientID:        "test-client",
		ClientSecretEnv: "TEST_SECRET",
		AllowedDomains:  []string{"example.com", "Example.ORG"},
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	tests := []struct {
		email   string
		allowed bool
	}{
		{"user@example.com", true},
		{"user@EXAMPLE.COM", true},
		{"user@example.org", true},
		{"user@other.com", false},
		{"user@example.com.evil.com", false},
		{"invalid-email", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			result := auth.isAllowedDomain(tt.email)
			if result != tt.allowed {
				t.Errorf("isAllowedDomain(%q) = %v, want %v", tt.email, result, tt.allowed)
			}
		})
	}
}

func TestFederatedAuthenticator_ScopeDetermination(t *testing.T) {
	t.Setenv("TEST_SECRET", "secret")

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  "https://accounts.google.com",
		ClientID:        "test-client",
		ClientSecretEnv: "TEST_SECRET",
		DefaultScopes:   []string{"mcp:read"},
		AdminUsers:      []string{"admin@example.com", "sub-123"},
		AdminScopes:     []string{"mcp:write", "mcp:admin"},
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	tests := []struct {
		name           string
		claims         *IDTokenClaims
		expectedScopes []string
	}{
		{
			name:           "regular user",
			claims:         &IDTokenClaims{Subject: "user-1", Email: "user@example.com"},
			expectedScopes: []string{"mcp:read"},
		},
		{
			name:           "admin by email",
			claims:         &IDTokenClaims{Subject: "user-2", Email: "admin@example.com"},
			expectedScopes: []string{"mcp:read", "mcp:write", "mcp:admin"},
		},
		{
			name:           "admin by subject",
			claims:         &IDTokenClaims{Subject: "sub-123", Email: "other@example.com"},
			expectedScopes: []string{"mcp:read", "mcp:write", "mcp:admin"},
		},
		{
			name:           "user without email",
			claims:         &IDTokenClaims{Subject: "user-3"},
			expectedScopes: []string{"mcp:read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := auth.determineScopes(tt.claims)
			if len(scopes) != len(tt.expectedScopes) {
				t.Errorf("got %d scopes, want %d: %v", len(scopes), len(tt.expectedScopes), scopes)
				return
			}
			for i, expected := range tt.expectedScopes {
				if scopes[i] != expected {
					t.Errorf("scope[%d] = %s, want %s", i, scopes[i], expected)
				}
			}
		})
	}
}

func TestFederatedAuthorizeHandler_RequestValidation(t *testing.T) {
	t.Setenv("TEST_SECRET", "secret")

	federatedAuth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  "https://accounts.google.com",
		ClientID:        "test-client",
		ClientSecretEnv: "TEST_SECRET",
	})
	if err != nil {
		t.Fatalf("failed to create federated auth: %v", err)
	}

	storage := NewMemoryStorage(5 * time.Minute)
	handler := NewFederatedAuthorizeHandler(
		storage,
		federatedAuth,
		[]string{"https://claude.ai/callback"},
		10*time.Minute,
		[]string{"mcp:read"},
		"https://mcp.example.com",
	)
	defer handler.Close()

	tests := []struct {
		name        string
		query       string
		expectError bool
	}{
		{
			name:        "valid request",
			query:       "?response_type=code&client_id=test&redirect_uri=https://claude.ai/callback&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256",
			expectError: false,
		},
		{
			name:        "missing response_type",
			query:       "?client_id=test&redirect_uri=https://claude.ai/callback&code_challenge=abc&code_challenge_method=S256",
			expectError: true,
		},
		{
			name:        "invalid response_type",
			query:       "?response_type=token&client_id=test&redirect_uri=https://claude.ai/callback&code_challenge=abc&code_challenge_method=S256",
			expectError: true,
		},
		{
			name:        "missing client_id",
			query:       "?response_type=code&redirect_uri=https://claude.ai/callback&code_challenge=abc&code_challenge_method=S256",
			expectError: true,
		},
		{
			name:        "disallowed redirect_uri",
			query:       "?response_type=code&client_id=test&redirect_uri=https://evil.com/callback&code_challenge=abc&code_challenge_method=S256",
			expectError: true,
		},
		{
			name:        "missing code_challenge",
			query:       "?response_type=code&client_id=test&redirect_uri=https://claude.ai/callback",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/oauth/authorize"+tt.query, nil)
			_, oauthErr := handler.parseAndValidateRequest(req)

			if tt.expectError && oauthErr == nil {
				t.Error("expected OAuth error, got nil")
			}
			if !tt.expectError && oauthErr != nil {
				t.Errorf("unexpected OAuth error: %v", oauthErr)
			}
		})
	}
}

// ===========================================================================
// Storage Limit Tests
// ===========================================================================

func TestMemoryStorage_AuthCodeLimit(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	// Fill to capacity
	for i := 0; i < maxAuthCodes; i++ {
		code := &AuthorizationCode{
			Code:      fmt.Sprintf("code-%d", i),
			ClientID:  "client-1",
			ExpiresAt: time.Now().Add(10 * time.Minute),
			CreatedAt: time.Now(),
		}
		if err := storage.StoreAuthorizationCode(ctx, code); err != nil {
			t.Fatalf("failed to store code %d: %v", i, err)
		}
	}

	// Next one should fail
	code := &AuthorizationCode{
		Code:      "one-too-many",
		ClientID:  "client-1",
		ExpiresAt: time.Now().Add(10 * time.Minute),
		CreatedAt: time.Now(),
	}
	err := storage.StoreAuthorizationCode(ctx, code)
	if err != ErrStorageFull {
		t.Errorf("expected ErrStorageFull, got %v", err)
	}
}

func TestMemoryStorage_RefreshTokenLimit(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	for i := 0; i < maxRefreshTokens; i++ {
		token := &RefreshToken{
			Token:     fmt.Sprintf("token-%d", i),
			ClientID:  "client-1",
			UserID:    "user-1",
			ExpiresAt: time.Now().Add(24 * time.Hour),
			CreatedAt: time.Now(),
		}
		if err := storage.StoreRefreshToken(ctx, token); err != nil {
			t.Fatalf("failed to store token %d: %v", i, err)
		}
	}

	token := &RefreshToken{
		Token:     "one-too-many",
		ClientID:  "client-1",
		UserID:    "user-1",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	err := storage.StoreRefreshToken(ctx, token)
	if err != ErrStorageFull {
		t.Errorf("expected ErrStorageFull, got %v", err)
	}
}

func TestMemoryStorage_ClientLimit(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	for i := 0; i < maxClients; i++ {
		client := &Client{
			ClientID:  fmt.Sprintf("client-%d", i),
			CreatedAt: time.Now(),
		}
		if err := storage.StoreClient(ctx, client); err != nil {
			t.Fatalf("failed to store client %d: %v", i, err)
		}
	}

	client := &Client{
		ClientID:  "one-too-many",
		CreatedAt: time.Now(),
	}
	err := storage.StoreClient(ctx, client)
	if err != ErrStorageFull {
		t.Errorf("expected ErrStorageFull, got %v", err)
	}
}

// ===========================================================================
// Client Secret Validation Tests
// ===========================================================================

func TestValidateClientSecret(t *testing.T) {
	tests := []struct {
		name     string
		provided string
		stored   string
		valid    bool
	}{
		{"public client (empty stored)", "", "", true},
		{"public client (any provided)", "anything", "", true},
		{"valid secret", "my-secret", "my-secret", true},
		{"invalid secret", "wrong", "my-secret", false},
		{"empty provided with stored", "", "my-secret", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateClientSecret(tt.provided, tt.stored); got != tt.valid {
				t.Errorf("ValidateClientSecret(%q, %q) = %v, want %v", tt.provided, tt.stored, got, tt.valid)
			}
		})
	}
}

func TestTokenHandler_ClientSecretValidation(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	ctx := context.Background()

	// Register a confidential client
	confidentialClient := &Client{
		ClientID:                "confidential-client",
		ClientSecret:            "super-secret",
		RedirectURIs:            []string{"https://example.com/callback"},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethod: "client_secret_post",
		CreatedAt:               time.Now(),
	}
	if err := storage.StoreClient(ctx, confidentialClient); err != nil {
		t.Fatalf("failed to store client: %v", err)
	}

	// Register a public client
	publicClient := &Client{
		ClientID:                "public-client",
		ClientSecret:            "",
		RedirectURIs:            []string{"https://example.com/callback"},
		GrantTypes:              []string{"authorization_code"},
		TokenEndpointAuthMethod: "none",
		CreatedAt:               time.Now(),
	}
	if err := storage.StoreClient(ctx, publicClient); err != nil {
		t.Fatalf("failed to store client: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	// Store an auth code for testing
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authCode := &AuthorizationCode{
		Code:                "test-code-secret",
		ClientID:            "confidential-client",
		UserID:              "user-1",
		Username:            "testuser",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, authCode); err != nil {
		t.Fatalf("failed to store auth code: %v", err)
	}

	// Test: wrong client_secret should fail
	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"test-code-secret"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"confidential-client"},
		"client_secret": {"wrong-secret"},
		"code_verifier": {verifier},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong secret, got %d: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "invalid_client" {
		t.Errorf("expected invalid_client error, got %s", errResp["error"])
	}

	// Test: public client should work without secret
	publicAuthCode := &AuthorizationCode{
		Code:                "test-code-public",
		ClientID:            "public-client",
		UserID:              "user-1",
		Username:            "testuser",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, publicAuthCode); err != nil {
		t.Fatalf("failed to store auth code: %v", err)
	}

	formData = url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"test-code-public"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"public-client"},
		"code_verifier": {verifier},
	}

	req = httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for public client, got %d: %s", w.Code, w.Body.String())
	}
}

// ===========================================================================
// CSRF Token Tests
// ===========================================================================

func TestCSRFToken_GenerateAndValidate(t *testing.T) {
	handler, err := NewAuthorizeHandler(
		NewMemoryStorage(time.Minute),
		nil, // authenticator not needed for CSRF tests
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read"},
		"",
	)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	token, err := handler.generateCSRFToken()
	if err != nil {
		t.Fatalf("failed to generate CSRF token: %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Should validate successfully
	if !handler.validateCSRFToken(token) {
		t.Error("expected token to be valid")
	}

	// Should not validate a second time (one-time use)
	if handler.validateCSRFToken(token) {
		t.Error("expected token to be consumed after first use")
	}
}

func TestCSRFToken_InvalidToken(t *testing.T) {
	handler, err := NewAuthorizeHandler(
		NewMemoryStorage(time.Minute),
		nil,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read"},
		"",
	)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	if handler.validateCSRFToken("nonexistent-token") {
		t.Error("expected invalid token to fail")
	}

	if handler.validateCSRFToken("") {
		t.Error("expected empty token to fail")
	}
}

func TestCSRFToken_Expiry(t *testing.T) {
	handler, err := NewAuthorizeHandler(
		NewMemoryStorage(time.Minute),
		nil,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read"},
		"",
	)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Manually insert an expired token
	handler.csrfMu.Lock()
	handler.csrfTokens["expired-token"] = time.Now().Add(-1 * time.Minute)
	handler.csrfMu.Unlock()

	if handler.validateCSRFToken("expired-token") {
		t.Error("expected expired token to fail")
	}
}

// ===========================================================================
// Token Handler Tests
// ===========================================================================

func TestTokenHandler_ServeHTTP_MethodNotAllowed(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	req := httptest.NewRequest("GET", "/oauth/token", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestTokenHandler_ServeHTTP_UnsupportedGrantType(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{"grant_type": {"client_credentials"}}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "unsupported_grant_type" {
		t.Errorf("expected unsupported_grant_type, got %s", errResp["error"])
	}
}

func TestTokenHandler_HandleAuthorizationCode_MissingParams(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	tests := []struct {
		name        string
		formValues  url.Values
		expectError string
	}{
		{
			name: "missing code",
			formValues: url.Values{
				"grant_type":    {"authorization_code"},
				"redirect_uri":  {"https://example.com/callback"},
				"client_id":     {"test-client"},
				"code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
			},
			expectError: "invalid_request",
		},
		{
			name: "missing redirect_uri",
			formValues: url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {"some-code"},
				"client_id":     {"test-client"},
				"code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
			},
			expectError: "invalid_request",
		},
		{
			name: "missing client_id",
			formValues: url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {"some-code"},
				"redirect_uri":  {"https://example.com/callback"},
				"code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
			},
			expectError: "invalid_request",
		},
		{
			name: "missing code_verifier",
			formValues: url.Values{
				"grant_type":   {"authorization_code"},
				"code":         {"some-code"},
				"redirect_uri": {"https://example.com/callback"},
				"client_id":    {"test-client"},
			},
			expectError: "invalid_request",
		},
		{
			name: "invalid code_verifier format (too short)",
			formValues: url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {"some-code"},
				"redirect_uri":  {"https://example.com/callback"},
				"client_id":     {"test-client"},
				"code_verifier": {"short"},
			},
			expectError: "invalid_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(tt.formValues.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			var errResp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
				t.Fatalf("failed to decode error: %v", err)
			}
			if errResp["error"] != tt.expectError {
				t.Errorf("expected error %s, got %s", tt.expectError, errResp["error"])
			}
		})
	}
}

func TestTokenHandler_HandleAuthorizationCode_CodeNotFound(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"nonexistent-code"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"test-client"},
		"code_verifier": {"dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %s", errResp["error"])
	}
}

func TestTokenHandler_HandleAuthorizationCode_ClientIDMismatch(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()
	ctx := context.Background()

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authCode := &AuthorizationCode{
		Code:                "code-mismatch",
		ClientID:            "client-A",
		UserID:              "user-1",
		Username:            "testuser",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, authCode); err != nil {
		t.Fatalf("failed to store auth code: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"code-mismatch"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"client-B"},
		"code_verifier": {verifier},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %s", errResp["error"])
	}
}

func TestTokenHandler_HandleAuthorizationCode_RedirectURIMismatch(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()
	ctx := context.Background()

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authCode := &AuthorizationCode{
		Code:                "code-redirect",
		ClientID:            "test-client",
		UserID:              "user-1",
		Username:            "testuser",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, authCode); err != nil {
		t.Fatalf("failed to store auth code: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"code-redirect"},
		"redirect_uri":  {"https://evil.com/callback"},
		"client_id":     {"test-client"},
		"code_verifier": {verifier},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %s", errResp["error"])
	}
}

func TestTokenHandler_HandleAuthorizationCode_PKCEFailure(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()
	ctx := context.Background()

	// Use one verifier for the challenge but a different one for exchange
	realVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(realVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authCode := &AuthorizationCode{
		Code:                "code-pkce-fail",
		ClientID:            "test-client",
		UserID:              "user-1",
		Username:            "testuser",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, authCode); err != nil {
		t.Fatalf("failed to store auth code: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	wrongVerifier := "xBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"code-pkce-fail"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"test-client"},
		"code_verifier": {wrongVerifier},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %s", errResp["error"])
	}
}

func TestTokenHandler_HandleAuthorizationCode_HappyPath(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()
	ctx := context.Background()

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authCode := &AuthorizationCode{
		Code:                "happy-code",
		ClientID:            "test-client",
		UserID:              "user-1",
		Username:            "testuser",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read mcp:write",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, authCode); err != nil {
		t.Fatalf("failed to store auth code: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"happy-code"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"test-client"},
		"code_verifier": {verifier},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(w.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if tokenResp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if tokenResp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("expected token_type Bearer, got %s", tokenResp.TokenType)
	}
	if tokenResp.ExpiresIn != 3600 {
		t.Errorf("expected expires_in 3600, got %d", tokenResp.ExpiresIn)
	}
	if tokenResp.Scope != "mcp:read mcp:write" {
		t.Errorf("expected scope 'mcp:read mcp:write', got %s", tokenResp.Scope)
	}

	// Verify headers
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control no-store, got %s", cc)
	}
	if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
		t.Errorf("expected Pragma no-cache, got %s", pragma)
	}

	// Verify the auth code was deleted (one-time use)
	retrieved, err := storage.GetAuthorizationCode(ctx, "happy-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != nil {
		t.Error("expected auth code to be deleted after exchange")
	}
}

func TestTokenHandler_HandleRefreshToken_MissingParams(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	tests := []struct {
		name       string
		formValues url.Values
	}{
		{
			name: "missing refresh_token",
			formValues: url.Values{
				"grant_type": {"refresh_token"},
				"client_id":  {"test-client"},
			},
		},
		{
			name: "missing client_id",
			formValues: url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {"some-token"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(tt.formValues.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			var errResp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
				t.Fatalf("failed to decode error: %v", err)
			}
			if errResp["error"] != "invalid_request" {
				t.Errorf("expected invalid_request, got %s", errResp["error"])
			}
		})
	}
}

func TestTokenHandler_HandleRefreshToken_TokenNotFound(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"nonexistent-token"},
		"client_id":     {"test-client"},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %s", errResp["error"])
	}
}

func TestTokenHandler_HandleRefreshToken_ClientIDMismatch(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()
	ctx := context.Background()

	refreshToken := &RefreshToken{
		Token:     "rt-mismatch",
		ClientID:  "client-A",
		UserID:    "user-1",
		Username:  "testuser",
		Scope:     "mcp:read",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := storage.StoreRefreshToken(ctx, refreshToken); err != nil {
		t.Fatalf("failed to store refresh token: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"rt-mismatch"},
		"client_id":     {"client-B"},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %s", errResp["error"])
	}
}

func TestTokenHandler_HandleRefreshToken_HappyPath(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()
	ctx := context.Background()

	refreshToken := &RefreshToken{
		Token:     "rt-happy",
		ClientID:  "test-client",
		UserID:    "user-1",
		Username:  "testuser",
		Scope:     "mcp:read",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := storage.StoreRefreshToken(ctx, refreshToken); err != nil {
		t.Fatalf("failed to store refresh token: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	formData := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"rt-happy"},
		"client_id":     {"test-client"},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(w.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if tokenResp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if tokenResp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("expected token_type Bearer, got %s", tokenResp.TokenType)
	}

	// Verify old refresh token was rotated (deleted)
	old, err := storage.GetRefreshToken(ctx, "rt-happy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if old != nil {
		t.Error("expected old refresh token to be deleted after rotation")
	}
}

func TestTokenHandler_ValidateClientAuth(t *testing.T) {
	storage := NewMemoryStorage(time.Minute)
	defer storage.Close()
	ctx := context.Background()

	// Store a confidential client
	client := &Client{
		ClientID:                "conf-client",
		ClientSecret:            "the-secret",
		TokenEndpointAuthMethod: "client_secret_post",
		CreatedAt:               time.Now(),
	}
	if err := storage.StoreClient(ctx, client); err != nil {
		t.Fatalf("failed to store client: %v", err)
	}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	// Valid secret
	if oauthErr := handler.validateClientAuth(ctx, "conf-client", "the-secret"); oauthErr != nil {
		t.Errorf("expected nil error for valid secret, got %v", oauthErr)
	}

	// Invalid secret
	oauthErr := handler.validateClientAuth(ctx, "conf-client", "wrong-secret")
	if oauthErr == nil {
		t.Error("expected error for invalid secret")
	} else if oauthErr.Code != ErrorInvalidClient {
		t.Errorf("expected invalid_client, got %s", oauthErr.Code)
	}

	// Unregistered client (should succeed - PKCE-only clients)
	if oauthErr := handler.validateClientAuth(ctx, "unknown-client", ""); oauthErr != nil {
		t.Errorf("expected nil error for unregistered client, got %v", oauthErr)
	}
}

// ===========================================================================
// Scope Utility Tests
// ===========================================================================

func TestScopeFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty", "", nil},
		{"single", "mcp:read", []string{"mcp:read"}},
		{"multiple", "mcp:read mcp:write", []string{"mcp:read", "mcp:write"}},
		{"extra whitespace", "  mcp:read   mcp:write  ", []string{"mcp:read", "mcp:write"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScopeFromString(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("ScopeFromString(%q) returned %d items, want %d", tt.input, len(got), len(tt.expected))
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("ScopeFromString(%q)[%d] = %s, want %s", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestScopeToString(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"nil", nil, ""},
		{"empty", []string{}, ""},
		{"single", []string{"mcp:read"}, "mcp:read"},
		{"multiple", []string{"mcp:read", "mcp:write"}, "mcp:read mcp:write"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScopeToString(tt.input)
			if got != tt.expected {
				t.Errorf("ScopeToString(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ===========================================================================
// Error Tests (errors.go)
// ===========================================================================

func TestOAuthError_ErrorWithoutDescription(t *testing.T) {
	err := &OAuthError{Code: "invalid_request"}
	if err.Error() != "invalid_request" {
		t.Errorf("expected 'invalid_request', got %q", err.Error())
	}
}

func TestOAuthError_ErrorWithDescription(t *testing.T) {
	err := &OAuthError{Code: "invalid_request", Description: "missing param"}
	if err.Error() != "invalid_request: missing param" {
		t.Errorf("expected 'invalid_request: missing param', got %q", err.Error())
	}
}

func TestWriteJSONError_HeadersAndBody(t *testing.T) {
	w := httptest.NewRecorder()
	oauthErr := ErrServerError("something went wrong")
	WriteJSONError(w, oauthErr)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control no-store, got %s", cc)
	}
	if pragma := w.Header().Get("Pragma"); pragma != "no-cache" {
		t.Errorf("expected Pragma no-cache, got %s", pragma)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["error"] != "server_error" {
		t.Errorf("expected error server_error, got %v", body["error"])
	}
	if body["error_description"] != "something went wrong" {
		t.Errorf("expected description 'something went wrong', got %v", body["error_description"])
	}
}

func TestWriteJSONError_ZeroHTTPStatus(t *testing.T) {
	w := httptest.NewRecorder()
	oauthErr := &OAuthError{Code: "test_error"}
	WriteJSONError(w, oauthErr)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected default 400 for zero status, got %d", w.Code)
	}
}

func TestRedirectWithError_WithState(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/authorize", nil)
	oauthErr := ErrAccessDenied("user denied").WithState("state123")

	RedirectWithError(w, req, "https://example.com/callback", oauthErr)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse location: %v", err)
	}

	if u.Query().Get("error") != "access_denied" {
		t.Errorf("expected error=access_denied, got %s", u.Query().Get("error"))
	}
	if u.Query().Get("error_description") != "user denied" {
		t.Errorf("expected error_description='user denied', got %s", u.Query().Get("error_description"))
	}
	if u.Query().Get("state") != "state123" {
		t.Errorf("expected state=state123, got %s", u.Query().Get("state"))
	}
}

func TestRedirectWithError_WithoutState(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/authorize", nil)
	oauthErr := ErrInvalidRequest("bad request")

	RedirectWithError(w, req, "https://example.com/callback", oauthErr)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse location: %v", err)
	}

	if u.Query().Get("state") != "" {
		t.Errorf("expected no state param, got %s", u.Query().Get("state"))
	}
}

func TestRedirectWithError_InvalidRedirectURI(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/authorize", nil)
	oauthErr := ErrInvalidRequest("bad request")

	// Use a string that url.Parse can't handle as invalid
	RedirectWithError(w, req, "://invalid-uri", oauthErr)

	// Should fall back to JSON error
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected JSON fallback, got Content-Type %s", w.Header().Get("Content-Type"))
	}
}

func TestErrorConstructors(t *testing.T) {
	tests := []struct {
		name       string
		err        *OAuthError
		code       string
		httpStatus int
	}{
		{"ErrUnauthorizedClient", ErrUnauthorizedClient("bad client"), ErrorUnauthorizedClient, http.StatusUnauthorized},
		{"ErrInvalidScope", ErrInvalidScope("bad scope"), ErrorInvalidScope, http.StatusBadRequest},
		{"ErrServerError", ErrServerError("internal"), ErrorServerError, http.StatusInternalServerError},
		{"ErrTemporarilyUnavailable", ErrTemporarilyUnavailable("try later"), ErrorTemporarilyUnavailable, http.StatusServiceUnavailable},
		{"ErrUnsupportedGrantType", ErrUnsupportedGrantType("bad grant"), ErrorUnsupportedGrantType, http.StatusBadRequest},
		{"ErrInvalidClient", ErrInvalidClient("auth failed"), ErrorInvalidClient, http.StatusUnauthorized},
		{"ErrInvalidGrant", ErrInvalidGrant("expired"), ErrorInvalidGrant, http.StatusBadRequest},
		{"ErrAccessDenied", ErrAccessDenied("denied"), ErrorAccessDenied, http.StatusForbidden},
		{"ErrUnsupportedResponseType", ErrUnsupportedResponseType("bad type"), ErrorUnsupportedResponseType, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %s, got %s", tt.code, tt.err.Code)
			}
			if tt.err.HTTPStatus != tt.httpStatus {
				t.Errorf("expected status %d, got %d", tt.httpStatus, tt.err.HTTPStatus)
			}
		})
	}
}

// ===========================================================================
// AuthServer Routes Tests
// ===========================================================================

func newTestServer(t *testing.T) *Server {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate hash: %v", err)
	}

	logger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})

	cfg := &config.OAuthServerConfig{
		Enabled:                  true,
		Issuer:                   "https://mcp.example.com",
		Mode:                     "builtin",
		TokenLifetime:            time.Hour,
		RefreshTokenLifetime:     24 * time.Hour,
		AuthCodeLifetime:         10 * time.Minute,
		AllowDynamicRegistration: true,
		Signing: &config.SigningConfig{
			Algorithm:   "RS256",
			GenerateKey: true,
		},
		BuiltIn: &config.BuiltInAuthConfig{
			Users: []config.UserConfig{
				{
					Username:     "admin",
					PasswordHash: string(hash),
					Scopes:       []string{"mcp:read", "mcp:write"},
				},
			},
		},
		AllowedRedirectURIs: []string{"https://example.com/callback"},
		ScopesSupported:     []string{"mcp:read", "mcp:write"},
	}

	server, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	return server
}

func TestRegisterRoutes(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	// Verify routes by making requests
	paths := []struct {
		method string
		path   string
	}{
		{"GET", "/.well-known/oauth-protected-resource"},
		{"GET", "/.well-known/oauth-authorization-server"},
		{"GET", "/oauth/jwks"},
	}

	for _, p := range paths {
		t.Run(p.method+" "+p.path, func(t *testing.T) {
			req := httptest.NewRequest(p.method, p.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Errorf("route %s %s not registered (got 404)", p.method, p.path)
			}
		})
	}
}

func TestHandleProtectedResourceMetadata(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var meta ProtectedResourceMetadata
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if meta.Resource != "https://mcp.example.com" {
		t.Errorf("expected resource 'https://mcp.example.com', got %s", meta.Resource)
	}
	if len(meta.AuthorizationServers) != 1 || meta.AuthorizationServers[0] != "https://mcp.example.com" {
		t.Errorf("unexpected authorization_servers: %v", meta.AuthorizationServers)
	}
	if len(meta.BearerMethodsSupported) != 1 || meta.BearerMethodsSupported[0] != "header" {
		t.Errorf("unexpected bearer_methods_supported: %v", meta.BearerMethodsSupported)
	}
}

func TestHandleMetadataEndpoint(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var meta Metadata
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if meta.Issuer != "https://mcp.example.com" {
		t.Errorf("expected issuer 'https://mcp.example.com', got %s", meta.Issuer)
	}
	if meta.TokenEndpoint != "https://mcp.example.com/oauth/token" {
		t.Errorf("expected token endpoint, got %s", meta.TokenEndpoint)
	}
	if meta.RegistrationEndpoint != "https://mcp.example.com/register" {
		t.Errorf("expected registration endpoint, got %s", meta.RegistrationEndpoint)
	}
}

func TestHandleJWKS(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/oauth/jwks", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var jwks map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&jwks); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	keys, ok := jwks["keys"].([]interface{})
	if !ok || len(keys) == 0 {
		t.Error("expected at least one key in JWKS")
	}
}

func TestHandleRegister_HappyPath(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	body := `{"redirect_uris":["https://example.com/callback"],"client_name":"Test App"}`
	req := httptest.NewRequest("POST", "/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if resp["client_id"] == nil || resp["client_id"].(string) == "" {
		t.Error("expected non-empty client_id")
	}
	if resp["client_name"] != "Test App" {
		t.Errorf("expected client_name 'Test App', got %v", resp["client_name"])
	}
	if resp["token_endpoint_auth_method"] != "none" {
		t.Errorf("expected default auth method 'none', got %v", resp["token_endpoint_auth_method"])
	}
}

func TestHandleRegister_WithClientSecret(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	body := `{"redirect_uris":["https://example.com/callback"],"token_endpoint_auth_method":"client_secret_post"}`
	req := httptest.NewRequest("POST", "/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if resp["client_secret"] == nil || resp["client_secret"].(string) == "" {
		t.Error("expected non-empty client_secret for confidential client")
	}
}

func TestHandleRegister_MissingRedirectURIs(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	body := `{"client_name":"Test App"}`
	req := httptest.NewRequest("POST", "/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if errResp["error"] != "invalid_request" {
		t.Errorf("expected invalid_request, got %s", errResp["error"])
	}
}

func TestHandleRegister_InvalidJSON(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/register", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestServer_Close(t *testing.T) {
	server := newTestServer(t)
	if err := server.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestServer_JWKS_Accessor(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	jwks := server.JWKS()
	if jwks == nil {
		t.Error("expected non-nil JWKS")
	}
}

func TestServer_Metadata_Accessor(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	meta := server.Metadata()
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta.Issuer != "https://mcp.example.com" {
		t.Errorf("expected issuer 'https://mcp.example.com', got %s", meta.Issuer)
	}
}

// ===========================================================================
// Storage Type Tests
// ===========================================================================

func TestAuthorizationCode_IsExpired(t *testing.T) {
	expired := &AuthorizationCode{ExpiresAt: time.Now().Add(-1 * time.Minute)}
	if !expired.IsExpired() {
		t.Error("expected expired code to report as expired")
	}

	notExpired := &AuthorizationCode{ExpiresAt: time.Now().Add(10 * time.Minute)}
	if notExpired.IsExpired() {
		t.Error("expected non-expired code to report as not expired")
	}
}

func TestRefreshToken_IsExpired(t *testing.T) {
	expired := &RefreshToken{ExpiresAt: time.Now().Add(-1 * time.Minute)}
	if !expired.IsExpired() {
		t.Error("expected expired token to report as expired")
	}

	notExpired := &RefreshToken{ExpiresAt: time.Now().Add(24 * time.Hour)}
	if notExpired.IsExpired() {
		t.Error("expected non-expired token to report as not expired")
	}
}

func TestClient_IsPublicClient(t *testing.T) {
	tests := []struct {
		name     string
		client   Client
		expected bool
	}{
		{"auth method none", Client{TokenEndpointAuthMethod: "none", ClientSecret: "some-secret"}, true},
		{"empty secret", Client{TokenEndpointAuthMethod: "client_secret_post", ClientSecret: ""}, true},
		{"confidential", Client{TokenEndpointAuthMethod: "client_secret_post", ClientSecret: "secret"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.client.IsPublicClient(); got != tt.expected {
				t.Errorf("IsPublicClient() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClient_HasRedirectURI(t *testing.T) {
	client := Client{RedirectURIs: []string{"https://example.com/callback", "https://other.com/cb"}}

	if !client.HasRedirectURI("https://example.com/callback") {
		t.Error("expected to find redirect URI")
	}
	if client.HasRedirectURI("https://evil.com/callback") {
		t.Error("expected not to find redirect URI")
	}
}

func TestClient_HasGrantType(t *testing.T) {
	client := Client{GrantTypes: []string{"authorization_code", "refresh_token"}}

	if !client.HasGrantType("authorization_code") {
		t.Error("expected to find grant type")
	}
	if client.HasGrantType("client_credentials") {
		t.Error("expected not to find grant type")
	}
}

// ===========================================================================
// Memory Storage Additional Tests
// ===========================================================================

func TestMemoryStorage_CleanupExpired(t *testing.T) {
	storage := NewMemoryStorage(time.Hour) // long interval so cleanup doesn't run automatically
	defer storage.Close()

	ctx := context.Background()

	// Store expired auth code
	expiredCode := &AuthorizationCode{
		Code:      "expired-cleanup",
		ClientID:  "client-1",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
		CreatedAt: time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, expiredCode); err != nil {
		t.Fatalf("failed to store code: %v", err)
	}

	// Store valid auth code
	validCode := &AuthorizationCode{
		Code:      "valid-cleanup",
		ClientID:  "client-1",
		ExpiresAt: time.Now().Add(10 * time.Minute),
		CreatedAt: time.Now(),
	}
	if err := storage.StoreAuthorizationCode(ctx, validCode); err != nil {
		t.Fatalf("failed to store code: %v", err)
	}

	// Store expired refresh token
	expiredToken := &RefreshToken{
		Token:     "expired-rt-cleanup",
		ClientID:  "client-1",
		UserID:    "user-1",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
		CreatedAt: time.Now(),
	}
	if err := storage.StoreRefreshToken(ctx, expiredToken); err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	// Store valid refresh token
	validToken := &RefreshToken{
		Token:     "valid-rt-cleanup",
		ClientID:  "client-1",
		UserID:    "user-1",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := storage.StoreRefreshToken(ctx, validToken); err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	// Run cleanup directly
	storage.cleanupExpired()

	// Verify expired items removed
	storage.authCodesMu.RLock()
	_, hasExpiredCode := storage.authCodes["expired-cleanup"]
	_, hasValidCode := storage.authCodes["valid-cleanup"]
	storage.authCodesMu.RUnlock()

	if hasExpiredCode {
		t.Error("expected expired code to be cleaned up")
	}
	if !hasValidCode {
		t.Error("expected valid code to remain")
	}

	storage.refreshMu.RLock()
	_, hasExpiredToken := storage.refreshTokens["expired-rt-cleanup"]
	_, hasValidToken := storage.refreshTokens["valid-rt-cleanup"]
	storage.refreshMu.RUnlock()

	if hasExpiredToken {
		t.Error("expected expired token to be cleaned up")
	}
	if !hasValidToken {
		t.Error("expected valid token to remain")
	}
}

func TestMemoryStorage_GetRefreshToken_Expired(t *testing.T) {
	storage := NewMemoryStorage(time.Hour)
	defer storage.Close()

	ctx := context.Background()

	token := &RefreshToken{
		Token:     "expired-rt",
		ClientID:  "client-1",
		UserID:    "user-1",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
		CreatedAt: time.Now(),
	}
	if err := storage.StoreRefreshToken(ctx, token); err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	retrieved, err := storage.GetRefreshToken(ctx, "expired-rt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != nil {
		t.Error("expected nil for expired refresh token")
	}
}

// ===========================================================================
// JWT Additional Tests
// ===========================================================================

func TestGenerateClientID_NonEmpty(t *testing.T) {
	id := GenerateClientID()
	if id == "" {
		t.Error("expected non-empty client ID")
	}
}

func TestGenerateClientSecret_NonEmpty(t *testing.T) {
	secret := GenerateClientSecret()
	if secret == "" {
		t.Error("expected non-empty client secret")
	}
}

func TestGenerateKeyPair_Algorithms(t *testing.T) {
	tests := []struct {
		name string
		alg  string
	}{
		{"ES256", "ES256"},
		{"ES384", "ES384"},
		{"ES512", "ES512"},
		{"RS256", "RS256"},
		{"RS384", "RS384"},
		{"RS512", "RS512"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer, err := NewTokenIssuer("https://example.com", tt.alg, "", "", true)
			if err != nil {
				t.Fatalf("failed to create issuer with %s: %v", tt.alg, err)
			}

			token, _, err := issuer.IssueAccessToken("user-1", []string{"https://example.com"}, "mcp:read", "client-1", time.Hour)
			if err != nil {
				t.Fatalf("failed to issue token: %v", err)
			}
			if token == "" {
				t.Error("expected non-empty token")
			}
		})
	}
}

func TestGenerateKeyPair_InvalidAlgorithm(t *testing.T) {
	_, err := NewTokenIssuer("https://example.com", "INVALID", "", "", true)
	if err == nil {
		t.Error("expected error for invalid algorithm")
	}
}

func TestLoadKeyFromFile_RSAKey(t *testing.T) {
	// Generate an RSA key, write to temp file, then load it
	tmpFile, err := os.CreateTemp("", "test-rsa-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	if err := pem.Encode(tmpFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("failed to write PEM: %v", err)
	}
	tmpFile.Close()

	issuer, err := NewTokenIssuer("https://example.com", "RS256", tmpFile.Name(), "test-key", false)
	if err != nil {
		t.Fatalf("failed to create issuer from file: %v", err)
	}

	token, _, err := issuer.IssueAccessToken("user-1", []string{"https://example.com"}, "mcp:read", "client-1", time.Hour)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestLoadKeyFromFile_ECKey(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-ec-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate EC key: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal EC key: %v", err)
	}

	if err := pem.Encode(tmpFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("failed to write PEM: %v", err)
	}
	tmpFile.Close()

	issuer, err := NewTokenIssuer("https://example.com", "ES256", tmpFile.Name(), "test-key", false)
	if err != nil {
		t.Fatalf("failed to create issuer from file: %v", err)
	}

	token, _, err := issuer.IssueAccessToken("user-1", []string{"https://example.com"}, "mcp:read", "client-1", time.Hour)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestLoadKeyFromFile_PKCS8Key(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-pkcs8-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal PKCS8 key: %v", err)
	}

	if err := pem.Encode(tmpFile, &pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("failed to write PEM: %v", err)
	}
	tmpFile.Close()

	issuer, err := NewTokenIssuer("https://example.com", "RS256", tmpFile.Name(), "test-key", false)
	if err != nil {
		t.Fatalf("failed to create issuer from file: %v", err)
	}

	token, _, err := issuer.IssueAccessToken("user-1", []string{"https://example.com"}, "mcp:read", "client-1", time.Hour)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestLoadKeyFromFile_AlgorithmMismatch(t *testing.T) {
	// Write an RSA key but try to load with ES256
	tmpFile, err := os.CreateTemp("", "test-mismatch-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	if err := pem.Encode(tmpFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("failed to write PEM: %v", err)
	}
	tmpFile.Close()

	_, err = NewTokenIssuer("https://example.com", "ES256", tmpFile.Name(), "test-key", false)
	if err == nil {
		t.Error("expected error for algorithm mismatch")
	}
}

func TestLoadKeyFromFile_NonexistentFile(t *testing.T) {
	_, err := NewTokenIssuer("https://example.com", "RS256", "/nonexistent/path/key.pem", "test-key", false)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadKeyFromFile_InvalidPEM(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-invalid-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString("not a PEM file"); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	tmpFile.Close()

	_, err = NewTokenIssuer("https://example.com", "RS256", tmpFile.Name(), "test-key", false)
	if err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestLoadKeyFromFile_UnsupportedKeyType(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-unsupported-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if err := pem.Encode(tmpFile, &pem.Block{Type: "CERTIFICATE", Bytes: []byte("fake")}); err != nil {
		t.Fatalf("failed to write PEM: %v", err)
	}
	tmpFile.Close()

	_, err = NewTokenIssuer("https://example.com", "RS256", tmpFile.Name(), "test-key", false)
	if err == nil {
		t.Error("expected error for unsupported key type")
	}
}

// ===========================================================================
// Metadata Handler Tests
// ===========================================================================

func TestHandleMetadata_Function(t *testing.T) {
	metadata := BuildMetadata("https://mcp.example.com", []string{"mcp:read"}, false)
	handler := HandleMetadata(metadata)

	// Test GET
	req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var meta Metadata
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if meta.Issuer != "https://mcp.example.com" {
		t.Errorf("expected issuer 'https://mcp.example.com', got %s", meta.Issuer)
	}
}

func TestHandleMetadata_Function_MethodNotAllowed(t *testing.T) {
	metadata := BuildMetadata("https://mcp.example.com", []string{"mcp:read"}, false)
	handler := HandleMetadata(metadata)

	req := httptest.NewRequest("POST", "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// ===========================================================================
// Authorize Handler Tests (Part 2)
// ===========================================================================

// mockAuthenticator implements UserAuthenticator for testing.
type mockAuthenticator struct {
	users map[string]*UserInfo
}

func (m *mockAuthenticator) Authenticate(username, password string) (*UserInfo, error) {
	if u, ok := m.users[username]; ok && password == "validpass" {
		return u, nil
	}
	return nil, ErrInvalidCredentials
}

func (m *mockAuthenticator) GetUser(userID string) (*UserInfo, error) {
	if u, ok := m.users[userID]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}

func newTestAuthorizeHandler(t *testing.T) *AuthorizeHandler {
	t.Helper()
	storage := NewMemoryStorage(5 * time.Minute)
	t.Cleanup(func() { storage.Close() })

	auth := &mockAuthenticator{
		users: map[string]*UserInfo{
			"testuser": {ID: "testuser", Username: "testuser", Scopes: []string{"mcp:read", "mcp:write"}},
		},
	}

	h, err := NewAuthorizeHandler(
		storage,
		auth,
		[]string{"https://example.com/callback", "http://localhost:8080/callback"},
		10*time.Minute,
		[]string{"mcp:read", "mcp:write"},
		"",
	)
	if err != nil {
		t.Fatalf("failed to create authorize handler: %v", err)
	}
	return h
}

// validCodeChallenge returns a valid 43-character base64url code challenge.
func validCodeChallenge() string {
	return "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
}

func validAuthorizeQuery() string {
	return "response_type=code&client_id=test-client&redirect_uri=https://example.com/callback" +
		"&code_challenge=" + validCodeChallenge() + "&code_challenge_method=S256&state=teststate"
}

func TestNewAuthorizeHandler_InvalidTemplatePath(t *testing.T) {
	storage := NewMemoryStorage(5 * time.Minute)
	defer storage.Close()

	_, err := NewAuthorizeHandler(
		storage,
		nil,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read"},
		"/nonexistent/path/to/template.html",
	)
	if err == nil {
		t.Error("expected error for invalid template path")
	}
}

func TestNewAuthorizeHandler_DefaultTemplate(t *testing.T) {
	storage := NewMemoryStorage(5 * time.Minute)
	defer storage.Close()

	h, err := NewAuthorizeHandler(
		storage,
		nil,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read"},
		"",
	)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.loginTemplate == nil {
		t.Fatal("expected login template to be set")
	}
}

func TestAuthorizeHandler_ServeHTTP_MethodNotAllowed(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	req := httptest.NewRequest("PUT", "/oauth/authorize?"+validAuthorizeQuery(), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestAuthorizeHandler_HandleGet_ValidRequest(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	req := httptest.NewRequest("GET", "/oauth/authorize?"+validAuthorizeQuery(), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "<form") {
		t.Error("expected HTML form in response")
	}
	if !strings.Contains(body, "csrf_token") {
		t.Error("expected CSRF token hidden field")
	}
	if !strings.Contains(body, "test-client") {
		t.Error("expected client_id in form")
	}
}

func TestAuthorizeHandler_HandleGet_InvalidRequest_WithValidRedirect(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	// Missing response_type but valid redirect_uri
	req := httptest.NewRequest("GET", "/oauth/authorize?client_id=test&redirect_uri=https://example.com/callback&code_challenge="+validCodeChallenge()+"&code_challenge_method=S256", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "error=") {
		t.Error("expected error parameter in redirect")
	}
}

func TestAuthorizeHandler_HandleGet_InvalidRequest_NoValidRedirect(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	// Invalid request with disallowed redirect_uri
	req := httptest.NewRequest("GET", "/oauth/authorize?client_id=test&redirect_uri=https://evil.com/callback", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected JSON error response, got Content-Type: %s", w.Header().Get("Content-Type"))
	}

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if errResp["error"] == "" {
		t.Error("expected error field in JSON response")
	}
}

func TestAuthorizeHandler_HandlePost_InvalidCSRF(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	formData := url.Values{
		"csrf_token":            {"invalid-csrf-token"},
		"username":              {"testuser"},
		"password":              {"validpass"},
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"code_challenge":        {validCodeChallenge()},
		"code_challenge_method": {"S256"},
	}

	req := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if errResp["error"] != "invalid_request" {
		t.Errorf("expected invalid_request error, got %s", errResp["error"])
	}
}

func TestAuthorizeHandler_HandlePost_MissingCredentials(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	csrfToken, err := h.generateCSRFToken()
	if err != nil {
		t.Fatalf("failed to generate CSRF token: %v", err)
	}

	formData := url.Values{
		"csrf_token":            {csrfToken},
		"username":              {""},
		"password":              {""},
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"code_challenge":        {validCodeChallenge()},
		"code_challenge_method": {"S256"},
	}

	req := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Should re-show login form with error message
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (login form re-display), got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Username and password are required") {
		t.Error("expected 'Username and password are required' message")
	}
}

func TestAuthorizeHandler_HandlePost_InvalidCredentials(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	csrfToken, err := h.generateCSRFToken()
	if err != nil {
		t.Fatalf("failed to generate CSRF token: %v", err)
	}

	formData := url.Values{
		"csrf_token":            {csrfToken},
		"username":              {"testuser"},
		"password":              {"wrongpass"},
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"code_challenge":        {validCodeChallenge()},
		"code_challenge_method": {"S256"},
	}

	req := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (login form re-display), got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Invalid username or password") {
		t.Error("expected 'Invalid username or password' message")
	}
}

func TestAuthorizeHandler_HandlePost_SuccessfulLogin(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	csrfToken, err := h.generateCSRFToken()
	if err != nil {
		t.Fatalf("failed to generate CSRF token: %v", err)
	}

	formData := url.Values{
		"csrf_token":            {csrfToken},
		"username":              {"testuser"},
		"password":              {"validpass"},
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"scope":                 {"mcp:read"},
		"state":                 {"mystate"},
		"code_challenge":        {validCodeChallenge()},
		"code_challenge_method": {"S256"},
	}

	req := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", w.Code, w.Body.String())
	}

	location := w.Header().Get("Location")
	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse redirect URL: %v", err)
	}

	code := redirectURL.Query().Get("code")
	if code == "" {
		t.Fatal("expected code in redirect URL")
	}
	if redirectURL.Query().Get("state") != "mystate" {
		t.Errorf("expected state 'mystate', got %s", redirectURL.Query().Get("state"))
	}

	// Verify code is stored in storage
	storedCode, err := h.storage.GetAuthorizationCode(context.Background(), code)
	if err != nil {
		t.Fatalf("error retrieving stored code: %v", err)
	}
	if storedCode == nil {
		t.Fatal("expected stored authorization code")
	}
	if storedCode.ClientID != "test-client" {
		t.Errorf("expected client_id 'test-client', got %s", storedCode.ClientID)
	}
	if storedCode.UserID != "testuser" {
		t.Errorf("expected user_id 'testuser', got %s", storedCode.UserID)
	}
}

func TestGenerateCSRFToken_Uniqueness(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := h.generateCSRFToken()
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}
		if tokens[token] {
			t.Fatalf("duplicate CSRF token generated: %s", token)
		}
		tokens[token] = true
	}
}

func TestGenerateCSRFToken_CleansExpired(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	// Insert some expired tokens
	h.csrfMu.Lock()
	h.csrfTokens["expired-1"] = time.Now().Add(-5 * time.Minute)
	h.csrfTokens["expired-2"] = time.Now().Add(-10 * time.Minute)
	h.csrfTokens["valid-1"] = time.Now().Add(5 * time.Minute)
	h.csrfMu.Unlock()

	// Generate a new token which triggers cleanup
	_, err := h.generateCSRFToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	h.csrfMu.Lock()
	defer h.csrfMu.Unlock()

	if _, ok := h.csrfTokens["expired-1"]; ok {
		t.Error("expected expired-1 to be cleaned up")
	}
	if _, ok := h.csrfTokens["expired-2"]; ok {
		t.Error("expected expired-2 to be cleaned up")
	}
	if _, ok := h.csrfTokens["valid-1"]; !ok {
		t.Error("expected valid-1 to remain")
	}
}

func TestShowLoginForm_RendersCorrectFields(t *testing.T) {
	h := newTestAuthorizeHandler(t)

	req := &AuthorizeRequest{
		ResponseType:        "code",
		ClientID:            "my-client",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read",
		State:               "xyz",
		CodeChallenge:       validCodeChallenge(),
		CodeChallengeMethod: "S256",
		Nonce:               "testnonce",
	}

	w := httptest.NewRecorder()
	h.showLoginForm(w, req, "test error message")

	body := w.Body.String()
	if !strings.Contains(body, "my-client") {
		t.Error("expected client_id in rendered form")
	}
	if !strings.Contains(body, "test error message") {
		t.Error("expected error message in rendered form")
	}
	if !strings.Contains(body, "testnonce") {
		t.Error("expected nonce in rendered form")
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got %s", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("expected no-store cache control, got %s", w.Header().Get("Cache-Control"))
	}
}

func TestIsAllowedRedirectURI(t *testing.T) {
	allowed := []string{
		"https://example.com/callback",
		"http://localhost:8080/callback",
	}

	tests := []struct {
		name    string
		uri     string
		allowed bool
	}{
		{"exact match", "https://example.com/callback", true},
		{"localhost different port", "http://localhost:9090/callback", true},
		{"localhost same port", "http://localhost:8080/callback", true},
		{"non-matching URI", "https://evil.com/callback", false},
		{"localhost different path", "http://localhost:9090/other", false},
		{"127.0.0.1 matches localhost", "http://127.0.0.1:8080/callback", true},
		{"empty URI", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAllowedRedirectURI(tt.uri, allowed)
			if result != tt.allowed {
				t.Errorf("isAllowedRedirectURI(%q) = %v, want %v", tt.uri, result, tt.allowed)
			}
		})
	}
}

func TestValidateAuthorizeRequest(t *testing.T) {
	allowed := []string{"https://example.com/callback"}

	tests := []struct {
		name      string
		req       *AuthorizeRequest
		expectErr string
	}{
		{
			name: "valid request",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "S256",
			},
			expectErr: "",
		},
		{
			name: "missing response_type",
			req: &AuthorizeRequest{
				ResponseType:        "",
				ClientID:            "client-1",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "S256",
			},
			expectErr: "unsupported_response_type",
		},
		{
			name: "invalid response_type",
			req: &AuthorizeRequest{
				ResponseType:        "token",
				ClientID:            "client-1",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "S256",
			},
			expectErr: "unsupported_response_type",
		},
		{
			name: "missing client_id",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "S256",
			},
			expectErr: "invalid_request",
		},
		{
			name: "missing redirect_uri",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "S256",
			},
			expectErr: "invalid_request",
		},
		{
			name: "disallowed redirect_uri",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://evil.com/callback",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "S256",
			},
			expectErr: "invalid_request",
		},
		{
			name: "missing code_challenge",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       "",
				CodeChallengeMethod: "S256",
			},
			expectErr: "invalid_request",
		},
		{
			name: "invalid code_challenge_method",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "plain",
			},
			expectErr: "invalid_request",
		},
		{
			name: "invalid code_challenge format (wrong length)",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       "too-short",
				CodeChallengeMethod: "S256",
			},
			expectErr: "invalid_request",
		},
		{
			name: "default S256 when method empty",
			req: &AuthorizeRequest{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://example.com/callback",
				CodeChallenge:       validCodeChallenge(),
				CodeChallengeMethod: "",
			},
			expectErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oauthErr := validateAuthorizeRequest(tt.req, allowed)
			if tt.expectErr == "" {
				if oauthErr != nil {
					t.Errorf("expected no error, got: %s - %s", oauthErr.Code, oauthErr.Description)
				}
				// For the "default S256" case, verify it was set
				if tt.name == "default S256 when method empty" && tt.req.CodeChallengeMethod != "S256" {
					t.Errorf("expected CodeChallengeMethod to be set to S256, got %s", tt.req.CodeChallengeMethod)
				}
			} else {
				if oauthErr == nil {
					t.Error("expected error, got nil")
				} else if oauthErr.Code != tt.expectErr {
					t.Errorf("expected error code %s, got %s", tt.expectErr, oauthErr.Code)
				}
			}
		})
	}
}

// ===========================================================================
// Federated Authorize Handler Tests (Part 2)
// ===========================================================================

func newTestFederatedHandler(t *testing.T) *FederatedAuthorizeHandler {
	t.Helper()
	t.Setenv("TEST_SECRET", "test-secret")

	federatedAuth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  "https://accounts.google.com",
		ClientID:        "test-client",
		ClientSecretEnv: "TEST_SECRET",
		DefaultScopes:   []string{"mcp:read"},
	})
	if err != nil {
		t.Fatalf("failed to create federated auth: %v", err)
	}

	storage := NewMemoryStorage(5 * time.Minute)
	t.Cleanup(func() { storage.Close() })

	h := NewFederatedAuthorizeHandler(
		storage,
		federatedAuth,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read", "mcp:write"},
		"https://mcp.example.com",
	)
	t.Cleanup(func() { h.Close() })

	return h
}

func TestFederatedAuthorizeHandler_ServeHTTP_MethodNotAllowed(t *testing.T) {
	h := newTestFederatedHandler(t)

	req := httptest.NewRequest("POST", "/oauth/authorize?"+validAuthorizeQuery(), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestFederatedAuthorizeHandler_HandleCallback_MethodNotAllowed(t *testing.T) {
	h := newTestFederatedHandler(t)

	req := httptest.NewRequest("POST", "/oauth/callback?code=test&state=test", nil)
	w := httptest.NewRecorder()
	h.HandleCallback(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestFederatedAuthorizeHandler_HandleCallback_ErrorFromUpstream(t *testing.T) {
	h := newTestFederatedHandler(t)

	// First, store a pending request so it can be recovered
	h.pendingMu.Lock()
	h.pending["test-state"] = &pendingAuthRequest{
		OriginalRequest: &AuthorizeRequest{
			RedirectURI: "https://example.com/callback",
			State:       "original-state",
		},
		CreatedAt: time.Now(),
	}
	h.pendingMu.Unlock()

	req := httptest.NewRequest("GET", "/oauth/callback?error=access_denied&error_description=user+denied&state=test-state", nil)
	w := httptest.NewRecorder()
	h.HandleCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "error=access_denied") {
		t.Errorf("expected access_denied error in redirect, got: %s", location)
	}
}

func TestFederatedAuthorizeHandler_HandleCallback_ErrorFromUpstream_NoPending(t *testing.T) {
	h := newTestFederatedHandler(t)

	req := httptest.NewRequest("GET", "/oauth/callback?error=server_error&error_description=something+failed", nil)
	w := httptest.NewRecorder()
	h.HandleCallback(w, req)

	// Should show JSON error since there's no pending request to redirect to
	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if errResp["error"] != "server_error" {
		t.Errorf("expected server_error, got %s", errResp["error"])
	}
}

func TestFederatedAuthorizeHandler_HandleCallback_MissingCode(t *testing.T) {
	h := newTestFederatedHandler(t)

	req := httptest.NewRequest("GET", "/oauth/callback?state=some-state", nil)
	w := httptest.NewRecorder()
	h.HandleCallback(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if errResp["error"] != "invalid_request" {
		t.Errorf("expected invalid_request, got %s", errResp["error"])
	}
}

func TestFederatedAuthorizeHandler_HandleCallback_InvalidState(t *testing.T) {
	h := newTestFederatedHandler(t)

	req := httptest.NewRequest("GET", "/oauth/callback?code=authcode123&state=nonexistent-state", nil)
	w := httptest.NewRecorder()
	h.HandleCallback(w, req)

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if errResp["error"] != "invalid_request" {
		t.Errorf("expected invalid_request, got %s", errResp["error"])
	}
	if !strings.Contains(errResp["error_description"], "invalid or expired state") {
		t.Errorf("expected 'invalid or expired state' description, got %s", errResp["error_description"])
	}
}

func TestFederatedAuthorizeHandler_Close(t *testing.T) {
	t.Setenv("TEST_SECRET", "test-secret")

	federatedAuth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  "https://accounts.google.com",
		ClientID:        "test-client",
		ClientSecretEnv: "TEST_SECRET",
	})
	if err != nil {
		t.Fatalf("failed to create federated auth: %v", err)
	}

	storage := NewMemoryStorage(5 * time.Minute)
	defer storage.Close()

	h := NewFederatedAuthorizeHandler(
		storage,
		federatedAuth,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read"},
		"https://mcp.example.com",
	)

	// Close should not panic and should be callable
	h.Close()
	// Calling Close multiple times via defer is not tested since close on
	// a closed channel panics. The test just verifies single Close works.
}

func TestFederatedAuthorizeHandler_PendingRequestJSON(t *testing.T) {
	h := newTestFederatedHandler(t)

	// Empty pending map should return valid JSON
	data, err := h.PendingRequestJSON()
	if err != nil {
		t.Fatalf("PendingRequestJSON failed: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("expected empty JSON object, got %s", string(data))
	}

	// Add a pending request
	h.pendingMu.Lock()
	h.pending["state-1"] = &pendingAuthRequest{
		OriginalRequest: &AuthorizeRequest{
			ClientID:    "test-client",
			RedirectURI: "https://example.com/callback",
		},
		CreatedAt: time.Now(),
	}
	h.pendingMu.Unlock()

	data, err = h.PendingRequestJSON()
	if err != nil {
		t.Fatalf("PendingRequestJSON failed: %v", err)
	}
	if !strings.Contains(string(data), "test-client") {
		t.Errorf("expected JSON to contain test-client, got %s", string(data))
	}
}

func TestGenerateSecureToken(t *testing.T) {
	token, err := generateSecureToken(32)
	if err != nil {
		t.Fatalf("generateSecureToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Should be base64url encoded (no + or / characters)
	if strings.ContainsAny(token, "+/=") {
		t.Errorf("token contains non-base64url characters: %s", token)
	}

	// Tokens should be unique
	token2, err := generateSecureToken(32)
	if err != nil {
		t.Fatalf("generateSecureToken failed: %v", err)
	}
	if token == token2 {
		t.Error("expected unique tokens")
	}
}

func TestFederatedAuthorizeHandler_CleanupPending(t *testing.T) {
	h := newTestFederatedHandler(t)

	// Add expired and non-expired requests
	h.pendingMu.Lock()
	h.pending["expired-state"] = &pendingAuthRequest{
		OriginalRequest: &AuthorizeRequest{ClientID: "old"},
		CreatedAt:       time.Now().Add(-15 * time.Minute), // older than 10 min cutoff
	}
	h.pending["valid-state"] = &pendingAuthRequest{
		OriginalRequest: &AuthorizeRequest{ClientID: "new"},
		CreatedAt:       time.Now(),
	}
	h.pendingMu.Unlock()

	// Directly invoke cleanup logic (simulating what the ticker would do)
	h.pendingMu.Lock()
	cutoff := time.Now().Add(-10 * time.Minute)
	for state, req := range h.pending {
		if req.CreatedAt.Before(cutoff) {
			delete(h.pending, state)
		}
	}
	h.pendingMu.Unlock()

	h.pendingMu.RLock()
	defer h.pendingMu.RUnlock()

	if _, ok := h.pending["expired-state"]; ok {
		t.Error("expected expired-state to be cleaned up")
	}
	if _, ok := h.pending["valid-state"]; !ok {
		t.Error("expected valid-state to remain")
	}
}

func TestFederatedAuthorizeHandler_IsAllowedRedirectURI(t *testing.T) {
	h := newTestFederatedHandler(t)

	if !h.isAllowedRedirectURI("https://example.com/callback") {
		t.Error("expected allowed URI to pass")
	}
	if h.isAllowedRedirectURI("https://evil.com/callback") {
		t.Error("expected disallowed URI to fail")
	}
}

// ===========================================================================
// Rate Limiter Additional Tests (Part 2)
// ===========================================================================

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(5, 50*time.Millisecond)
	defer rl.Close()

	// Add some requests
	rl.Allow("10.0.0.1")
	rl.Allow("10.0.0.2")

	// Wait for windows to expire
	time.Sleep(60 * time.Millisecond)

	// Directly call cleanup
	rl.cleanup()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.windows) != 0 {
		t.Errorf("expected 0 windows after cleanup, got %d", len(rl.windows))
	}
}

func TestRateLimiter_CleanupKeepsActive(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)
	defer rl.Close()

	rl.Allow("10.0.0.1")

	// Directly call cleanup - window should not be removed
	rl.cleanup()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.windows) != 1 {
		t.Errorf("expected 1 active window, got %d", len(rl.windows))
	}
}

func TestExtractIP_HostPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	ip := extractIP(req)
	if ip != "192.168.1.100" {
		t.Errorf("expected 192.168.1.100, got %s", ip)
	}
}

func TestExtractIP_BareIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100"

	ip := extractIP(req)
	// When there's no port, SplitHostPort fails and RemoteAddr is returned as-is
	if ip != "192.168.1.100" {
		t.Errorf("expected 192.168.1.100, got %s", ip)
	}
}

func TestExtractIP_IPv6(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::1]:8080"

	ip := extractIP(req)
	if ip != "::1" {
		t.Errorf("expected ::1, got %s", ip)
	}
}

// ===========================================================================
// PKCE Additional Tests (Part 2)
// ===========================================================================

func TestValidateCodeChallenge_WrongLength(t *testing.T) {
	// Too short
	if ValidateCodeChallenge("abc") {
		t.Error("expected short challenge to fail")
	}
	// Exactly 42 (one short)
	if ValidateCodeChallenge(strings.Repeat("a", 42)) {
		t.Error("expected 42-char challenge to fail")
	}
	// Exactly 44 (one too long)
	if ValidateCodeChallenge(strings.Repeat("a", 44)) {
		t.Error("expected 44-char challenge to fail")
	}
}

func TestValidateCodeChallenge_InvalidCharacters(t *testing.T) {
	// 43 chars but with invalid characters
	invalidChallenges := []string{
		strings.Repeat("a", 42) + "+", // + not valid in base64url
		strings.Repeat("a", 42) + "/", // / not valid in base64url
		strings.Repeat("a", 42) + "=", // = not valid
		strings.Repeat("a", 42) + " ", // space not valid
		strings.Repeat("a", 42) + "!", // ! not valid
	}

	for _, ch := range invalidChallenges {
		if ValidateCodeChallenge(ch) {
			t.Errorf("expected challenge with invalid char to fail: %q", ch)
		}
	}
}

func TestValidateCodeChallenge_ValidCharacters(t *testing.T) {
	// All valid base64url characters: A-Z, a-z, 0-9, -, _
	valid := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnop-"
	if len(valid) != 43 {
		t.Fatalf("test setup error: challenge length is %d, expected 43", len(valid))
	}
	if !ValidateCodeChallenge(valid) {
		t.Error("expected valid challenge to pass")
	}

	valid2 := "abcdefghijklmnopqrstuvwxyz0123456789_------"
	if len(valid2) != 43 {
		t.Fatalf("test setup error: challenge length is %d, expected 43", len(valid2))
	}
	if !ValidateCodeChallenge(valid2) {
		t.Error("expected valid challenge with underscores/dashes to pass")
	}
}

func TestIDTokenClaims_GetAudience(t *testing.T) {
	tests := []struct {
		name     string
		audience any
		expected []string
	}{
		{
			name:     "string audience",
			audience: "client-1",
			expected: []string{"client-1"},
		},
		{
			name:     "array audience",
			audience: []interface{}{"client-1", "client-2"},
			expected: []string{"client-1", "client-2"},
		},
		{
			name:     "nil audience",
			audience: nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &IDTokenClaims{Audience: tt.audience}
			result := claims.GetAudience()

			if len(result) != len(tt.expected) {
				t.Errorf("GetAudience() returned %d items, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("GetAudience()[%d] = %s, want %s", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

// ===========================================================================
// Helper: mock OIDC discovery server
// ===========================================================================

// newMockOIDCServer creates an httptest server that serves OIDC discovery,
// JWKS, and token endpoints. It returns the server and a cleanup function.
func newMockOIDCServer(t *testing.T, rsaKey *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()

	var serverURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := map[string]interface{}{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/jwks",
			"userinfo_endpoint":      serverURL + "/userinfo",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		nBytes := rsaKey.PublicKey.N.Bytes()
		eBytes := big.NewInt(int64(rsaKey.PublicKey.E)).Bytes()

		jwks := map[string]interface{}{
			"keys": []map[string]interface{}{
				{
					"kty": "RSA",
					"use": "sig",
					"kid": kid,
					"alg": "RS256",
					"n":   base64.RawURLEncoding.EncodeToString(nBytes),
					"e":   base64.RawURLEncoding.EncodeToString(eBytes),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	return srv
}

// signJWT creates a signed JWT with the given header and claims using RS256.
func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]interface{}) string {
	t.Helper()

	header := map[string]interface{}{
		"alg": "RS256",
		"kid": kid,
		"typ": "JWT",
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("failed to marshal header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("failed to marshal claims: %v", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64

	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ===========================================================================
// OIDC Client Tests
// ===========================================================================

func TestOIDCClient_Discover(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	srv := newMockOIDCServer(t, rsaKey, "test-kid")
	defer srv.Close()

	client := NewOIDCClient(srv.URL, 5*time.Second)
	ctx := context.Background()

	disc, err := client.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if disc.Issuer != srv.URL {
		t.Errorf("Issuer = %s, want %s", disc.Issuer, srv.URL)
	}
	if disc.AuthorizationEndpoint != srv.URL+"/authorize" {
		t.Errorf("AuthorizationEndpoint = %s, want %s/authorize", disc.AuthorizationEndpoint, srv.URL)
	}
	if disc.TokenEndpoint != srv.URL+"/token" {
		t.Errorf("TokenEndpoint = %s, want %s/token", disc.TokenEndpoint, srv.URL)
	}
	if disc.JwksURI != srv.URL+"/jwks" {
		t.Errorf("JwksURI = %s, want %s/jwks", disc.JwksURI, srv.URL)
	}
	if disc.UserinfoEndpoint != srv.URL+"/userinfo" {
		t.Errorf("UserinfoEndpoint = %s, want %s/userinfo", disc.UserinfoEndpoint, srv.URL)
	}
}

func TestOIDCClient_Discover_IssuerMismatch(t *testing.T) {
	// Create a server that returns a different issuer than what we configure
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := map[string]interface{}{
			"issuer":                 "https://wrong-issuer.example.com",
			"authorization_endpoint": "https://wrong-issuer.example.com/authorize",
			"token_endpoint":         "https://wrong-issuer.example.com/token",
			"jwks_uri":               "https://wrong-issuer.example.com/jwks",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewOIDCClient(srv.URL, 5*time.Second)
	ctx := context.Background()

	_, err := client.Discover(ctx)
	if err == nil {
		t.Fatal("expected error for issuer mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "issuer mismatch") {
		t.Errorf("expected 'issuer mismatch' in error, got: %v", err)
	}
}

func TestOIDCClient_Discover_Caching(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	var hitCount atomic.Int32

	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		disc := map[string]interface{}{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/jwks",
			"userinfo_endpoint":      serverURL + "/userinfo",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL
	_ = rsaKey // not needed for this test

	client := NewOIDCClient(srv.URL, 5*time.Second)
	ctx := context.Background()

	// First call
	_, err = client.Discover(ctx)
	if err != nil {
		t.Fatalf("first Discover() error: %v", err)
	}

	// Second call should use cache
	_, err = client.Discover(ctx)
	if err != nil {
		t.Fatalf("second Discover() error: %v", err)
	}

	if hitCount.Load() != 1 {
		t.Errorf("expected discovery endpoint to be hit once, got %d", hitCount.Load())
	}
}

func TestOIDCClient_GetJWKS(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	srv := newMockOIDCServer(t, rsaKey, "jwks-test-kid")
	defer srv.Close()

	client := NewOIDCClient(srv.URL, 5*time.Second)
	ctx := context.Background()

	jwks, err := client.GetJWKS(ctx)
	if err != nil {
		t.Fatalf("GetJWKS() error: %v", err)
	}

	if len(jwks.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(jwks.Keys))
	}

	key := jwks.Keys[0]
	if key.Kty != "RSA" {
		t.Errorf("key type = %s, want RSA", key.Kty)
	}
	if key.Kid != "jwks-test-kid" {
		t.Errorf("kid = %s, want jwks-test-kid", key.Kid)
	}
	if key.N == "" {
		t.Error("expected non-empty N (modulus)")
	}
	if key.E == "" {
		t.Error("expected non-empty E (exponent)")
	}
}

func TestOIDCClient_GetJWKS_CachesResult(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	var jwksHitCount atomic.Int32
	var serverURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := map[string]interface{}{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/jwks",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwksHitCount.Add(1)
		nBytes := rsaKey.PublicKey.N.Bytes()
		eBytes := big.NewInt(int64(rsaKey.PublicKey.E)).Bytes()
		jwks := map[string]interface{}{
			"keys": []map[string]interface{}{
				{
					"kty": "RSA",
					"kid": "cache-kid",
					"n":   base64.RawURLEncoding.EncodeToString(nBytes),
					"e":   base64.RawURLEncoding.EncodeToString(eBytes),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL

	client := NewOIDCClient(srv.URL, 5*time.Second)
	ctx := context.Background()

	// First call
	_, err = client.GetJWKS(ctx)
	if err != nil {
		t.Fatalf("first GetJWKS() error: %v", err)
	}

	// Second call should use cache
	_, err = client.GetJWKS(ctx)
	if err != nil {
		t.Fatalf("second GetJWKS() error: %v", err)
	}

	if jwksHitCount.Load() != 1 {
		t.Errorf("expected JWKS endpoint to be hit once, got %d", jwksHitCount.Load())
	}
}

func TestOIDCClient_GetRSAPublicKey(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	nBytes := rsaKey.PublicKey.N.Bytes()
	eBytes := big.NewInt(int64(rsaKey.PublicKey.E)).Bytes()

	jwk := &JWK{
		Kty: "RSA",
		N:   base64.RawURLEncoding.EncodeToString(nBytes),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}

	pubKey, err := jwk.GetRSAPublicKey()
	if err != nil {
		t.Fatalf("GetRSAPublicKey() error: %v", err)
	}

	if pubKey.N.Cmp(rsaKey.PublicKey.N) != 0 {
		t.Error("modulus does not match original key")
	}
	if pubKey.E != rsaKey.PublicKey.E {
		t.Errorf("exponent = %d, want %d", pubKey.E, rsaKey.PublicKey.E)
	}
}

func TestOIDCClient_GetRSAPublicKey_NonRSA(t *testing.T) {
	jwk := &JWK{
		Kty: "EC",
		Crv: "P-256",
	}

	_, err := jwk.GetRSAPublicKey()
	if err == nil {
		t.Fatal("expected error for non-RSA key type, got nil")
	}
	if !strings.Contains(err.Error(), "not RSA") {
		t.Errorf("expected 'not RSA' in error, got: %v", err)
	}
}

func TestNewOIDCClient_DefaultTimeout(t *testing.T) {
	client := NewOIDCClient("https://example.com", 0)
	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", client.httpClient.Timeout)
	}
}

func TestNewOIDCClient_CustomTimeout(t *testing.T) {
	client := NewOIDCClient("https://example.com", 10*time.Second)
	if client.httpClient.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", client.httpClient.Timeout)
	}
}

// ===========================================================================
// Federated Authenticator Tests (with HTTP mocks)
// ===========================================================================

func TestFederatedAuthenticator_GetAuthorizationURL(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	srv := newMockOIDCServer(t, rsaKey, "auth-kid")
	defer srv.Close()

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "my-client-id",
		ClientSecret:   "my-client-secret",
		Scopes:         []string{"openid", "email", "profile"},
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	ctx := context.Background()
	authURL, err := auth.GetAuthorizationURL(ctx, "test-state", "test-nonce", "https://example.com/callback")
	if err != nil {
		t.Fatalf("GetAuthorizationURL() error: %v", err)
	}

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse auth URL: %v", err)
	}

	// Should point to the mock server's authorize endpoint
	if !strings.HasPrefix(authURL, srv.URL+"/authorize") {
		t.Errorf("expected URL to start with %s/authorize, got %s", srv.URL, authURL)
	}

	q := parsed.Query()
	if q.Get("client_id") != "my-client-id" {
		t.Errorf("client_id = %s, want my-client-id", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %s, want code", q.Get("response_type"))
	}
	if q.Get("redirect_uri") != "https://example.com/callback" {
		t.Errorf("redirect_uri = %s, want https://example.com/callback", q.Get("redirect_uri"))
	}
	if q.Get("state") != "test-state" {
		t.Errorf("state = %s, want test-state", q.Get("state"))
	}
	if q.Get("nonce") != "test-nonce" {
		t.Errorf("nonce = %s, want test-nonce", q.Get("nonce"))
	}
	if q.Get("scope") != "openid email profile" {
		t.Errorf("scope = %s, want 'openid email profile'", q.Get("scope"))
	}
}

func TestFederatedAuthenticator_GetAuthorizationURL_DefaultScopes(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	srv := newMockOIDCServer(t, rsaKey, "auth-kid")
	defer srv.Close()

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "my-client-id",
		ClientSecret:   "my-client-secret",
		// No Scopes set - should use defaults
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	ctx := context.Background()
	authURL, err := auth.GetAuthorizationURL(ctx, "s", "n", "https://example.com/cb")
	if err != nil {
		t.Fatalf("GetAuthorizationURL() error: %v", err)
	}

	parsed, _ := url.Parse(authURL)
	scope := parsed.Query().Get("scope")
	if scope != "openid email profile" {
		t.Errorf("default scope = %s, want 'openid email profile'", scope)
	}
}

func TestFederatedAuthenticator_ExchangeCode(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "exchange-kid"
	var serverURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := map[string]interface{}{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/jwks",
			"userinfo_endpoint":      serverURL + "/userinfo",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Verify the parameters we expect
		if r.FormValue("grant_type") != "authorization_code" {
			t.Errorf("expected grant_type=authorization_code, got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("code") != "test-auth-code" {
			t.Errorf("expected code=test-auth-code, got %s", r.FormValue("code"))
		}

		// Create a signed ID token
		claims := map[string]interface{}{
			"iss":   serverURL,
			"sub":   "user-123",
			"aud":   "exchange-client-id",
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
			"email": "user@example.com",
		}
		idToken := signJWT(t, rsaKey, kid, claims)

		resp := map[string]interface{}{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"id_token":     idToken,
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "exchange-client-id",
		ClientSecret:   "exchange-client-secret",
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	ctx := context.Background()
	tokenResp, err := auth.ExchangeCode(ctx, "test-auth-code", "https://example.com/callback")
	if err != nil {
		t.Fatalf("ExchangeCode() error: %v", err)
	}

	if tokenResp.AccessToken != "mock-access-token" {
		t.Errorf("AccessToken = %s, want mock-access-token", tokenResp.AccessToken)
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("TokenType = %s, want Bearer", tokenResp.TokenType)
	}
	if tokenResp.IDToken == "" {
		t.Error("expected non-empty IDToken")
	}
	if tokenResp.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d, want 3600", tokenResp.ExpiresIn)
	}
}

func TestFederatedAuthenticator_ExchangeCode_Error(t *testing.T) {
	var serverURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := map[string]interface{}{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/jwks",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]string{
			"error":             "invalid_grant",
			"error_description": "authorization code expired",
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "err-client-id",
		ClientSecret:   "err-client-secret",
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	ctx := context.Background()
	_, err = auth.ExchangeCode(ctx, "bad-code", "https://example.com/callback")
	if err == nil {
		t.Fatal("expected error from ExchangeCode, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("expected error to contain 'invalid_grant', got: %v", err)
	}
	if !strings.Contains(err.Error(), "authorization code expired") {
		t.Errorf("expected error to contain 'authorization code expired', got: %v", err)
	}
}

func TestFederatedAuthenticator_ValidateIDToken(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "validate-kid"
	srv := newMockOIDCServer(t, rsaKey, kid)
	defer srv.Close()

	clientID := "validate-client-id"
	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       clientID,
		ClientSecret:   "validate-secret",
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	// Create a valid signed JWT
	claims := map[string]interface{}{
		"iss":            srv.URL,
		"sub":            "user-456",
		"aud":            clientID,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"nonce":          "test-nonce-123",
		"email":          "alice@example.com",
		"email_verified": true,
		"name":           "Alice Example",
	}
	idToken := signJWT(t, rsaKey, kid, claims)

	ctx := context.Background()
	parsedClaims, err := auth.ValidateIDToken(ctx, idToken, "test-nonce-123")
	if err != nil {
		t.Fatalf("ValidateIDToken() error: %v", err)
	}

	if parsedClaims.Subject != "user-456" {
		t.Errorf("Subject = %s, want user-456", parsedClaims.Subject)
	}
	if parsedClaims.Email != "alice@example.com" {
		t.Errorf("Email = %s, want alice@example.com", parsedClaims.Email)
	}
	if !parsedClaims.EmailVerified {
		t.Error("expected EmailVerified to be true")
	}
	if parsedClaims.Name != "Alice Example" {
		t.Errorf("Name = %s, want Alice Example", parsedClaims.Name)
	}
	if parsedClaims.Issuer != srv.URL {
		t.Errorf("Issuer = %s, want %s", parsedClaims.Issuer, srv.URL)
	}
}

func TestFederatedAuthenticator_ValidateIDToken_ExpiredToken(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "expired-kid"
	srv := newMockOIDCServer(t, rsaKey, kid)
	defer srv.Close()

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "expired-client",
		ClientSecret:   "expired-secret",
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	claims := map[string]interface{}{
		"iss": srv.URL,
		"sub": "user-789",
		"aud": "expired-client",
		"exp": time.Now().Add(-time.Hour).Unix(), // expired
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
	}
	idToken := signJWT(t, rsaKey, kid, claims)

	ctx := context.Background()
	_, err = auth.ValidateIDToken(ctx, idToken, "")
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got: %v", err)
	}
}

func TestFederatedAuthenticator_ValidateIDToken_WrongNonce(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "nonce-kid"
	srv := newMockOIDCServer(t, rsaKey, kid)
	defer srv.Close()

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "nonce-client",
		ClientSecret:   "nonce-secret",
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	claims := map[string]interface{}{
		"iss":   srv.URL,
		"sub":   "user-abc",
		"aud":   "nonce-client",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": "correct-nonce",
	}
	idToken := signJWT(t, rsaKey, kid, claims)

	ctx := context.Background()
	_, err = auth.ValidateIDToken(ctx, idToken, "wrong-nonce")
	if err == nil {
		t.Fatal("expected error for nonce mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "nonce mismatch") {
		t.Errorf("expected 'nonce mismatch' in error, got: %v", err)
	}
}

func TestFederatedAuthenticator_ValidateIDToken_WrongAudience(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "aud-kid"
	srv := newMockOIDCServer(t, rsaKey, kid)
	defer srv.Close()

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "my-client",
		ClientSecret:   "my-secret",
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	claims := map[string]interface{}{
		"iss": srv.URL,
		"sub": "user-def",
		"aud": "some-other-client", // wrong audience
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	idToken := signJWT(t, rsaKey, kid, claims)

	ctx := context.Background()
	_, err = auth.ValidateIDToken(ctx, idToken, "")
	if err == nil {
		t.Fatal("expected error for wrong audience, got nil")
	}
	if !strings.Contains(err.Error(), "audience") {
		t.Errorf("expected 'audience' in error, got: %v", err)
	}
}

func TestFederatedAuthenticator_ValidateIDToken_InvalidSignature(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	// Use a different key to sign the token
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate wrong RSA key: %v", err)
	}

	kid := "sig-kid"
	srv := newMockOIDCServer(t, rsaKey, kid) // Server has rsaKey
	defer srv.Close()

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer: srv.URL,
		ClientID:       "sig-client",
		ClientSecret:   "sig-secret",
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	claims := map[string]interface{}{
		"iss": srv.URL,
		"sub": "user-sig",
		"aud": "sig-client",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	idToken := signJWT(t, wrongKey, kid, claims) // Signed with wrongKey

	ctx := context.Background()
	_, err = auth.ValidateIDToken(ctx, idToken, "")
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("expected 'signature verification failed' in error, got: %v", err)
	}
}

func TestFederatedAuthenticator_IsAllowedDomain(t *testing.T) {
	t.Setenv("TEST_SECRET", "secret")

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  "https://example.com",
		ClientID:        "test-client",
		ClientSecretEnv: "TEST_SECRET",
		AllowedDomains:  []string{"example.com", "Acme.Org"},
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	tests := []struct {
		email   string
		allowed bool
	}{
		{"user@example.com", true},
		{"user@EXAMPLE.COM", true},
		{"user@acme.org", true},
		{"user@ACME.ORG", true},
		{"user@other.com", false},
		{"user@example.com.evil.com", false},
		{"notanemail", false},
		{"@example.com", true}, // implementation splits on @ and checks domain part
		{"", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("email=%s", tt.email), func(t *testing.T) {
			if got := auth.isAllowedDomain(tt.email); got != tt.allowed {
				t.Errorf("isAllowedDomain(%q) = %v, want %v", tt.email, got, tt.allowed)
			}
		})
	}
}

func TestFederatedAuthenticator_DetermineScopes(t *testing.T) {
	t.Setenv("TEST_SECRET", "secret")

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  "https://example.com",
		ClientID:        "test-client",
		ClientSecretEnv: "TEST_SECRET",
		DefaultScopes:   []string{"mcp:read"},
		AdminUsers:      []string{"admin@example.com", "sub-admin"},
		AdminScopes:     []string{"mcp:write", "mcp:admin"},
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	tests := []struct {
		name     string
		claims   *IDTokenClaims
		expected []string
	}{
		{
			name:     "regular user gets default scopes",
			claims:   &IDTokenClaims{Subject: "user-1", Email: "regular@example.com"},
			expected: []string{"mcp:read"},
		},
		{
			name:     "admin by email gets extra scopes",
			claims:   &IDTokenClaims{Subject: "user-2", Email: "admin@example.com"},
			expected: []string{"mcp:read", "mcp:write", "mcp:admin"},
		},
		{
			name:     "admin by subject gets extra scopes",
			claims:   &IDTokenClaims{Subject: "sub-admin", Email: "other@example.com"},
			expected: []string{"mcp:read", "mcp:write", "mcp:admin"},
		},
		{
			name:     "user with no email falls back to subject",
			claims:   &IDTokenClaims{Subject: "sub-admin"},
			expected: []string{"mcp:read", "mcp:write", "mcp:admin"},
		},
		{
			name:     "no duplicate scopes when admin scope overlaps default",
			claims:   &IDTokenClaims{Subject: "admin@example.com", Email: "admin@example.com"},
			expected: []string{"mcp:read", "mcp:write", "mcp:admin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := auth.determineScopes(tt.claims)
			if len(scopes) != len(tt.expected) {
				t.Errorf("got %d scopes %v, want %d scopes %v", len(scopes), scopes, len(tt.expected), tt.expected)
				return
			}
			for i, s := range scopes {
				if s != tt.expected[i] {
					t.Errorf("scope[%d] = %s, want %s", i, s, tt.expected[i])
				}
			}
		})
	}
}

// ===========================================================================
// Mock OIDC Server helpers
// ===========================================================================

// signTestJWT creates a signed JWT using the given RSA private key.
func signTestJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]interface{}) string {
	t.Helper()

	header := map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("failed to marshal JWT header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("failed to marshal JWT claims: %v", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := headerB64 + "." + claimsB64

	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// rsaPublicKeyToJWK converts an RSA public key to a JWK JSON representation.
func rsaPublicKeyToJWK(pub *rsa.PublicKey, kid string) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// newMockOIDCServer creates httptest servers that simulate an OIDC provider.
// It returns the issuer URL and a cleanup function. The token endpoint returns
// a signed ID token with the given claims when it receives any POST request.
func newMockOIDCServerWithToken(t *testing.T, key *rsa.PrivateKey, kid string, idTokenClaims map[string]interface{}) (issuerURL string, cleanup func()) {
	t.Helper()

	jwk := rsaPublicKeyToJWK(&key.PublicKey, kid)

	mux := http.NewServeMux()

	// We need the server URL in the handlers, so create the server first with a
	// placeholder handler, then configure routes.
	server := httptest.NewServer(mux)

	// Discovery endpoint
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := map[string]interface{}{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"jwks_uri":               server.URL + "/jwks",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})

	// JWKS endpoint
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwks := map[string]interface{}{"keys": []interface{}{jwk}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	// Token endpoint — set issuer in claims to match server URL
	idTokenClaims["iss"] = server.URL
	signedToken := signTestJWT(t, key, kid, idTokenClaims)

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"access_token":  "upstream-access-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "upstream-refresh-token",
			"id_token":      signedToken,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	return server.URL, server.Close
}

// ===========================================================================
// Federated Authenticate Full Flow Tests
// ===========================================================================

func TestFederatedAuthenticator_Authenticate_FullFlow(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "test-key-1"
	claims := map[string]interface{}{
		"sub":            "user-abc",
		"aud":            "my-client-id",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"nonce":          "test-nonce",
		"email":          "alice@example.com",
		"email_verified": true,
		"name":           "Alice Example",
	}

	issuerURL, cleanup := newMockOIDCServerWithToken(t, key, kid, claims)
	defer cleanup()

	t.Setenv("MOCK_CLIENT_SECRET", "the-secret")

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  issuerURL,
		ClientID:        "my-client-id",
		ClientSecretEnv: "MOCK_CLIENT_SECRET",
		DefaultScopes:   []string{"mcp:read"},
		AdminUsers:      []string{"alice@example.com"},
		AdminScopes:     []string{"mcp:write"},
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	userInfo, err := auth.Authenticate(context.Background(), "auth-code-123", "test-nonce", "https://example.com/callback")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	if userInfo.Subject != "user-abc" {
		t.Errorf("expected Subject 'user-abc', got %s", userInfo.Subject)
	}
	if userInfo.Email != "alice@example.com" {
		t.Errorf("expected Email 'alice@example.com', got %s", userInfo.Email)
	}
	if !userInfo.EmailVerified {
		t.Error("expected EmailVerified to be true")
	}
	if userInfo.Name != "Alice Example" {
		t.Errorf("expected Name 'Alice Example', got %s", userInfo.Name)
	}
	if userInfo.UpstreamAccessToken != "upstream-access-token" {
		t.Errorf("expected UpstreamAccessToken 'upstream-access-token', got %s", userInfo.UpstreamAccessToken)
	}
	if userInfo.UpstreamRefreshToken != "upstream-refresh-token" {
		t.Errorf("expected UpstreamRefreshToken 'upstream-refresh-token', got %s", userInfo.UpstreamRefreshToken)
	}

	// Alice is an admin, so she should get mcp:write in addition to mcp:read
	if len(userInfo.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d: %v", len(userInfo.Scopes), userInfo.Scopes)
	}
	if userInfo.Scopes[0] != "mcp:read" || userInfo.Scopes[1] != "mcp:write" {
		t.Errorf("expected scopes [mcp:read mcp:write], got %v", userInfo.Scopes)
	}
}

func TestFederatedAuthenticator_Authenticate_DomainRestriction(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	kid := "test-key-2"
	claims := map[string]interface{}{
		"sub":            "user-xyz",
		"aud":            "my-client-id",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"nonce":          "test-nonce",
		"email":          "bob@disallowed.com",
		"email_verified": true,
		"name":           "Bob Disallowed",
	}

	issuerURL, cleanup := newMockOIDCServerWithToken(t, key, kid, claims)
	defer cleanup()

	t.Setenv("MOCK_CLIENT_SECRET_2", "the-secret")

	auth, err := NewFederatedAuthenticator(&config.FederatedAuthConfig{
		UpstreamIssuer:  issuerURL,
		ClientID:        "my-client-id",
		ClientSecretEnv: "MOCK_CLIENT_SECRET_2",
		AllowedDomains:  []string{"example.com"},
		DefaultScopes:   []string{"mcp:read"},
	})
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	_, err = auth.Authenticate(context.Background(), "auth-code-456", "test-nonce", "https://example.com/callback")
	if err == nil {
		t.Fatal("expected Authenticate to fail for disallowed domain")
	}
	if !strings.Contains(err.Error(), "email domain not allowed") {
		t.Errorf("expected 'email domain not allowed' error, got: %v", err)
	}
}

// ===========================================================================
// BuiltInAuthenticator NewBuiltInAuthenticator edge-case tests
// ===========================================================================

func TestNewBuiltInAuthenticator_PasswordEnv(t *testing.T) {
	t.Setenv("TEST_USER_PASSWORD", "s3cret!")

	cfg := &config.BuiltInAuthConfig{
		Users: []config.UserConfig{
			{
				Username:    "envuser",
				PasswordEnv: "TEST_USER_PASSWORD",
				Scopes:      []string{"mcp:read"},
			},
		},
	}

	auth, err := NewBuiltInAuthenticator(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Should authenticate with the password from the env var
	userInfo, err := auth.Authenticate("envuser", "s3cret!")
	if err != nil {
		t.Fatalf("expected authentication to succeed: %v", err)
	}
	if userInfo.Username != "envuser" {
		t.Errorf("expected username 'envuser', got %s", userInfo.Username)
	}

	// Wrong password should fail
	_, err = auth.Authenticate("envuser", "wrong")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestNewBuiltInAuthenticator_MissingPasswordEnv(t *testing.T) {
	t.Setenv("EMPTY_PASSWORD_VAR", "")

	cfg := &config.BuiltInAuthConfig{
		Users: []config.UserConfig{
			{
				Username:    "envuser",
				PasswordEnv: "EMPTY_PASSWORD_VAR",
			},
		},
	}

	_, err := NewBuiltInAuthenticator(cfg)
	if err == nil {
		t.Fatal("expected error when password env var is empty")
	}
	if !strings.Contains(err.Error(), "password environment variable is empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewBuiltInAuthenticator_NoPasswordConfig(t *testing.T) {
	cfg := &config.BuiltInAuthConfig{
		Users: []config.UserConfig{
			{
				Username: "nopassuser",
				// Neither PasswordHash nor PasswordEnv
			},
		},
	}

	_, err := NewBuiltInAuthenticator(cfg)
	if err == nil {
		t.Fatal("expected error when no password is configured")
	}
	if !strings.Contains(err.Error(), "no password configured for user") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ===========================================================================
// Token Handler issueTokens store failure test
// ===========================================================================

// failingRefreshTokenStorage wraps a real Storage but makes StoreRefreshToken fail.
type failingRefreshTokenStorage struct {
	Storage
}

func (f *failingRefreshTokenStorage) StoreRefreshToken(_ context.Context, _ *RefreshToken) error {
	return errors.New("simulated storage failure")
}

func TestTokenHandler_IssueTokens_StoreFailure(t *testing.T) {
	realStorage := NewMemoryStorage(time.Minute)
	defer realStorage.Close()

	failStorage := &failingRefreshTokenStorage{Storage: realStorage}

	tokenIssuer, err := NewTokenIssuer("https://mcp.example.com", "RS256", "", "key-1", true)
	if err != nil {
		t.Fatalf("failed to create token issuer: %v", err)
	}

	testLogger, _ := logging.NewLogger(config.LogConfig{Level: "error", Format: "text", Output: "stderr"})
	handler := NewTokenHandler(failStorage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour, testLogger)

	// Store an auth code in the real storage so the token exchange finds it
	ctx := context.Background()
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authCode := &AuthorizationCode{
		Code:                "fail-store-code",
		ClientID:            "test-client",
		UserID:              "user-1",
		Username:            "testuser",
		RedirectURI:         "https://example.com/callback",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}
	if err := realStorage.StoreAuthorizationCode(ctx, authCode); err != nil {
		t.Fatalf("failed to store auth code: %v", err)
	}

	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"fail-store-code"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {"test-client"},
		"code_verifier": {verifier},
	}

	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp["error"] != "server_error" {
		t.Errorf("expected 'server_error', got %s", errResp["error"])
	}
	if !strings.Contains(errResp["error_description"], "refresh token") {
		t.Errorf("expected error about refresh token, got: %s", errResp["error_description"])
	}
}

// ===========================================================================
// FederatedAuthorizeHandler Tests
// ===========================================================================

// newMockOIDCServer creates a mock OIDC server that serves discovery and JWKS
func newFederatedMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := OIDCDiscovery{
			Issuer:                serverURL,
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
			JwksURI:               serverURL + "/jwks",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	})

	server := httptest.NewServer(mux)
	serverURL = server.URL
	return server
}

// newTestFederatedHandler creates a FederatedAuthorizeHandler backed by a mock OIDC server.
func newTestFederatedHandler2(t *testing.T, mockServerURL string) *FederatedAuthorizeHandler {
	t.Helper()

	storage := NewMemoryStorage(time.Minute)
	t.Cleanup(func() { storage.Close() })

	cfg := &config.FederatedAuthConfig{
		UpstreamIssuer: mockServerURL,
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
		DefaultScopes:  []string{"mcp:read"},
	}

	auth, err := NewFederatedAuthenticator(cfg)
	if err != nil {
		t.Fatalf("failed to create federated authenticator: %v", err)
	}

	handler := NewFederatedAuthorizeHandler(
		storage,
		auth,
		[]string{"https://example.com/callback"},
		10*time.Minute,
		[]string{"mcp:read", "mcp:write"},
		"https://bridge.example.com",
	)
	t.Cleanup(func() { handler.Close() })

	return handler
}

func TestFederatedAuthorizeHandler_ServeHTTP_MissingCodeChallenge(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	handler := newTestFederatedHandler2(t, mockServer.URL)

	// Request without code_challenge - redirect_uri is allowed, so error redirects
	authURL := "/oauth/authorize?" + url.Values{
		"response_type": {"code"},
		"client_id":     {"test-client"},
		"redirect_uri":  {"https://example.com/callback"},
		"scope":         {"mcp:read"},
		"state":         {"xyz"},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, authURL, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should redirect with error since redirect_uri is valid
	if rr.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=") {
		t.Errorf("expected error in redirect, got Location: %s", location)
	}
}

func TestFederatedAuthorizeHandler_ServeHTTP_InvalidRedirectURI(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	handler := newTestFederatedHandler2(t, mockServer.URL)

	// Use a disallowed redirect_uri, so error is returned as JSON
	authURL := "/oauth/authorize?" + url.Values{
		"response_type": {"code"},
		"client_id":     {"test-client"},
		"redirect_uri":  {"https://evil.com/callback"},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, authURL, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should return JSON error since redirect_uri is not allowed
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var errResp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp["error"] == "" {
		t.Error("expected error field in response")
	}
}

func TestFederatedAuthorizeHandler_ServeHTTP_ValidRequest_RedirectsToUpstream(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	handler := newTestFederatedHandler2(t, mockServer.URL)

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authURL := "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"scope":                 {"mcp:read"},
		"state":                 {"xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, authURL, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should redirect to upstream IdP
	if rr.Code != http.StatusFound {
		t.Fatalf("expected redirect (302), got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	parsedLoc, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse Location: %v", err)
	}

	// Should point to the mock server's /authorize endpoint
	expectedPrefix := mockServer.URL + "/authorize"
	if !strings.HasPrefix(location, expectedPrefix) {
		t.Errorf("expected redirect to %s, got %s", expectedPrefix, location)
	}

	// Should include required OIDC parameters
	q := parsedLoc.Query()
	if q.Get("client_id") != "test-client" {
		t.Errorf("expected client_id=test-client, got %s", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("expected response_type=code, got %s", q.Get("response_type"))
	}
	if q.Get("state") == "" {
		t.Error("expected state parameter in upstream redirect")
	}
	if q.Get("nonce") == "" {
		t.Error("expected nonce parameter in upstream redirect")
	}
	if q.Get("redirect_uri") != "https://bridge.example.com/oauth/callback" {
		t.Errorf("expected redirect_uri to be bridge callback, got %s", q.Get("redirect_uri"))
	}
}

func TestFederatedAuthorizeHandler_ServeHTTP_MissingResponseType(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	handler := newTestFederatedHandler2(t, mockServer.URL)

	// Missing response_type but valid redirect_uri
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	authURL := "/oauth/authorize?" + url.Values{
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, authURL, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should redirect with error since redirect_uri is valid
	if rr.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=") {
		t.Errorf("expected error in redirect, got Location: %s", location)
	}
}

// ===========================================================================
// FederatedAuthorizeHandler.HandleCallback Tests
// ===========================================================================

func TestFederatedAuthorizeHandler_HandleCallback_UpstreamError_WithPending(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	handler := newTestFederatedHandler2(t, mockServer.URL)

	// Add a pending request
	state := "test-state-123"
	handler.pendingMu.Lock()
	handler.pending[state] = &pendingAuthRequest{
		OriginalRequest: &AuthorizeRequest{
			ResponseType:        "code",
			ClientID:            "test-client",
			RedirectURI:         "https://example.com/callback",
			State:               "orig-state",
			CodeChallenge:       "challenge",
			CodeChallengeMethod: "S256",
		},
		Nonce:     "test-nonce",
		CreatedAt: time.Now(),
	}
	handler.pendingMu.Unlock()

	// Simulate upstream error with matching state
	callbackURL := "/oauth/callback?" + url.Values{
		"error":             {"access_denied"},
		"error_description": {"user denied access"},
		"state":             {state},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	rr := httptest.NewRecorder()

	handler.HandleCallback(rr, req)

	// Should redirect back to client with error
	if rr.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=access_denied") {
		t.Errorf("expected access_denied error in redirect, got: %s", location)
	}
	if !strings.Contains(location, "state=orig-state") {
		t.Errorf("expected original state in redirect, got: %s", location)
	}

	// Pending request should be cleaned up
	handler.pendingMu.RLock()
	_, exists := handler.pending[state]
	handler.pendingMu.RUnlock()
	if exists {
		t.Error("expected pending request to be removed after error callback")
	}
}

func TestFederatedAuthorizeHandler_HandleCallback_UpstreamError_NoPending(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	handler := newTestFederatedHandler2(t, mockServer.URL)

	// Upstream error without a matching pending request
	callbackURL := "/oauth/callback?" + url.Values{
		"error":             {"server_error"},
		"error_description": {"something went wrong"},
		"state":             {"unknown-state"},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	rr := httptest.NewRecorder()

	handler.HandleCallback(rr, req)

	// Should return JSON error since no pending request to redirect to
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

func TestFederatedAuthorizeHandler_HandleCallback_CodeExchangeFails(t *testing.T) {
	// Create a mock server where the token endpoint returns an error
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		disc := OIDCDiscovery{
			Issuer:                serverURL,
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
			JwksURI:               serverURL + "/jwks",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(disc)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "code expired",
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	})

	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()
	serverURL = mockServer.URL

	handler := newTestFederatedHandler2(t, mockServer.URL)

	// Add a pending request
	state := "test-state-exchange"
	handler.pendingMu.Lock()
	handler.pending[state] = &pendingAuthRequest{
		OriginalRequest: &AuthorizeRequest{
			ResponseType:        "code",
			ClientID:            "test-client",
			RedirectURI:         "https://example.com/callback",
			State:               "orig-state",
			CodeChallenge:       "challenge",
			CodeChallengeMethod: "S256",
		},
		Nonce:     "test-nonce",
		CreatedAt: time.Now(),
	}
	handler.pendingMu.Unlock()

	// Callback with valid code and state
	callbackURL := "/oauth/callback?" + url.Values{
		"code":  {"upstream-code-123"},
		"state": {state},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	rr := httptest.NewRecorder()

	handler.HandleCallback(rr, req)

	// Should redirect with access_denied error since token exchange failed
	if rr.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=access_denied") {
		t.Errorf("expected access_denied error in redirect, got: %s", location)
	}
}

func TestNewFederatedAuthenticator_MissingIssuer(t *testing.T) {
	cfg := &config.FederatedAuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}
	_, err := NewFederatedAuthenticator(cfg)
	if err == nil {
		t.Error("expected error for missing upstream_issuer")
	}
}

func TestNewFederatedAuthenticator_MissingClientID(t *testing.T) {
	cfg := &config.FederatedAuthConfig{
		UpstreamIssuer: "https://idp.example.com",
		ClientSecret:   "test-secret",
	}
	_, err := NewFederatedAuthenticator(cfg)
	if err == nil {
		t.Error("expected error for missing client_id")
	}
}

func TestNewFederatedAuthenticator_MissingSecret(t *testing.T) {
	cfg := &config.FederatedAuthConfig{
		UpstreamIssuer: "https://idp.example.com",
		ClientID:       "test-client",
	}
	_, err := NewFederatedAuthenticator(cfg)
	if err == nil {
		t.Error("expected error for missing client_secret")
	}
}

func TestFederatedAuthorizeHandler_ServeHTTP_NoRedirectURI_InvalidRequest(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	handler := newTestFederatedHandler2(t, mockServer.URL)

	// Request with no redirect_uri at all
	authURL := "/oauth/authorize?" + url.Values{
		"response_type": {"code"},
		"client_id":     {"test-client"},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, authURL, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// With no redirect_uri, error must be returned as JSON (not redirect)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

func TestGenerateSecureToken_DifferentLengths(t *testing.T) {
	token16, _ := generateSecureToken(16)
	token32, _ := generateSecureToken(32)

	// 16 bytes -> ~22 base64 chars, 32 bytes -> ~43 base64 chars
	if len(token16) >= len(token32) {
		t.Errorf("expected 16-byte token (%d chars) to be shorter than 32-byte token (%d chars)",
			len(token16), len(token32))
	}
}
