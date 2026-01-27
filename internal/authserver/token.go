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
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// TokenHandler handles the token endpoint.
type TokenHandler struct {
	storage              Storage
	tokenIssuer          *TokenIssuer
	issuer               string
	tokenLifetime        time.Duration
	refreshTokenLifetime time.Duration
}

// NewTokenHandler creates a new token handler.
func NewTokenHandler(
	storage Storage,
	tokenIssuer *TokenIssuer,
	issuer string,
	tokenLifetime time.Duration,
	refreshTokenLifetime time.Duration,
) *TokenHandler {
	return &TokenHandler{
		storage:              storage,
		tokenIssuer:          tokenIssuer,
		issuer:               issuer,
		tokenLifetime:        tokenLifetime,
		refreshTokenLifetime: refreshTokenLifetime,
	}
}

// TokenResponse is the OAuth token response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ServeHTTP handles POST requests to the token endpoint.
func (h *TokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		WriteJSONError(w, ErrInvalidRequest("failed to parse form"))
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.handleAuthorizationCode(w, r)
	case "refresh_token":
		h.handleRefreshToken(w, r)
	default:
		WriteJSONError(w, ErrUnsupportedGrantType("grant_type must be 'authorization_code' or 'refresh_token'"))
	}
}

// handleAuthorizationCode handles the authorization_code grant.
func (h *TokenHandler) handleAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	clientID := r.FormValue("client_id")
	codeVerifier := r.FormValue("code_verifier")

	// Validate required parameters
	if code == "" {
		WriteJSONError(w, ErrInvalidRequest("code is required"))
		return
	}
	if redirectURI == "" {
		WriteJSONError(w, ErrInvalidRequest("redirect_uri is required"))
		return
	}
	if clientID == "" {
		WriteJSONError(w, ErrInvalidRequest("client_id is required"))
		return
	}
	if codeVerifier == "" {
		WriteJSONError(w, ErrInvalidRequest("code_verifier is required (PKCE)"))
		return
	}

	// Validate code verifier format
	if !ValidateCodeVerifier(codeVerifier) {
		WriteJSONError(w, ErrInvalidRequest("invalid code_verifier format"))
		return
	}

	ctx := r.Context()

	// Retrieve authorization code
	authCode, err := h.storage.GetAuthorizationCode(ctx, code)
	if err != nil {
		WriteJSONError(w, ErrServerError("failed to retrieve authorization code"))
		return
	}
	if authCode == nil {
		WriteJSONError(w, ErrInvalidGrant("authorization code not found or expired"))
		return
	}

	// Delete the authorization code immediately (one-time use)
	_ = h.storage.DeleteAuthorizationCode(ctx, code)

	// Validate client_id matches
	if authCode.ClientID != clientID {
		WriteJSONError(w, ErrInvalidGrant("client_id mismatch"))
		return
	}

	// Validate redirect_uri matches
	if authCode.RedirectURI != redirectURI {
		WriteJSONError(w, ErrInvalidGrant("redirect_uri mismatch"))
		return
	}

	// Validate PKCE
	if !ValidatePKCE(codeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
		WriteJSONError(w, ErrInvalidGrant("PKCE validation failed"))
		return
	}

	// Issue tokens
	h.issueTokens(w, ctx, authCode.UserID, authCode.Username, authCode.ClientID, authCode.Scope)
}

// handleRefreshToken handles the refresh_token grant.
func (h *TokenHandler) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")

	// Validate required parameters
	if refreshToken == "" {
		WriteJSONError(w, ErrInvalidRequest("refresh_token is required"))
		return
	}
	if clientID == "" {
		WriteJSONError(w, ErrInvalidRequest("client_id is required"))
		return
	}

	ctx := r.Context()

	// Retrieve refresh token
	token, err := h.storage.GetRefreshToken(ctx, refreshToken)
	if err != nil {
		WriteJSONError(w, ErrServerError("failed to retrieve refresh token"))
		return
	}
	if token == nil {
		WriteJSONError(w, ErrInvalidGrant("refresh token not found or expired"))
		return
	}

	// Validate client_id matches
	if token.ClientID != clientID {
		WriteJSONError(w, ErrInvalidGrant("client_id mismatch"))
		return
	}

	// Delete old refresh token (rotation)
	_ = h.storage.DeleteRefreshToken(ctx, refreshToken)

	// Issue new tokens
	h.issueTokens(w, ctx, token.UserID, token.Username, token.ClientID, token.Scope)
}

// issueTokens generates and returns access and refresh tokens.
func (h *TokenHandler) issueTokens(w http.ResponseWriter, ctx context.Context, userID, username, clientID, scope string) {
	// Generate access token
	accessToken, _, err := h.tokenIssuer.IssueAccessToken(
		userID,
		[]string{h.issuer},
		scope,
		clientID,
		h.tokenLifetime,
	)
	if err != nil {
		WriteJSONError(w, ErrServerError("failed to generate access token"))
		return
	}

	// Generate refresh token
	refreshTokenValue, err := GenerateRefreshToken()
	if err != nil {
		WriteJSONError(w, ErrServerError("failed to generate refresh token"))
		return
	}

	// Store refresh token
	refreshToken := &RefreshToken{
		Token:     refreshTokenValue,
		ClientID:  clientID,
		UserID:    userID,
		Username:  username,
		Scope:     scope,
		ExpiresAt: time.Now().Add(h.refreshTokenLifetime),
		CreatedAt: time.Now(),
	}

	if err := h.storage.StoreRefreshToken(ctx, refreshToken); err != nil {
		WriteJSONError(w, ErrServerError("failed to store refresh token"))
		return
	}

	// Build response
	response := TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(h.tokenLifetime.Seconds()),
		RefreshToken: refreshTokenValue,
		Scope:        scope,
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Can't do much here, headers already sent
		return
	}
}

// ScopeFromString converts a space-separated scope string to a slice.
func ScopeFromString(scope string) []string {
	if scope == "" {
		return nil
	}
	return strings.Fields(scope)
}

// ScopeToString converts a scope slice to a space-separated string.
func ScopeToString(scopes []string) string {
	return strings.Join(scopes, " ")
}
