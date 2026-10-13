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
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// storeTestClient registers a public client with the given redirect URIs.
func storeTestClient(t *testing.T, storage Storage, clientID string, redirectURIs ...string) {
	t.Helper()
	client := &Client{
		ClientID:                clientID,
		RedirectURIs:            redirectURIs,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethod: "none",
		CreatedAt:               time.Now(),
	}
	if err := storage.StoreClient(context.Background(), client); err != nil {
		t.Fatalf("failed to store client: %v", err)
	}
}

func authorizeQuery(clientID, redirectURI string) string {
	return "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {validCodeChallenge()},
		"code_challenge_method": {"S256"},
		"state":                 {"teststate"},
	}.Encode()
}

// assertJSONError checks that the response is a JSON error with the given
// code, rather than a redirect to the client.
func assertJSONError(t *testing.T, w *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if w.Code == http.StatusFound {
		t.Fatalf("expected no redirect, got 302 to %s", w.Header().Get("Location"))
	}
	var errResp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("expected JSON error body: %v", err)
	}
	if errResp["error"] != wantCode {
		t.Errorf("expected error %q, got %q (%s)", wantCode, errResp["error"], errResp["error_description"])
	}
}

func TestHandleRegister_RejectsUnlistedRedirectURI(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	for _, uris := range []string{
		`["https://evil.example/callback"]`,
		`["https://example.com/callback","https://evil.example/callback"]`,
	} {
		body := `{"redirect_uris":` + uris + `}`
		req := httptest.NewRequest("POST", "/register", strings.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", uris, w.Code, w.Body.String())
		}
		assertJSONError(t, w, ErrorInvalidRedirectURI)
	}
}

func TestAuthorizeHandler_RegisteredClient_RedirectURIBinding(t *testing.T) {
	tests := []struct {
		name        string
		redirectURI string
		wantOK      bool
	}{
		{"registered URI", "https://example.com/callback", true},
		{"allow-listed but not registered", "http://localhost:8080/callback", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestAuthorizeHandler(t)
			storeTestClient(t, h.storage, "reg-client", "https://example.com/callback")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", authorizeQuery("reg-client", tt.redirectURI), nil))

			if tt.wantOK {
				if w.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
				}
				return
			}
			assertJSONError(t, w, ErrorInvalidRequest)
		})
	}
}

func TestAuthorizeHandler_RegisteredLoopbackClient_AnyPort(t *testing.T) {
	h := newTestAuthorizeHandler(t)
	storeTestClient(t, h.storage, "cli-client", "http://localhost:8080/callback")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", authorizeQuery("cli-client", "http://127.0.0.1:53682/callback"), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthorizeHandler_RegisteredClient_ErrorNotRedirectedToForeignURI(t *testing.T) {
	h := newTestAuthorizeHandler(t)
	storeTestClient(t, h.storage, "reg-client", "https://example.com/callback")

	// Missing response_type, with an allow-listed URI the client never
	// registered: the error must not be redirected there.
	q := url.Values{
		"client_id":    {"reg-client"},
		"redirect_uri": {"http://localhost:8080/callback"},
	}.Encode()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/oauth/authorize?"+q, nil))

	assertJSONError(t, w, ErrorUnsupportedResponseType)
}

func TestAuthorizeHandler_UnregisteredClient(t *testing.T) {
	for _, require := range []bool{false, true} {
		h := newTestAuthorizeHandler(t)
		h.requireRegisteredClients = require

		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", authorizeQuery("unknown-client", "https://example.com/callback"), nil))

		if !require {
			if w.Code != http.StatusOK {
				t.Fatalf("require=false: expected 200, got %d: %s", w.Code, w.Body.String())
			}
			continue
		}
		assertJSONError(t, w, ErrorInvalidRequest)
	}
}

func TestAuthorizeHandler_HandlePost_RegisteredClient_UnregisteredRedirect(t *testing.T) {
	h := newTestAuthorizeHandler(t)
	storeTestClient(t, h.storage, "reg-client", "https://example.com/callback")

	csrfToken, err := h.generateCSRFToken()
	if err != nil {
		t.Fatalf("failed to generate CSRF token: %v", err)
	}
	formData := url.Values{
		"csrf_token":            {csrfToken},
		"username":              {"testuser"},
		"password":              {"validpass"},
		"response_type":         {"code"},
		"client_id":             {"reg-client"},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"code_challenge":        {validCodeChallenge()},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assertJSONError(t, w, ErrorInvalidRequest)
}

func TestFederatedAuthorizeHandler_ClientBinding(t *testing.T) {
	mockServer := newFederatedMockServer(t)
	defer mockServer.Close()

	t.Run("registered client, unregistered redirect", func(t *testing.T) {
		h := newTestFederatedHandler2(t, mockServer.URL)
		storeTestClient(t, h.storage, "reg-client", "https://other.example/callback")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", authorizeQuery("reg-client", "https://example.com/callback"), nil))

		assertJSONError(t, w, ErrorInvalidRequest)
	})

	t.Run("unregistered client when required", func(t *testing.T) {
		h := newTestFederatedHandler2(t, mockServer.URL)
		h.requireRegisteredClients = true

		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", authorizeQuery("unknown-client", "https://example.com/callback"), nil))

		assertJSONError(t, w, ErrorInvalidRequest)
	})

	t.Run("registered client, registered redirect", func(t *testing.T) {
		h := newTestFederatedHandler2(t, mockServer.URL)
		h.requireRegisteredClients = true
		storeTestClient(t, h.storage, "reg-client", "https://example.com/callback")

		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", authorizeQuery("reg-client", "https://example.com/callback"), nil))

		if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), mockServer.URL) {
			t.Fatalf("expected redirect to upstream IdP, got %d to %s", w.Code, w.Header().Get("Location"))
		}
	})
}

func TestTokenHandler_RequireRegisteredClients(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	if !server.tokenHandler.requireRegisteredClients {
		t.Fatal("expected token handler to require registered clients when dynamic registration is enabled")
	}
	if !server.builtinAuthorizeHandler.requireRegisteredClients {
		t.Fatal("expected authorize handler to require registered clients when dynamic registration is enabled")
	}

	oauthErr := server.tokenHandler.validateClientAuth(context.Background(), "unknown-client", "")
	if oauthErr == nil || oauthErr.Code != ErrorInvalidClient {
		t.Fatalf("expected invalid_client for unregistered client, got %v", oauthErr)
	}
}
