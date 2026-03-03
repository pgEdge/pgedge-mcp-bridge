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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

	tokenHandler := NewTokenHandler(storage, tokenIssuer, "https://mcp.example.com", time.Hour, 24*time.Hour)

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

	// Step 2: Submit login form (POST)
	formData := url.Values{
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
