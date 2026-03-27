# Configuration Reference

This document provides a complete reference for all configuration options in the MCP Bridge.

## Configuration File

The bridge reads configuration from a YAML file. If not specified on the command line, it searches for `config.yaml` in:

1. `/etc/pgedge/config.yaml`
2. Directory containing the executable

Specify a custom path with the `-c` flag:

```bash
mcp-bridge -c /path/to/config.yaml
```

## Environment Variable Substitution

Configuration files support environment variable substitution using `${VAR}` or `$VAR` syntax:

```yaml
auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"
```

## Global Options

### mode

**Required.** Operating mode of the bridge.

| Value | Description |
|-------|-------------|
| `server` | Wrap a local MCP server and expose over HTTP |
| `client` | Connect to a remote HTTP MCP server and expose via stdio |

```yaml
mode: server
```

## Logging Configuration

### log.level

Log level for output messages.

| Value | Description |
|-------|-------------|
| `debug` | Verbose debugging information |
| `info` | General operational information (default) |
| `warn` | Warning messages |
| `error` | Error messages only |

### log.format

Output format for log messages.

| Value | Description |
|-------|-------------|
| `text` | Human-readable text format |
| `json` | Structured JSON format |

### log.output

Log output destination.

| Value | Description |
|-------|-------------|
| `stderr` | Standard error (default) |
| `stdout` | Standard output |
| `/path/to/file` | Write to file |

```yaml
log:
  level: info
  format: json
  output: stderr
```

## Server Configuration

Server mode configuration options. Required when `mode: server`.

### server.listen

**Required.** Address and port to listen on.

```yaml
server:
  listen: ":8080"           # All interfaces, port 8080
  listen: "127.0.0.1:8080"  # Localhost only
  listen: "0.0.0.0:443"     # All interfaces, port 443
```

### server.read_timeout

Maximum duration for reading the entire request. Default: `30s`

### server.write_timeout

Maximum duration for writing the response. Default: `60s`

### server.idle_timeout

Maximum duration to wait for the next request on keep-alive connections. Default: `120s`

### server.sse_keepalive_interval

Interval for sending keepalive pings to SSE (Server-Sent Events) clients. A shorter interval helps detect broken connections faster. Default: `30s`

```yaml
server:
  listen: ":8080"
  read_timeout: 30s
  write_timeout: 60s
  idle_timeout: 120s
  sse_keepalive_interval: 30s
```

## MCP Server Configuration

Configuration for the MCP subprocess. Nested under `server:`. Required when `mode: server`.

### server.mcp_server.command

**Required.** Command to execute the MCP server.

### server.mcp_server.args

Arguments to pass to the command.

### server.mcp_server.dir

Working directory for the subprocess.

### server.mcp_server.env

Environment variables for the subprocess.

```yaml
server:
  mcp_server:
    command: "python"
    args:
      - "-m"
      - "mcp_server"
    dir: "/opt/mcp-server"
    env:
      PYTHONUNBUFFERED: "1"
      LOG_LEVEL: "info"
```

### server.mcp_server.read_timeout

Maximum time to wait for a response from the MCP subprocess. If a request to the subprocess takes longer than this, a timeout error is returned. Default: `30s`

### server.mcp_server.graceful_shutdown_timeout

Maximum time to wait for graceful shutdown. Default: `30s`

### server.mcp_server.restart_on_failure

Automatically restart the subprocess if it exits unexpectedly. Default: `false`

### server.mcp_server.max_restarts

Maximum number of restart attempts. Default: `5`

### server.mcp_server.restart_delay

Delay between restart attempts. Default: `5s`

```yaml
server:
  mcp_server:
    command: "python"
    args: ["-m", "mcp_server"]
    read_timeout: 30s
    graceful_shutdown_timeout: 30s
    restart_on_failure: true
    max_restarts: 5
    restart_delay: 5s
```

## Client Configuration

Client mode configuration options. Required when `mode: client`.

### client.url

**Required.** URL of the remote MCP HTTP server.

```yaml
client:
  url: "https://mcp.example.com/mcp"
```

### client.timeout

Request timeout. Default: `30s`

### client.max_idle_conns

Maximum idle connections in the pool. Default: `10`

### client.idle_conn_timeout

Idle connection timeout. Default: `90s`

```yaml
client:
  url: "https://mcp.example.com/mcp"
  timeout: 30s
  max_idle_conns: 10
  idle_conn_timeout: 90s
```

## Retry Configuration

Retry settings for client mode. Nested under `client:`.

### client.retry.enabled

Enable automatic retry on failure. Default: `false`

### client.retry.max_retries

Maximum retry attempts. Default: `3`

### client.retry.initial_delay

Initial delay between retries. Default: `100ms`

### client.retry.max_delay

Maximum delay between retries. Default: `5s`

### client.retry.multiplier

Backoff multiplier. Default: `2.0`

```yaml
client:
  retry:
    enabled: true
    max_retries: 5
    initial_delay: 1s
    max_delay: 30s
    multiplier: 2.0
```

## Session Configuration

