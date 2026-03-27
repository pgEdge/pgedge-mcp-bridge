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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FederatedAuthorizeHandler handles the authorization endpoint for federated mode.
type FederatedAuthorizeHandler struct {
	storage             Storage
	federatedAuth       *FederatedAuthenticator
	allowedRedirectURIs []string
	authCodeLifetime    time.Duration
	scopesSupported     []string
	issuer              string

	// Pending authorization state (stores original OAuth request while user authenticates with IdP)
	pendingMu sync.RWMutex
	pending   map[string]*pendingAuthRequest

	// done signals the cleanup goroutine to stop
	done chan struct{}
}

// pendingAuthRequest stores state while the user is authenticating with upstream IdP
type pendingAuthRequest struct {
	OriginalRequest *AuthorizeRequest
	Nonce           string // Nonce for upstream ID token validation
	CreatedAt       time.Time
}

// NewFederatedAuthorizeHandler creates a new federated authorization handler.
func NewFederatedAuthorizeHandler(
	storage Storage,
	federatedAuth *FederatedAuthenticator,
	allowedRedirectURIs []string,
	authCodeLifetime time.Duration,
	scopesSupported []string,
	issuer string,
) *FederatedAuthorizeHandler {
	h := &FederatedAuthorizeHandler{
		storage:             storage,
		federatedAuth:       federatedAuth,
		allowedRedirectURIs: allowedRedirectURIs,
		authCodeLifetime:    authCodeLifetime,
		scopesSupported:     scopesSupported,
		issuer:              issuer,
		pending:             make(map[string]*pendingAuthRequest),
		done:                make(chan struct{}),
	}

	// Start cleanup goroutine for expired pending requests
	go h.cleanupPending()

	return h
}

// cleanupPending removes expired pending authorization requests
func (h *FederatedAuthorizeHandler) cleanupPending() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.pendingMu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for state, req := range h.pending {
				if req.CreatedAt.Before(cutoff) {
					delete(h.pending, state)
				}
			}
			h.pendingMu.Unlock()
		}
	}
}

// ServeHTTP handles GET requests by redirecting to upstream IdP.
func (h *FederatedAuthorizeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse and validate the authorization request
	req, oauthErr := h.parseAndValidateRequest(r)
	if oauthErr != nil {
		redirectURI := r.URL.Query().Get("redirect_uri")
		if redirectURI != "" && h.isAllowedRedirectURI(redirectURI) {
			RedirectWithError(w, r, redirectURI, oauthErr)
			return
		}
		WriteJSONError(w, oauthErr)
		return
	}

	// Generate state that includes our internal tracking
	internalState, err := generateSecureToken(32)
	if err != nil {
		RedirectWithError(w, r, req.RedirectURI, ErrServerError("failed to generate state").WithState(req.State))
		return
	}

	// Generate nonce for ID token validation
	nonce, err := generateSecureToken(16)
	if err != nil {
		RedirectWithError(w, r, req.RedirectURI, ErrServerError("failed to generate nonce").WithState(req.State))
		return
	}

	// Store pending request
	h.pendingMu.Lock()
	h.pending[internalState] = &pendingAuthRequest{
		OriginalRequest: req,
		Nonce:           nonce,
		CreatedAt:       time.Now(),
	}
	h.pendingMu.Unlock()

	// Build callback URI for upstream IdP
	callbackURI := h.issuer + "/oauth/callback"

	// Get authorization URL from upstream IdP
	upstreamURL, err := h.federatedAuth.GetAuthorizationURL(r.Context(), internalState, nonce, callbackURI)
	if err != nil {
		RedirectWithError(w, r, req.RedirectURI, ErrServerError("failed to build authorization URL").WithState(req.State))
		return
	}

	// Redirect to upstream IdP
	http.Redirect(w, r, upstreamURL, http.StatusFound)
}

