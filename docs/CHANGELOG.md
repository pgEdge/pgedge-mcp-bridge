# Changelog

All notable changes to the pgEdge MCP Bridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **Security**: Builds now require Go 1.26.10, which adds eight compromised eMudhra intermediate CAs to the `crypto/x509` denylist (CVE-2026-111943)
- **Security**: Redirect URI validation now identifies loopback URIs by their parsed host rather than a string prefix, so a URI such as `http://localhost:@attacker.example/callback` (where `localhost:` is userinfo) can no longer pass as localhost and receive the authorization code. Port-agnostic loopback matching now also requires the query and the encoded path to match exactly, and no longer applies to URIs with userinfo or a fragment; `[::1]` is now recognised as a loopback host ([#5](https://github.com/pgEdge/pgedge-mcp-bridge/issues/5))
- **Examples**: The OAuth example configuration listed `http://localhost:*/callback`, which failed configuration validation at startup because `*` is not a valid port; it now uses a concrete port, which matches any port on the loopback interface
- **Security**: Authorization requests are now bound to registered clients. Dynamic client registration rejects `redirect_uris` that `allowed_redirect_uris` does not allow (`invalid_redirect_uri`), a registered client may only use the redirect URIs it registered, and when `allow_dynamic_registration` is enabled the authorization and token endpoints reject unregistered `client_id`s. Registrations are held in memory, so clients must register again after a restart ([#7](https://github.com/pgEdge/pgedge-mcp-bridge/issues/7))

## [1.0.0-beta1] - 2026-06-26

### Added

- **Session pool observability**: The `/health` endpoint now reports `active_sessions` and `max_sessions` so the session pool can be monitored without restarting the process, and every session eviction is logged at INFO level with the session ID and a reason (`idle-timeout` or `explicit-delete`)

### Changed

- **Dependencies**: Updated `go-jose` and the documentation toolchain (`urllib3`, `requests`, `idna`, `Pygments`, `pymdown-extensions`) to their latest patched releases, clearing all outstanding security advisories

### Fixed

- **Session management**: Idle MCP sessions are now evicted once they have been inactive for longer than `session.timeout`, freeing their slot against `session.max_sessions`. Previously the idle reaper only started when `session.enabled` was explicitly set to `true`, so a configuration that omitted that flag would let the session pool fill to `max_sessions` and stay full until the process was restarted, rejecting every new client with `maximum sessions reached`
- **Client mode**: JSON-RPC notifications (requests without an `id`) no longer produce stdout output, even when the upstream server emits a response body for them. Per JSON-RPC 2.0 §4.1 notifications must not receive a response; forwarding one tripped strict client-side validators such as Claude Desktop's MCP SDK

[1.0.0-beta1]: https://github.com/pgEdge/pgedge-mcp-bridge/releases/tag/v1.0.0-beta1

## [1.0.0-alpha6] - 2026-03-31

### Added

- **Customisable OAuth login page**: Support for custom login page templates with configurable title, CSS stylesheet URL, and logo, or a fully custom HTML login page served from a user-specified path
- **MCP Streamable HTTP transport**: Support for the Streamable HTTP transport protocol, where POST requests with `Accept: text/event-stream` receive SSE-wrapped responses
- **Configurable timeouts**: New timeout settings for SSE keepalive (`server.sse_keepalive_interval`), MCP subprocess reads (`server.mcp_server.read_timeout`), OAuth HTTP requests (`auth.oauth.http_timeout`), and federated OAuth HTTP requests (`oauth_server.federated.http_timeout`), all defaulting to 30s

### Changed

- **Code quality**: Cleanup and refactoring across the codebase
- **Test coverage**: Improved test coverage
- **Documentation**: Expanded README and fixed documentation examples

### Fixed

- **Security**: Redirect URI validation now requires exact path matching for localhost URIs, preventing open redirect attacks
- **Security**: RSA signature verification now uses the correct hash algorithm for RS384 and RS512 (previously always used SHA256)
- **Security**: Bearer token validation now holds a read lock during concurrent access, preventing data races
- **Security**: Cryptographic random number generation now panics on failure instead of silently producing zero values
- **Security**: Registration endpoint request body is now limited to 64KB to prevent memory exhaustion
- **Security**: JWT verification error details are no longer leaked to clients
- **Reliability**: Authorization code and refresh token deletion errors are now logged instead of silently discarded
- **Reliability**: Federated authorization handler cleanup goroutine now properly shuts down on Close()
- **TLS**: A warning is now logged when `insecure_skip_verify` is enabled

[1.0.0-alpha6]: https://github.com/pgEdge/pgedge-mcp-bridge/releases/tag/v1.0.0-alpha6

## [1.0.0-alpha4] - 2026-03-03

### Added

- **RFC 9728 Protected Resource Metadata**: New `/.well-known/oauth-protected-resource` endpoint for OAuth resource discovery, required by Claude's connection flow
- **Root-path MCP endpoint**: MCP handlers now accept requests at `/` in addition to `/mcp`, for compatibility with clients that POST directly to the server URL
- **Root-level registration endpoint**: Dynamic client registration now served at `/register` in addition to `/oauth/register`

### Changed

- **Registration endpoint**: Metadata document now advertises `/register` as the canonical registration endpoint
- **Auth middleware**: Added `/oauth/callback`, `/.well-known/oauth-protected-resource`, and `/register` to auth skip paths

[1.0.0-alpha4]: https://github.com/pgEdge/pgedge-mcp-bridge/releases/tag/v1.0.0-alpha4

## [1.0.0-alpha3] - 2026-01-27

### Added

- **OAuth Authorization Server**: Built-in OAuth 2.0 Authorization Server for direct Claude Desktop integration
  - Implements OAuth 2.0 Authorization Code flow with PKCE (RFC 7636)
  - Publishes OAuth metadata at `/.well-known/oauth-authorization-server` (RFC 8414)
  - JWKS endpoint at `/oauth/jwks` for token verification
  - Built-in mode with local user management and bcrypt password hashing
  - Federated mode for delegating authentication to upstream IdPs (Google, Okta, etc.)
  - Dynamic client registration support (RFC 7591)
  - JWT access tokens with RS256 or ES256 signing
  - Refresh token rotation for long-lived sessions
- **New Endpoints**:
  - `GET /ready` - Readiness check endpoint (verifies MCP subprocess is running)
  - `GET /.well-known/oauth-authorization-server` - OAuth server metadata
  - `GET /oauth/jwks` - JSON Web Key Set
  - `GET/POST /oauth/authorize` - Authorization endpoint
  - `POST /oauth/token` - Token endpoint
  - `POST /oauth/register` - Dynamic client registration

### Changed

- **Configuration**: Updated config file search order to `/etc/pgedge/config.yaml` then executable directory
- **Documentation**: Comprehensive updates to reflect OAuth server capabilities and correct configuration field names

[1.0.0-alpha3]: https://github.com/pgEdge/pgedge-mcp-bridge/releases/tag/v1.0.0-alpha3

## [1.0.0-alpha2] - 2026-01-26

### Added

- **Licence**: Added The PostgreSQL License file and documentation page
- **Copyright Headers**: Added standard pgEdge copyright headers to all Go source files

[1.0.0-alpha2]: https://github.com/pgEdge/pgedge-mcp-bridge/releases/tag/v1.0.0-alpha2

## [1.0.0-alpha1] - 2026-01-26

### Added

- **Bidirectional Transport Bridge**: Bridge between HTTP/SSE and stdio transports for the Model Context Protocol (MCP)
- **Server Mode**: Wrap local stdio-based MCP servers and expose them over HTTP
  - JSON-RPC 2.0 over HTTP POST for requests/responses
  - Server-Sent Events (SSE) for server-initiated notifications
  - Configurable base path for API endpoints
  - Health check endpoint
- **Client Mode**: Connect to remote HTTP MCP servers and expose via stdio
  - Automatic SSE reconnection with configurable retry logic
  - Configurable timeouts and connection pooling
- **Authentication**:
  - Bearer token authentication (static tokens or validation endpoint)
  - OAuth 2.0/OIDC support with JWKS validation
  - Token introspection support
- **TLS/HTTPS**:
  - Server-side TLS with configurable certificates
  - Client-side TLS with CA certificate verification
  - Mutual TLS (mTLS) support for client certificate authentication
- **Session Management**:
  - Stateful sessions with automatic timeout and cleanup
  - Configurable maximum concurrent sessions
  - Session affinity via `Mcp-Session-Id` header
- **Process Management**:
  - Automatic subprocess restart on failure
  - Configurable restart delays and maximum attempts
  - Graceful shutdown handling
- **CORS Support**: Configurable cross-origin resource sharing for browser clients
- **Logging**: Structured logging with configurable levels and JSON output format
- **Configuration**: YAML-based configuration with environment variable substitution

### Documentation

- Comprehensive documentation site using MkDocs with Material theme
- Installation guide for binaries and building from source
- Server mode and client mode configuration guides
- Authentication and TLS configuration guides
- Complete configuration reference
- CLI and HTTP API references

[1.0.0-alpha1]: https://github.com/pgEdge/pgedge-mcp-bridge/releases/tag/v1.0.0-alpha1