Session management settings. Nested under `server:`.

### server.session.enabled

Enable session support. Default: `true`

### server.session.timeout

Session inactivity timeout. Default: `30m`

### server.session.max_sessions

Maximum concurrent sessions. Default: `100`

### server.session.cleanup_interval

Interval for cleaning up expired sessions. Default: `5m`

```yaml
server:
  session:
    enabled: true
    timeout: 30m
    max_sessions: 100
    cleanup_interval: 5m
```

## Authentication Configuration

### auth.type

Authentication type.

| Value | Description |
|-------|-------------|
| `none` | No authentication |
| `bearer` | Bearer token authentication |
| `oauth` | OAuth 2.0/OIDC authentication |

### Bearer Authentication

#### Server Mode (Validating)

```yaml
server:
  auth:
    type: bearer
    bearer:
      valid_tokens:
        - "token1"
        - "token2"
      validation_endpoint: "https://auth.example.com/validate"  # Optional
```

#### Client Mode (Sending)

```yaml
client:
  auth:
    type: bearer
    bearer:
      token: "${MCP_AUTH_TOKEN}"
```

### OAuth Authentication

#### Server Mode (Validating)

```yaml
server:
  auth:
    type: oauth
    oauth:
      discovery_url: "https://auth.example.com"
      jwks_url: "https://auth.example.com/.well-known/jwks.json"
      introspection_url: "https://auth.example.com/introspect"
      scopes:
        - "mcp:read"
```

#### auth.oauth.http_timeout

Timeout for HTTP requests made during OAuth token operations (discovery, token exchange, introspection). Default: `30s`

#### Client Mode (Obtaining)

```yaml
client:
  auth:
    type: oauth
    oauth:
      discovery_url: "https://auth.example.com"
      token_url: "https://auth.example.com/oauth/token"
      client_id: "my-client"
      client_secret: "${OAUTH_CLIENT_SECRET}"
      scopes:
        - "mcp:read"
      use_pkce: true
      resource: "urn:mcp-server"
      http_timeout: 30s
```

## TLS Configuration

### Server TLS

```yaml
server:
  tls:
    enabled: true
    cert_file: "/path/to/server.crt"
    key_file: "/path/to/server.key"
    client_ca: "/path/to/client-ca.crt"
    client_auth: "verify"  # none, request, require, verify
    min_version: "1.2"
    max_version: "1.3"
```

### Client TLS

```yaml
client:
  tls:
    ca_cert: "/path/to/ca.crt"
    cert_file: "/path/to/client.crt"
    key_file: "/path/to/client.key"
    insecure_skip_verify: false
    server_name: "mcp.example.com"
```

## CORS Configuration

Cross-Origin Resource Sharing settings for browser clients.

### cors.enabled

Enable CORS support. Default: `false`

### cors.allowed_origins

List of allowed origins. Use `["*"]` for any origin.

### cors.allowed_methods

Allowed HTTP methods. Default: `["GET", "POST", "DELETE", "OPTIONS"]`

### cors.allowed_headers

Allowed request headers.

### cors.exposed_headers

Headers exposed to the client.

### cors.allow_credentials

Allow credentials (cookies, auth headers). Default: `false`

### cors.max_age

Preflight cache duration in seconds. Default: `86400`

```yaml
server:
  cors:
    enabled: true
    allowed_origins:
      - "https://app.example.com"
    allowed_methods:
      - "GET"
      - "POST"
      - "DELETE"
      - "OPTIONS"
    allowed_headers:
      - "Authorization"
      - "Content-Type"
      - "Mcp-Session-Id"
    exposed_headers:
      - "Mcp-Session-Id"
    allow_credentials: true
    max_age: 3600
```

## OAuth Authorization Server Configuration

The bridge can act as an OAuth 2.0 Authorization Server to issue access tokens. This enables direct integration with clients like Claude Desktop that expect OAuth authentication.

### oauth_server.enabled

Enable the OAuth authorization server. Default: `false`

### oauth_server.issuer

