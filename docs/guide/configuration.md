# Configuration Reference

This document provides a complete reference for all configuration options in the MCP Bridge.

## Configuration File

The bridge reads configuration from a YAML file. By default, it looks for `config.yaml` in the current directory or the directory containing the executable.

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

### logging.level

Log level for output messages.

| Value | Description |
|-------|-------------|
| `debug` | Verbose debugging information |
| `info` | General operational information (default) |
| `warn` | Warning messages |
| `error` | Error messages only |

### logging.format

Output format for log messages.

| Value | Description |
|-------|-------------|
| `text` | Human-readable text format |
| `json` | Structured JSON format |

### logging.output

Log output destination.

| Value | Description |
|-------|-------------|
| `stderr` | Standard error (default) |
| `stdout` | Standard output |
| `/path/to/file` | Write to file |

```yaml
logging:
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

### server.base_path

Base path for HTTP endpoints. Default: `/mcp`

```yaml
server:
  base_path: "/api/v1/mcp"
```

### server.read_timeout

Maximum duration for reading the entire request. Default: `30s`

### server.write_timeout

Maximum duration for writing the response. Default: `60s`

### server.idle_timeout

Maximum duration to wait for the next request on keep-alive connections. Default: `120s`

```yaml
server:
  listen: ":8080"
  read_timeout: 30s
  write_timeout: 60s
  idle_timeout: 120s
```

## MCP Server Configuration

Configuration for the MCP subprocess. Required when `mode: server`.

### mcp.command

**Required.** Command to execute the MCP server.

### mcp.args

Arguments to pass to the command.

### mcp.working_dir

Working directory for the subprocess.

### mcp.env

Environment variables for the subprocess.

```yaml
mcp:
  command: "python"
  args:
    - "-m"
    - "mcp_server"
  working_dir: "/opt/mcp-server"
  env:
    PYTHONUNBUFFERED: "1"
    LOG_LEVEL: "info"
```

### mcp.startup_timeout

Maximum time to wait for the MCP server to start. Default: `10s`

### mcp.shutdown_timeout

Maximum time to wait for graceful shutdown. Default: `5s`

### mcp.restart_on_failure

Automatically restart the subprocess if it exits unexpectedly. Default: `false`

### mcp.max_restarts

Maximum number of restart attempts. Default: `5`

### mcp.restart_delay

Delay between restart attempts. Default: `5s`

```yaml
mcp:
  command: "python"
  args: ["-m", "mcp_server"]
  startup_timeout: 10s
  shutdown_timeout: 5s
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

### client.connect_timeout

Connection establishment timeout. Default: `10s`

### client.keepalive_interval

SSE keepalive interval. Default: `30s`

### client.max_idle_conns

Maximum idle connections in the pool. Default: `10`

### client.idle_conn_timeout

Idle connection timeout. Default: `90s`

### client.max_response_size

Maximum response size in bytes. Default: `10485760` (10 MB)

```yaml
client:
  url: "https://mcp.example.com/mcp"
  timeout: 30s
  connect_timeout: 10s
  keepalive_interval: 30s
  max_idle_conns: 10
  idle_conn_timeout: 90s
  max_response_size: 10485760
```

## Retry Configuration

Retry settings for client mode.

### retry.enabled

Enable automatic retry on failure. Default: `false`

### retry.max_attempts

Maximum retry attempts. `0` for infinite. Default: `3`

### retry.initial_delay

Initial delay between retries. Default: `100ms`

### retry.max_delay

Maximum delay between retries. Default: `5s`

### retry.multiplier

Backoff multiplier. Default: `2.0`

### retry.jitter

Add random jitter to delays. Default: `true`

```yaml
retry:
  enabled: true
  max_attempts: 5
  initial_delay: 1s
  max_delay: 30s
  multiplier: 2.0
  jitter: true
```

## Session Configuration

Session management settings.

### session.enabled

Enable session support. Default: `true`

### session.timeout

Session inactivity timeout. Default: `30m`

### session.max_sessions

Maximum concurrent sessions. Default: `100`

### session.cleanup_interval

Interval for cleaning up expired sessions. Default: `5m`

### session.session_id

Fixed session ID (client mode only). Optional.

```yaml
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
auth:
  type: bearer
  bearer:
    tokens:
      - "token1"
      - "token2"
    validation_endpoint: "https://auth.example.com/validate"  # Optional
```

#### Client Mode (Sending)

```yaml
auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"
```

### OAuth Authentication

#### Server Mode (Validating)

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://auth.example.com"
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    introspection_url: "https://auth.example.com/introspect"
    audience: "mcp-bridge-api"
    required_scopes:
      - "mcp:read"
    claims:
      subject_claim: "sub"
      email_claim: "email"
    validation:
      allowed_algorithms:
        - "RS256"
      clock_skew: 30s
      require_expiration: true
      max_token_age: 1h
    jwks:
      refresh_interval: 1h
      fetch_timeout: 10s
    introspection:
      enabled: false
      client_id: "mcp-bridge"
      client_secret: "${OAUTH_CLIENT_SECRET}"
```

#### Client Mode (Obtaining)

```yaml
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
```

## TLS Configuration

### Server TLS

```yaml
tls:
  enabled: true
  cert_file: "/path/to/server.crt"
  key_file: "/path/to/server.key"
  client_ca_file: "/path/to/client-ca.crt"
  client_auth: "verify"  # none, request, require, verify
  min_version: "1.2"
  max_version: "1.3"
  cipher_suites:
    - "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"
  curve_preferences:
    - "X25519"
    - "P-256"
```

### Client TLS

```yaml
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

Preflight cache duration in seconds. Default: `0`

```yaml
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

## Health Check Configuration

### health.enabled

Enable health check endpoint. Default: `true`

### health.path

Path for health check endpoint. Default: `/health`

```yaml
health:
  enabled: true
  path: "/health"
```

## Stdio Configuration (Client Mode)

### stdio.buffer_size

Buffer size for stdin/stdout in bytes. Default: `65536`

### stdio.read_timeout

Read timeout for stdin. `0` for no timeout. Default: `0s`

```yaml
stdio:
  buffer_size: 65536
  read_timeout: 0s
```

## Complete Examples

### Server Mode

```yaml
mode: server

server:
  listen: ":8080"
  base_path: "/mcp"
  read_timeout: 30s
  write_timeout: 60s
  idle_timeout: 120s

mcp:
  command: "python"
  args: ["-m", "mcp_server"]
  working_dir: "/opt/mcp-server"
  env:
    PYTHONUNBUFFERED: "1"
  startup_timeout: 10s
  shutdown_timeout: 5s
  restart_on_failure: true
  max_restarts: 5

auth:
  type: bearer
  bearer:
    tokens:
      - "${MCP_AUTH_TOKEN}"

session:
  enabled: true
  timeout: 30m
  max_sessions: 100

cors:
  enabled: true
  allowed_origins:
    - "https://app.example.com"

logging:
  level: info
  format: json

health:
  enabled: true
```

### Client Mode

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"
  timeout: 30s
  connect_timeout: 10s

auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"

retry:
  enabled: true
  max_attempts: 5
  initial_delay: 1s
  max_delay: 30s

session:
  enabled: true

tls:
  ca_cert: "/etc/ssl/certs/ca-certificates.crt"

logging:
  level: info
  format: text
```
