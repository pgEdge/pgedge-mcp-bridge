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
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AuthorizeRequest represents a parsed authorization request.
type AuthorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Nonce               string
}

// AuthorizeHandler handles the authorization endpoint.
type AuthorizeHandler struct {
	storage             Storage
	authenticator       UserAuthenticator
	allowedRedirectURIs []string
	authCodeLifetime    time.Duration
	scopesSupported     []string
	loginTemplate       *template.Template
}

// NewAuthorizeHandler creates a new authorization handler for built-in mode.
func NewAuthorizeHandler(
	storage Storage,
	authenticator UserAuthenticator,
	allowedRedirectURIs []string,
	authCodeLifetime time.Duration,
	scopesSupported []string,
	customLoginTemplate string,
) (*AuthorizeHandler, error) {
	var loginTmpl *template.Template
	var err error

	if customLoginTemplate != "" {
		loginTmpl, err = template.ParseFiles(customLoginTemplate)
		if err != nil {
			return nil, err
		}
	} else {
		loginTmpl, err = template.New("login").Parse(defaultLoginTemplate)
		if err != nil {
			return nil, err
		}
	}

	return &AuthorizeHandler{
		storage:             storage,
		authenticator:       authenticator,
		allowedRedirectURIs: allowedRedirectURIs,
		authCodeLifetime:    authCodeLifetime,
		scopesSupported:     scopesSupported,
		loginTemplate:       loginTmpl,
	}, nil
}

// ServeHTTP handles GET (show login) and POST (process login) requests.
func (h *AuthorizeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGet validates the request and shows the login form.
func (h *AuthorizeHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	req, oauthErr := h.parseAndValidateRequest(r)
	if oauthErr != nil {
		// If we have a valid redirect_uri, redirect with error
		redirectURI := r.URL.Query().Get("redirect_uri")
		if redirectURI != "" && h.isAllowedRedirectURI(redirectURI) {
			RedirectWithError(w, r, redirectURI, oauthErr)
			return
		}
		// Otherwise show error directly
		WriteJSONError(w, oauthErr)
		return
	}

	// Show login form
	h.showLoginForm(w, req, "")
}

// handlePost processes the login form submission.
func (h *AuthorizeHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		WriteJSONError(w, ErrInvalidRequest("failed to parse form"))
		return
	}

	// Re-parse authorization request from form (preserved in hidden fields)
	req := &AuthorizeRequest{
		ResponseType:        r.FormValue("response_type"),
		ClientID:            r.FormValue("client_id"),
		RedirectURI:         r.FormValue("redirect_uri"),
		Scope:               r.FormValue("scope"),
		State:               r.FormValue("state"),
		CodeChallenge:       r.FormValue("code_challenge"),
		CodeChallengeMethod: r.FormValue("code_challenge_method"),
		Nonce:               r.FormValue("nonce"),
	}

	// Validate the request again
	if oauthErr := h.validateRequest(req); oauthErr != nil {
		if req.RedirectURI != "" && h.isAllowedRedirectURI(req.RedirectURI) {
			RedirectWithError(w, r, req.RedirectURI, oauthErr.WithState(req.State))
			return
		}
		WriteJSONError(w, oauthErr)
		return
	}

	// Get credentials from form
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.showLoginForm(w, req, "Username and password are required")
		return
	}

	// Authenticate user
	userInfo, err := h.authenticator.Authenticate(username, password)
	if err != nil {
		h.showLoginForm(w, req, "Invalid username or password")
		return
	}

	// Filter requested scopes against user's allowed scopes
	requestedScopes := strings.Fields(req.Scope)
	grantedScopes := FilterScopes(requestedScopes, userInfo.Scopes)
	if len(grantedScopes) == 0 {
		// Grant default scope if no specific scopes requested or matched
		grantedScopes = FilterScopes(h.scopesSupported, userInfo.Scopes)
	}

	// Generate authorization code
	code, err := GenerateAuthorizationCode()
	if err != nil {
		RedirectWithError(w, r, req.RedirectURI, ErrServerError("failed to generate authorization code").WithState(req.State))
		return
	}

	// Store authorization code
	authCode := &AuthorizationCode{
		Code:                code,
		ClientID:            req.ClientID,
		UserID:              userInfo.ID,
		Username:            userInfo.Username,
		RedirectURI:         req.RedirectURI,
		Scope:               strings.Join(grantedScopes, " "),
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		State:               req.State,
		Nonce:               req.Nonce,
		ExpiresAt:           time.Now().Add(h.authCodeLifetime),
		CreatedAt:           time.Now(),
	}

	if err := h.storage.StoreAuthorizationCode(context.Background(), authCode); err != nil {
		RedirectWithError(w, r, req.RedirectURI, ErrServerError("failed to store authorization code").WithState(req.State))
		return
	}

	// Redirect to client with authorization code
	redirectURL, _ := url.Parse(req.RedirectURI)
	q := redirectURL.Query()
	q.Set("code", code)
	if req.State != "" {
		q.Set("state", req.State)
	}
	redirectURL.RawQuery = q.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// parseAndValidateRequest parses and validates an authorization request.
