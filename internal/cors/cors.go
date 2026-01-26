/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package cors provides CORS (Cross-Origin Resource Sharing) handling for HTTP servers.
package cors

import (
	"net/http"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"github.com/rs/cors"
)

// Handler wraps the rs/cors library to provide CORS handling functionality.
// It allows configuring allowed origins, methods, headers, and other CORS options.
type Handler struct {
	cors    *cors.Cors
	enabled bool
}

// NewHandler creates a new CORS handler based on the provided configuration.
// If the configuration is nil or CORS is disabled, the handler will act as
// a passthrough that does not modify requests or responses.
func NewHandler(cfg *config.CORSConfig) *Handler {
	if cfg == nil || !cfg.Enabled {
		return &Handler{
			cors:    nil,
			enabled: false,
		}
	}

	// Build CORS options from configuration
	options := cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   cfg.AllowedMethods,
		AllowedHeaders:   cfg.AllowedHeaders,
		ExposedHeaders:   cfg.ExposedHeaders,
		AllowCredentials: cfg.AllowCredentials,
		MaxAge:           cfg.MaxAge,
	}

	// Apply sensible defaults if not specified
	if len(options.AllowedMethods) == 0 {
		options.AllowedMethods = []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		}
	}

	if len(options.AllowedHeaders) == 0 {
		options.AllowedHeaders = []string{
			"Accept",
			"Authorization",
			"Content-Type",
			"X-Requested-With",
		}
	}

	return &Handler{
		cors:    cors.New(options),
		enabled: true,
	}
}

// Wrap wraps an http.Handler with CORS handling.
// If CORS is not enabled, the original handler is returned unchanged.
func (h *Handler) Wrap(handler http.Handler) http.Handler {
	if !h.enabled || h.cors == nil {
		return handler
	}
	return h.cors.Handler(handler)
}

// IsEnabled returns whether CORS handling is enabled.
func (h *Handler) IsEnabled() bool {
	return h.enabled
}

// ServeHTTP implements the http.Handler interface, allowing the CORS handler
// to be used directly in middleware chains.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if !h.enabled || h.cors == nil {
		next(w, r)
		return
	}
	h.cors.ServeHTTP(w, r, next)
}
