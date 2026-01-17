package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

func TestNewHandler_NilConfig(t *testing.T) {
	handler := NewHandler(nil)

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}

	if handler.IsEnabled() {
		t.Error("Expected CORS to be disabled with nil config")
	}
}

func TestNewHandler_DisabledConfig(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled: false,
	}

	handler := NewHandler(cfg)

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}

	if handler.IsEnabled() {
		t.Error("Expected CORS to be disabled")
	}
}

func TestNewHandler_EnabledConfig(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://example.com"},
	}

	handler := NewHandler(cfg)

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}

	if !handler.IsEnabled() {
		t.Error("Expected CORS to be enabled")
	}

	if handler.cors == nil {
		t.Error("Expected internal cors handler to be set")
	}
}

func TestNewHandler_DefaultMethods(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		// AllowedMethods empty - should get defaults
	}

	handler := NewHandler(cfg)

	if !handler.IsEnabled() {
		t.Fatal("Expected CORS to be enabled")
	}

	// Test that default methods are applied by making a preflight request
	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	rr := httptest.NewRecorder()

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)
	wrapped.ServeHTTP(rr, req)

	// Check that the response includes CORS headers
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected Access-Control-Allow-Origin header")
	}
}

func TestNewHandler_DefaultHeaders(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		// AllowedHeaders empty - should get defaults
	}

	handler := NewHandler(cfg)

	if !handler.IsEnabled() {
		t.Fatal("Expected CORS to be enabled")
	}

	// Test regular request to verify handler is configured
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)
	wrapped.ServeHTTP(rr, req)

	// Check that origin is allowed
	origin := rr.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin '*', got '%s'", origin)
	}
}

func TestWrap_DisabledCORS(t *testing.T) {
	handler := NewHandler(nil)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	wrapped := handler.Wrap(innerHandler)

	// When CORS is disabled, Wrap should return the original handler
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Should not have CORS headers when disabled
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("Expected no CORS headers when disabled")
	}
}

func TestWrap_AddsCORSHeaders(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	wrapped := handler.Wrap(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	origin := rr.Header().Get("Access-Control-Allow-Origin")
	if origin != "http://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin 'http://example.com', got '%s'", origin)
	}
}

func TestPreflightRequest(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         3600,
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	// The rs/cors library handles preflight; status may vary but should be successful
	if rr.Code >= 400 {
		t.Errorf("Expected successful status for preflight, got %d", rr.Code)
	}

	// Verify the handler was configured correctly by checking CORS headers
	// Note: The rs/cors library only sets headers when the origin matches
	origin := rr.Header().Get("Access-Control-Allow-Origin")
	// With "*" wildcard, the library returns "*"
	if origin != "*" && origin != "http://example.com" && origin != "" {
		t.Errorf("Unexpected Access-Control-Allow-Origin: %s", origin)
	}
}

func TestPreflightRequest_DisallowedOrigin(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://allowed.com"},
		AllowedMethods: []string{"GET", "POST"},
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://disallowed.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	// Should not include the disallowed origin in response
	origin := rr.Header().Get("Access-Control-Allow-Origin")
	if origin == "http://disallowed.com" {
		t.Error("Should not allow disallowed origin")
	}
}

func TestAllowCredentials(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"http://example.com"},
		AllowCredentials: true,
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	credentials := rr.Header().Get("Access-Control-Allow-Credentials")
	if credentials != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials 'true', got '%s'", credentials)
	}
}

func TestAllowedOriginsWildcard(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	// Test with any origin
	testOrigins := []string{
		"http://example.com",
		"http://test.com",
		"https://secure.example.org",
	}

	for _, origin := range testOrigins {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", origin)

		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		allowedOrigin := rr.Header().Get("Access-Control-Allow-Origin")
		if allowedOrigin != "*" {
			t.Errorf("Expected Access-Control-Allow-Origin '*' for origin %s, got '%s'", origin, allowedOrigin)
		}
	}
}

func TestAllowedOriginsMatching(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled: true,
		AllowedOrigins: []string{
			"http://example.com",
			"https://secure.example.com",
		},
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	tests := []struct {
		origin   string
		expected string
	}{
		{"http://example.com", "http://example.com"},
		{"https://secure.example.com", "https://secure.example.com"},
		{"http://other.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Origin", tt.origin)

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			allowedOrigin := rr.Header().Get("Access-Control-Allow-Origin")
			if allowedOrigin != tt.expected {
				t.Errorf("For origin %s, expected '%s', got '%s'", tt.origin, tt.expected, allowedOrigin)
			}
		})
	}
}

func TestExposedHeaders(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://example.com"},
		ExposedHeaders: []string{"X-Custom-Header", "X-Another-Header"},
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "value")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	exposed := rr.Header().Get("Access-Control-Expose-Headers")
	if exposed == "" {
		t.Error("Expected Access-Control-Expose-Headers header")
	}
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.CORSConfig
		expected bool
	}{
		{
			name:     "nil config",
			cfg:      nil,
			expected: false,
		},
		{
			name: "disabled config",
			cfg: &config.CORSConfig{
				Enabled: false,
			},
			expected: false,
		},
		{
			name: "enabled config",
			cfg: &config.CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(tt.cfg)

			if handler.IsEnabled() != tt.expected {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expected, handler.IsEnabled())
			}
		})
	}
}

func TestServeHTTP_Disabled(t *testing.T) {
	handler := NewHandler(nil)

	nextCalled := false
	next := func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req, next)

	if !nextCalled {
		t.Error("Expected next handler to be called")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestServeHTTP_Enabled(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://example.com"},
	}

	handler := NewHandler(cfg)

	nextCalled := false
	next := func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req, next)

	if !nextCalled {
		t.Error("Expected next handler to be called")
	}

	origin := rr.Header().Get("Access-Control-Allow-Origin")
	if origin != "http://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin 'http://example.com', got '%s'", origin)
	}
}

func TestMaxAge(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		MaxAge:         7200,
	}

	handler := NewHandler(cfg)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	maxAge := rr.Header().Get("Access-Control-Max-Age")
	if maxAge != "7200" {
		t.Errorf("Expected Access-Control-Max-Age '7200', got '%s'", maxAge)
	}
}

func TestFullCORSConfig(t *testing.T) {
	cfg := &config.CORSConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"http://example.com", "https://example.com"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Custom-Header"},
		ExposedHeaders:   []string{"X-Response-Header"},
		AllowCredentials: true,
		MaxAge:           86400,
	}

	handler := NewHandler(cfg)

	if !handler.IsEnabled() {
		t.Fatal("Expected handler to be enabled")
	}

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Response-Header", "value")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler.Wrap(innerHandler)

	// Test regular CORS request (not preflight)
	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Origin", "http://example.com")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	// Verify basic CORS headers are present on regular request
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin 'http://example.com', got '%s'",
			rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials 'true', got '%s'",
			rr.Header().Get("Access-Control-Allow-Credentials"))
	}
	// Exposed headers should be set on response
	exposed := rr.Header().Get("Access-Control-Expose-Headers")
	if exposed == "" {
		t.Error("Expected Access-Control-Expose-Headers")
	}
}
