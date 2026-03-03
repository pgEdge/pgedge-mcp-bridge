# Changelog

All notable changes to the pgEdge MCP Bridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
