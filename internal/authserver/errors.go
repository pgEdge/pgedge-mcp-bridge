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
	"encoding/json"
	"net/http"
	"net/url"
)

// OAuth 2.0 error codes as defined in RFC 6749.
const (
	// ErrorInvalidRequest - The request is missing a required parameter,
	// includes an invalid parameter value, includes a parameter more than once,
	// or is otherwise malformed.
	ErrorInvalidRequest = "invalid_request"

	// ErrorUnauthorizedClient - The client is not authorized to request
	// an authorization code using this method.
	ErrorUnauthorizedClient = "unauthorized_client"

	// ErrorAccessDenied - The resource owner or authorization server denied the request.
	ErrorAccessDenied = "access_denied"

	// ErrorUnsupportedResponseType - The authorization server does not support
	// obtaining an authorization code using this method.
	ErrorUnsupportedResponseType = "unsupported_response_type"

	// ErrorInvalidScope - The requested scope is invalid, unknown, or malformed.
	ErrorInvalidScope = "invalid_scope"

	// ErrorServerError - The authorization server encountered an unexpected condition.
	ErrorServerError = "server_error"

	// ErrorTemporarilyUnavailable - The authorization server is currently unable
	// to handle the request due to temporary overloading or maintenance.
	ErrorTemporarilyUnavailable = "temporarily_unavailable"

	// ErrorInvalidClient - Client authentication failed.
	ErrorInvalidClient = "invalid_client"

	// ErrorInvalidGrant - The provided authorization grant or refresh token
	// is invalid, expired, revoked, or was issued to another client.
	ErrorInvalidGrant = "invalid_grant"

	// ErrorUnsupportedGrantType - The authorization grant type is not supported.
	ErrorUnsupportedGrantType = "unsupported_grant_type"
)

// OAuthError represents an OAuth 2.0 error response.
type OAuthError struct {
	// Code is the error code (e.g., "invalid_request").
	Code string `json:"error"`

	// Description is a human-readable description of the error.
	Description string `json:"error_description,omitempty"`

	// URI is a URI identifying a web page with error information.
	URI string `json:"error_uri,omitempty"`

	// State is the state parameter from the authorization request.
	State string `json:"state,omitempty"`

	// HTTPStatus is the HTTP status code to return.
	HTTPStatus int `json:"-"`
}

// Error implements the error interface.
func (e *OAuthError) Error() string {
	if e.Description != "" {
		return e.Code + ": " + e.Description
	}
	return e.Code
}

// NewOAuthError creates a new OAuth error.
func NewOAuthError(code, description string, httpStatus int) *OAuthError {
	return &OAuthError{
		Code:        code,
		Description: description,
		HTTPStatus:  httpStatus,
	}
}

// WithState adds the state parameter to the error.
func (e *OAuthError) WithState(state string) *OAuthError {
	e.State = state
	return e
}

// Common OAuth errors

// ErrInvalidRequest creates an invalid_request error.
func ErrInvalidRequest(description string) *OAuthError {
	return NewOAuthError(ErrorInvalidRequest, description, http.StatusBadRequest)
}

// ErrUnauthorizedClient creates an unauthorized_client error.
func ErrUnauthorizedClient(description string) *OAuthError {
	return NewOAuthError(ErrorUnauthorizedClient, description, http.StatusUnauthorized)
}

// ErrAccessDenied creates an access_denied error.
func ErrAccessDenied(description string) *OAuthError {
	return NewOAuthError(ErrorAccessDenied, description, http.StatusForbidden)
}

// ErrUnsupportedResponseType creates an unsupported_response_type error.
func ErrUnsupportedResponseType(description string) *OAuthError {
	return NewOAuthError(ErrorUnsupportedResponseType, description, http.StatusBadRequest)
}

// ErrInvalidScope creates an invalid_scope error.
func ErrInvalidScope(description string) *OAuthError {
	return NewOAuthError(ErrorInvalidScope, description, http.StatusBadRequest)
}

// ErrServerError creates a server_error error.
func ErrServerError(description string) *OAuthError {
	return NewOAuthError(ErrorServerError, description, http.StatusInternalServerError)
}

// ErrTemporarilyUnavailable creates a temporarily_unavailable error.
func ErrTemporarilyUnavailable(description string) *OAuthError {
	return NewOAuthError(ErrorTemporarilyUnavailable, description, http.StatusServiceUnavailable)
}

// ErrInvalidClient creates an invalid_client error.
func ErrInvalidClient(description string) *OAuthError {
	return NewOAuthError(ErrorInvalidClient, description, http.StatusUnauthorized)
}

// ErrInvalidGrant creates an invalid_grant error.
func ErrInvalidGrant(description string) *OAuthError {
	return NewOAuthError(ErrorInvalidGrant, description, http.StatusBadRequest)
}

// ErrUnsupportedGrantType creates an unsupported_grant_type error.
func ErrUnsupportedGrantType(description string) *OAuthError {
	return NewOAuthError(ErrorUnsupportedGrantType, description, http.StatusBadRequest)
}

// WriteJSONError writes an OAuth error as a JSON response.
func WriteJSONError(w http.ResponseWriter, err *OAuthError) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if err.HTTPStatus == 0 {
		err.HTTPStatus = http.StatusBadRequest
	}
	w.WriteHeader(err.HTTPStatus)

	_ = json.NewEncoder(w).Encode(err)
}

// RedirectWithError redirects to the redirect_uri with error parameters.
// This is used for authorization endpoint errors.
func RedirectWithError(w http.ResponseWriter, r *http.Request, redirectURI string, err *OAuthError) {
	u, parseErr := url.Parse(redirectURI)
	if parseErr != nil {
		// Fall back to JSON error if redirect URI is invalid
		WriteJSONError(w, err)
		return
	}

	q := u.Query()
	q.Set("error", err.Code)
	if err.Description != "" {
		q.Set("error_description", err.Description)
	}
	if err.State != "" {
		q.Set("state", err.State)
	}
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
}
