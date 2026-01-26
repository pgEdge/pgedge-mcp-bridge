# Changelog

All notable changes to the pgEdge MCP Bridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
