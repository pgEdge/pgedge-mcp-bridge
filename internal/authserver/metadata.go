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
	"strings"
)

// Metadata represents the OAuth 2.0 Authorization Server Metadata (RFC 8414).
type Metadata struct {
	// Issuer is the authorization server's issuer identifier (URL).
	Issuer string `json:"issuer"`

	// AuthorizationEndpoint is the URL of the authorization endpoint.
	AuthorizationEndpoint string `json:"authorization_endpoint"`

	// TokenEndpoint is the URL of the token endpoint.
	TokenEndpoint string `json:"token_endpoint"`

	// JWKSURI is the URL of the JSON Web Key Set document.
	JWKSURI string `json:"jwks_uri"`

	// RegistrationEndpoint is the URL of the dynamic client registration endpoint.
	RegistrationEndpoint string `json:"registration_endpoint,omitempty"`

	// ScopesSupported lists the scope values this server supports.
	ScopesSupported []string `json:"scopes_supported,omitempty"`

	// ResponseTypesSupported lists the response_type values this server supports.
	ResponseTypesSupported []string `json:"response_types_supported"`

	// ResponseModesSupported lists the response_mode values this server supports.
	ResponseModesSupported []string `json:"response_modes_supported,omitempty"`

	// GrantTypesSupported lists the grant types this server supports.
	GrantTypesSupported []string `json:"grant_types_supported"`

	// TokenEndpointAuthMethodsSupported lists the client authentication methods
	// supported by the token endpoint.
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`

	// CodeChallengeMethodsSupported lists the PKCE code challenge methods supported.
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`

	// RevocationEndpoint is the URL of the token revocation endpoint.
	RevocationEndpoint string `json:"revocation_endpoint,omitempty"`

	// ServiceDocumentation is the URL for human-readable documentation.
	ServiceDocumentation string `json:"service_documentation,omitempty"`
}

// BuildMetadata constructs the OAuth metadata for the server.
func BuildMetadata(issuer string, scopes []string, allowDynamicRegistration bool) *Metadata {
	// Ensure issuer doesn't have trailing slash
	issuer = strings.TrimSuffix(issuer, "/")

	m := &Metadata{
		Issuer:                issuer,
		AuthorizationEndpoint: issuer + "/oauth/authorize",
		TokenEndpoint:         issuer + "/oauth/token",
		JWKSURI:               issuer + "/oauth/jwks",
		ScopesSupported:       scopes,

		// Only support "code" response type (authorization code flow)
		ResponseTypesSupported: []string{"code"},

		// Support query response mode
		ResponseModesSupported: []string{"query"},

		// Support authorization_code and refresh_token grants
		GrantTypesSupported: []string{"authorization_code", "refresh_token"},

		// Support public clients (none) and confidential clients (client_secret_post)
		TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_post"},

		// Only support S256 PKCE (required by MCP spec)
		CodeChallengeMethodsSupported: []string{"S256"},
	}

	if allowDynamicRegistration {
		m.RegistrationEndpoint = issuer + "/oauth/register"
	}

	return m
}

// HandleMetadata returns an HTTP handler for the metadata endpoint.
func HandleMetadata(metadata *Metadata) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")

		if err := json.NewEncoder(w).Encode(metadata); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
}