// HandleCallback handles the callback from the upstream IdP.
func (h *FederatedAuthorizeHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()

	// Check for error from upstream
	if errCode := q.Get("error"); errCode != "" {
		errDesc := q.Get("error_description")
		// Try to recover the original request to redirect properly
		state := q.Get("state")
		h.pendingMu.RLock()
		pending := h.pending[state]
		h.pendingMu.RUnlock()

		if pending != nil {
			h.pendingMu.Lock()
			delete(h.pending, state)
			h.pendingMu.Unlock()

			RedirectWithError(w, r, pending.OriginalRequest.RedirectURI,
				&OAuthError{Code: errCode, Description: errDesc, State: pending.OriginalRequest.State})
			return
		}

		// No pending request, show generic error
		WriteJSONError(w, &OAuthError{Code: errCode, Description: errDesc})
		return
	}

	// Get the authorization code from upstream
	code := q.Get("code")
	state := q.Get("state")

	if code == "" {
		WriteJSONError(w, ErrInvalidRequest("missing authorization code"))
		return
	}

	// Retrieve pending request
	h.pendingMu.RLock()
	pending := h.pending[state]
	h.pendingMu.RUnlock()

	if pending == nil {
		WriteJSONError(w, ErrInvalidRequest("invalid or expired state"))
		return
	}

	// Remove from pending
	h.pendingMu.Lock()
	delete(h.pending, state)
	h.pendingMu.Unlock()

	// Build callback URI (must match what we sent to upstream)
	callbackURI := h.issuer + "/oauth/callback"

	// Authenticate with upstream (exchange code, validate ID token)
	userInfo, err := h.federatedAuth.Authenticate(r.Context(), code, pending.Nonce, callbackURI)
	if err != nil {
		RedirectWithError(w, r, pending.OriginalRequest.RedirectURI,
			ErrAccessDenied("authentication failed: "+err.Error()).WithState(pending.OriginalRequest.State))
		return
	}

	// Filter requested scopes against user's granted scopes
	requestedScopes := strings.Fields(pending.OriginalRequest.Scope)
	grantedScopes := FilterScopes(requestedScopes, userInfo.Scopes)
	if len(grantedScopes) == 0 {
		// Grant user's default scopes
		grantedScopes = userInfo.Scopes
	}

	// Generate our authorization code
	ourCode, err := GenerateAuthorizationCode()
	if err != nil {
		RedirectWithError(w, r, pending.OriginalRequest.RedirectURI,
			ErrServerError("failed to generate authorization code").WithState(pending.OriginalRequest.State))
		return
	}

	// Determine user ID - prefer email, fall back to subject
	userID := userInfo.Email
	if userID == "" {
		userID = userInfo.Subject
	}
	username := userInfo.Name
	if username == "" {
		username = userInfo.Email
		if username == "" {
			username = userInfo.Subject
		}
	}

	// Store authorization code
	authCode := &AuthorizationCode{
		Code:                ourCode,
		ClientID:            pending.OriginalRequest.ClientID,
		UserID:              userID,
		Username:            username,
		RedirectURI:         pending.OriginalRequest.RedirectURI,
		Scope:               strings.Join(grantedScopes, " "),
		CodeChallenge:       pending.OriginalRequest.CodeChallenge,
		CodeChallengeMethod: pending.OriginalRequest.CodeChallengeMethod,
		State:               pending.OriginalRequest.State,
		Nonce:               pending.OriginalRequest.Nonce,
		ExpiresAt:           time.Now().Add(h.authCodeLifetime),
		CreatedAt:           time.Now(),
		// Store upstream tokens for potential passthrough
		UpstreamAccessToken:  userInfo.UpstreamAccessToken,
		UpstreamRefreshToken: userInfo.UpstreamRefreshToken,
	}

	if err := h.storage.StoreAuthorizationCode(context.Background(), authCode); err != nil {
		RedirectWithError(w, r, pending.OriginalRequest.RedirectURI,
			ErrServerError("failed to store authorization code").WithState(pending.OriginalRequest.State))
		return
	}

	// Redirect to original client with our authorization code
	redirectURL, _ := url.Parse(pending.OriginalRequest.RedirectURI)
	query := redirectURL.Query()
	query.Set("code", ourCode)
	if pending.OriginalRequest.State != "" {
		query.Set("state", pending.OriginalRequest.State)
	}
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// parseAndValidateRequest parses and validates an authorization request.
func (h *FederatedAuthorizeHandler) parseAndValidateRequest(r *http.Request) (*AuthorizeRequest, *OAuthError) {
	q := r.URL.Query()

	req := &AuthorizeRequest{
		ResponseType:        q.Get("response_type"),
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		Scope:               q.Get("scope"),
		State:               q.Get("state"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
		Nonce:               q.Get("nonce"),
	}

	return req, h.validateRequest(req)
}

// validateRequest validates an authorization request.
func (h *FederatedAuthorizeHandler) validateRequest(req *AuthorizeRequest) *OAuthError {
	return validateAuthorizeRequest(req, h.allowedRedirectURIs)
}

// isAllowedRedirectURI checks if the URI is in the allowed list.
func (h *FederatedAuthorizeHandler) isAllowedRedirectURI(uri string) bool {
	return isAllowedRedirectURI(uri, h.allowedRedirectURIs)
}

// generateSecureToken generates a cryptographically secure random token.
func generateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Close stops background goroutines for graceful shutdown.
func (h *FederatedAuthorizeHandler) Close() {
	close(h.done)
}

// PendingRequestJSON returns the pending requests as JSON (for debugging).
func (h *FederatedAuthorizeHandler) PendingRequestJSON() ([]byte, error) {
	h.pendingMu.RLock()
	defer h.pendingMu.RUnlock()
	return json.Marshal(h.pending)
}
