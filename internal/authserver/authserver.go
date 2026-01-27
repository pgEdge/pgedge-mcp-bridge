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
	"fmt"
	"net/http"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
)

// Server is the OAuth 2.0 Authorization Server.
type Server struct {
	cfg              *config.OAuthServerConfig
	storage          Storage
	tokenIssuer      *TokenIssuer
	authenticator    UserAuthenticator
	metadata         *Metadata
	authorizeHandler *AuthorizeHandler
	tokenHandler     *TokenHandler
	logger           *logging.Logger
}

// New creates a new OAuth authorization server.
func New(cfg *config.OAuthServerConfig, logger *logging.Logger) (*Server, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Create storage (in-memory for now)
	storage := NewMemoryStorage(5 * time.Minute)

	// Create token issuer
	var keyFile string
	var keyID string
	var generateKey bool
	algorithm := "RS256"

	if cfg.Signing != nil {
		keyFile = cfg.Signing.KeyFile
		keyID = cfg.Signing.KeyID
		generateKey = cfg.Signing.GenerateKey
		if cfg.Signing.Algorithm != "" {
			algorithm = cfg.Signing.Algorithm
		}
	}

	tokenIssuer, err := NewTokenIssuer(cfg.Issuer, algorithm, keyFile, keyID, generateKey)
	if err != nil {
		return nil, fmt.Errorf("creating token issuer: %w", err)
	}

	// Create authenticator based on mode
	var authenticator UserAuthenticator
	switch cfg.Mode {
	case "builtin":
		if cfg.BuiltIn == nil {
			return nil, fmt.Errorf("builtin configuration required for builtin mode")
		}
		authenticator, err = NewBuiltInAuthenticator(cfg.BuiltIn)
		if err != nil {
			return nil, fmt.Errorf("creating builtin authenticator: %w", err)
		}
	case "federated":
		// TODO: Implement federated authenticator
		return nil, fmt.Errorf("federated mode not yet implemented")
	default:
		return nil, fmt.Errorf("unknown mode: %s", cfg.Mode)
	}

	// Build metadata
	metadata := BuildMetadata(cfg.Issuer, cfg.ScopesSupported, cfg.AllowDynamicRegistration)

	// Create authorize handler
	var loginTemplate string
	if cfg.BuiltIn != nil {
		loginTemplate = cfg.BuiltIn.LoginTemplate
	}
	authorizeHandler, err := NewAuthorizeHandler(
		storage,
		authenticator,
		cfg.AllowedRedirectURIs,
		cfg.AuthCodeLifetime,
		cfg.ScopesSupported,
		loginTemplate,
	)
	if err != nil {
		return nil, fmt.Errorf("creating authorize handler: %w", err)
	}

	// Create token handler
	tokenHandler := NewTokenHandler(
		storage,
		tokenIssuer,
		cfg.Issuer,
		cfg.TokenLifetime,
		cfg.RefreshTokenLifetime,
	)

	return &Server{
		cfg:              cfg,
		storage:          storage,
		tokenIssuer:      tokenIssuer,
		authenticator:    authenticator,
		metadata:         metadata,
		authorizeHandler: authorizeHandler,
		tokenHandler:     tokenHandler,
		logger:           logger,
	}, nil
}

// RegisterRoutes registers OAuth endpoints on the given mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Discovery endpoint
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleMetadata)

	// JWKS endpoint
	mux.HandleFunc("GET /oauth/jwks", s.handleJWKS)

	// Authorization endpoint
	mux.Handle("GET /oauth/authorize", s.authorizeHandler)
	mux.Handle("POST /oauth/authorize", s.authorizeHandler)

	// Token endpoint
	mux.Handle("POST /oauth/token", s.tokenHandler)

	// Optional: Dynamic client registration
	if s.cfg.AllowDynamicRegistration {
		mux.HandleFunc("POST /oauth/register", s.handleRegister)
	}

	s.logger.Info("OAuth server routes registered",
		"issuer", s.cfg.Issuer,
		"mode", s.cfg.Mode,
		"dynamic_registration", s.cfg.AllowDynamicRegistration,
	)
}

// handleMetadata serves the OAuth metadata document.
func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if err := json.NewEncoder(w).Encode(s.metadata); err != nil {
		s.logger.Error("failed to encode metadata", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleJWKS serves the JSON Web Key Set.
func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	jwks := s.tokenIssuer.JWKS()
	if err := json.NewEncoder(w).Encode(jwks); err != nil {
		s.logger.Error("failed to encode JWKS", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleRegister handles dynamic client registration.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, ErrInvalidRequest("invalid JSON body"))
		return
	}

	// Validate redirect URIs
	if len(req.RedirectURIs) == 0 {
		WriteJSONError(w, ErrInvalidRequest("redirect_uris is required"))
		return
	}

	// Generate client credentials
	clientID := GenerateClientID()
	clientSecret := ""
	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "none" // Default to public client
	}
	if authMethod != "none" {
		clientSecret = GenerateClientSecret()
	}

	// Create client
	client := &Client{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethod: authMethod,
		CreatedAt:               time.Now(),
	}

	// Store client
	if err := s.storage.StoreClient(r.Context(), client); err != nil {
		s.logger.Error("failed to store client", "error", err)
		WriteJSONError(w, ErrServerError("failed to register client"))
		return
	}

	// Build response
	response := struct {
		ClientID                string   `json:"client_id"`
		ClientSecret            string   `json:"client_secret,omitempty"`
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name,omitempty"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
	}{
		ClientID:                client.ClientID,
		ClientSecret:            clientSecret, // Only include if generated
		RedirectURIs:            client.RedirectURIs,
		ClientName:              client.ClientName,
		TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
		GrantTypes:              client.GrantTypes,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode registration response", "error", err)
	}
}

// Close releases resources used by the server.
func (s *Server) Close() error {
	if s.storage != nil {
		return s.storage.Close()
	}
	return nil
}

// Metadata returns the server's OAuth metadata.
func (s *Server) Metadata() *Metadata {
	return s.metadata
}

// JWKS returns the server's JSON Web Key Set.
func (s *Server) JWKS() interface{} {
	return s.tokenIssuer.JWKS()
}