**Required when enabled.** The OAuth issuer URL (typically the bridge's external URL).

### oauth_server.mode

Authentication mode: `builtin` or `federated`. Default: `builtin`

| Value | Description |
|-------|-------------|
| `builtin` | Local user management with bcrypt passwords |
| `federated` | Delegate authentication to upstream IdP |

### oauth_server.token_lifetime

Access token validity duration. Default: `1h`

### oauth_server.refresh_token_lifetime

Refresh token validity duration. Default: `24h`

### oauth_server.auth_code_lifetime

Authorization code validity duration. Default: `10m`

### oauth_server.allowed_redirect_uris

List of allowed OAuth redirect URI patterns.

### oauth_server.scopes_supported

List of scopes this server supports.

### oauth_server.allow_dynamic_registration

Enable dynamic client registration endpoint (`/oauth/register`). Default: `false`

### Built-in Mode Configuration

For local user management:

```yaml
server:
  oauth_server:
    enabled: true
    issuer: "https://mcp.example.com"
    mode: builtin
    token_lifetime: 1h
    refresh_token_lifetime: 24h
    signing:
      algorithm: RS256
      key_file: "/etc/pgedge/jwt-private.pem"
    builtin:
      users:
        - username: admin
          password_hash: "$2a$10$..."  # bcrypt hash
          scopes: ["mcp:read", "mcp:write"]
    allowed_redirect_uris:
      - "https://claude.ai/api/mcp/auth_callback"
    scopes_supported:
      - "mcp:read"
      - "mcp:write"
```

#### oauth_server.signing

JWT signing configuration:

| Field | Description |
|-------|-------------|
| `algorithm` | Signing algorithm: `RS256` or `ES256` |
| `key_file` | Path to private key file (PEM format) |
| `key_id` | Optional key ID for JWKS |
| `generate_key` | Generate ephemeral key (dev mode only) |

#### oauth_server.builtin.users

List of users for built-in authentication:

| Field | Description |
|-------|-------------|
| `username` | User's login name |
| `password_hash` | bcrypt hash of password |
| `password_env` | Environment variable containing plaintext password (hashed at runtime) |
| `scopes` | List of scopes to grant this user |

### Federated Mode Configuration

For delegating to an upstream identity provider (Google, Okta, etc.):

```yaml
server:
  oauth_server:
    enabled: true
    issuer: "https://mcp.example.com"
    mode: federated
    token_lifetime: 1h
    signing:
      algorithm: RS256
      key_file: "/etc/pgedge/jwt-private.pem"
    federated:
      upstream_issuer: "https://accounts.google.com"
      client_id: "${GOOGLE_CLIENT_ID}"
      client_secret_env: "GOOGLE_CLIENT_SECRET"
      scopes: ["openid", "email", "profile"]
      allowed_domains:
        - "example.com"
      default_scopes: ["mcp:read"]
      admin_users:
        - "admin@example.com"
      admin_scopes: ["mcp:read", "mcp:write", "mcp:admin"]
    allowed_redirect_uris:
      - "https://claude.ai/api/mcp/auth_callback"
```

#### oauth_server.federated

Upstream IdP configuration:

| Field | Description |
|-------|-------------|
| `upstream_issuer` | Upstream IdP's issuer URL |
| `client_id` | Client ID registered with upstream |
| `client_secret` | Client secret (or use `client_secret_env`) |
| `client_secret_env` | Environment variable for client secret |
| `scopes` | Scopes to request from upstream |
| `allowed_domains` | Restrict authentication to specific email domains |
| `default_scopes` | Scopes to grant all authenticated users |
| `admin_users` | Users (by email/subject) to grant admin scopes |
| `admin_scopes` | Additional scopes for admin users |
| `http_timeout` | Timeout for HTTP requests to upstream IdP. Default: `30s` |

## Complete Examples

### Server Mode

```yaml
mode: server

server:
  listen: ":8080"
  read_timeout: 30s
  write_timeout: 60s
  idle_timeout: 120s

  mcp_server:
    command: "python"
    args: ["-m", "mcp_server"]
    dir: "/opt/mcp-server"
    env:
      PYTHONUNBUFFERED: "1"
    graceful_shutdown_timeout: 5s
    restart_on_failure: true
    max_restarts: 5

  auth:
    type: bearer
    bearer:
      valid_tokens:
        - "${MCP_AUTH_TOKEN}"

  session:
    enabled: true
    timeout: 30m
    max_sessions: 100

  cors:
    enabled: true
    allowed_origins:
      - "https://app.example.com"

log:
  level: info
  format: json
```

### Server Mode with OAuth Authorization Server

```yaml
mode: server

server:
  listen: ":8443"
  read_timeout: 30s
  write_timeout: 60s
  idle_timeout: 120s

  tls:
    enabled: true
    cert_file: "/etc/pgedge/server.crt"
    key_file: "/etc/pgedge/server.key"

  mcp_server:
    command: "python"
    args: ["-m", "mcp_server"]
    graceful_shutdown_timeout: 5s

  oauth_server:
    enabled: true
    issuer: "https://mcp.example.com"
    mode: builtin
    token_lifetime: 1h
    refresh_token_lifetime: 24h
    signing:
      algorithm: RS256
      key_file: "/etc/pgedge/jwt-private.pem"
    builtin:
      users:
        - username: admin
          password_env: "ADMIN_PASSWORD"
          scopes: ["mcp:read", "mcp:write"]
    allowed_redirect_uris:
      - "https://claude.ai/api/mcp/auth_callback"

  auth:
    type: oauth
    oauth:
      jwks_url: "https://mcp.example.com/oauth/jwks"

  session:
    enabled: true
    timeout: 30m

log:
  level: info
  format: json
```

### Client Mode

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"
  timeout: 30s

  auth:
    type: bearer
    bearer:
      token: "${MCP_AUTH_TOKEN}"

  retry:
    enabled: true
    max_retries: 5
    initial_delay: 1s
    max_delay: 30s

  tls:
    ca_cert: "/etc/ssl/certs/ca-certificates.crt"

log:
  level: info
  format: text
```