func (h *AuthorizeHandler) parseAndValidateRequest(r *http.Request) (*AuthorizeRequest, *OAuthError) {
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
func (h *AuthorizeHandler) validateRequest(req *AuthorizeRequest) *OAuthError {
	// Validate response_type
	if req.ResponseType != "code" {
		return ErrUnsupportedResponseType("only 'code' response type is supported")
	}

	// Validate client_id
	if req.ClientID == "" {
		return ErrInvalidRequest("client_id is required")
	}

	// Validate redirect_uri
	if req.RedirectURI == "" {
		return ErrInvalidRequest("redirect_uri is required")
	}
	if !h.isAllowedRedirectURI(req.RedirectURI) {
		return ErrInvalidRequest("redirect_uri is not allowed")
	}

	// Validate PKCE (required per MCP spec)
	if req.CodeChallenge == "" {
		return ErrInvalidRequest("code_challenge is required (PKCE)")
	}
	if req.CodeChallengeMethod == "" {
		req.CodeChallengeMethod = "S256" // Default to S256
	}
	if !ValidateCodeChallengeMethod(req.CodeChallengeMethod) {
		return ErrInvalidRequest("only S256 code_challenge_method is supported")
	}
	if !ValidateCodeChallenge(req.CodeChallenge) {
		return ErrInvalidRequest("invalid code_challenge format")
	}

	return nil
}

// isAllowedRedirectURI checks if the URI is in the allowed list.
func (h *AuthorizeHandler) isAllowedRedirectURI(uri string) bool {
	for _, allowed := range h.allowedRedirectURIs {
		if allowed == uri {
			return true
		}
		// Support localhost with any port for development
		if strings.HasPrefix(allowed, "http://localhost:") && strings.HasPrefix(uri, "http://localhost:") {
			return true
		}
	}
	return false
}

// showLoginForm renders the login form.
func (h *AuthorizeHandler) showLoginForm(w http.ResponseWriter, req *AuthorizeRequest, errorMsg string) {
	data := loginTemplateData{
		Error:               errorMsg,
		ResponseType:        req.ResponseType,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		Scope:               req.Scope,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Nonce:               req.Nonce,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if err := h.loginTemplate.Execute(w, data); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

type loginTemplateData struct {
	Error               string
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Nonce               string
}

const defaultLoginTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Sign In - MCP Bridge</title>
    <style>
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: white;
            border-radius: 12px;
            box-shadow: 0 20px 60px rgba(0,0,0,0.3);
            padding: 40px;
            width: 100%;
            max-width: 400px;
        }
        h1 {
            color: #333;
            font-size: 24px;
            font-weight: 600;
            text-align: center;
            margin-bottom: 8px;
        }
        .subtitle {
            color: #666;
            text-align: center;
            margin-bottom: 32px;
            font-size: 14px;
        }
        .error {
            background: #fee2e2;
            border: 1px solid #fca5a5;
            color: #dc2626;
            padding: 12px;
            border-radius: 8px;
            margin-bottom: 20px;
            font-size: 14px;
        }
        .form-group {
            margin-bottom: 20px;
        }
        label {
            display: block;
            color: #374151;
            font-size: 14px;
            font-weight: 500;
            margin-bottom: 6px;
        }
        input[type="text"],
        input[type="password"] {
            width: 100%;
            padding: 12px 16px;
            border: 1px solid #d1d5db;
            border-radius: 8px;
            font-size: 16px;
            transition: border-color 0.2s, box-shadow 0.2s;
        }
        input[type="text"]:focus,
        input[type="password"]:focus {
            outline: none;
            border-color: #667eea;
            box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
        }
        button {
            width: 100%;
            padding: 14px;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 8px;
            font-size: 16px;
            font-weight: 600;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
        }
        button:hover {
            transform: translateY(-1px);
            box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
        }
        button:active {
            transform: translateY(0);
        }
        .client-info {
            margin-top: 24px;
            padding-top: 20px;
            border-top: 1px solid #e5e7eb;
            font-size: 12px;
            color: #9ca3af;
            text-align: center;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Sign In</h1>
        <p class="subtitle">Authorize access to MCP Bridge</p>

        {{if .Error}}
        <div class="error">{{.Error}}</div>
        {{end}}

        <form method="POST">
            <input type="hidden" name="response_type" value="{{.ResponseType}}">
            <input type="hidden" name="client_id" value="{{.ClientID}}">
            <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
            <input type="hidden" name="scope" value="{{.Scope}}">
            <input type="hidden" name="state" value="{{.State}}">
            <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
            <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
            <input type="hidden" name="nonce" value="{{.Nonce}}">

            <div class="form-group">
                <label for="username">Username</label>
                <input type="text" id="username" name="username" autocomplete="username" required autofocus>
            </div>

            <div class="form-group">
                <label for="password">Password</label>
                <input type="password" id="password" name="password" autocomplete="current-password" required>
            </div>

            <button type="submit">Sign In</button>
        </form>

        <div class="client-info">
            Signing in to: {{.ClientID}}
        </div>
    </div>
</body>
</html>`
