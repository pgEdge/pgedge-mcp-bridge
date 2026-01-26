# Server Mode

In server mode, the MCP Bridge wraps a local stdio-based MCP server and exposes it over HTTP. This allows remote clients to connect to MCP servers that would otherwise only be accessible locally.

## How It Works

```
HTTP Client                    MCP Bridge                    MCP Server
    |                              |                              |
    |-- POST /mcp (request) ------>|                              |
    |                              |-- stdin (JSON-RPC) --------->|
    |                              |<-- stdout (response) --------|
    |<---- HTTP Response ----------|                              |
    |                              |                              |
    |-- GET /mcp (SSE) ----------->|                              |
    |<---- SSE notifications ------|<-- stdout (notifications) ---|
```

The bridge:

1. Starts your MCP server as a subprocess
2. Listens for HTTP requests on the configured port
3. Forwards requests to the subprocess via stdin
4. Returns responses from stdout as HTTP responses
5. Streams notifications via Server-Sent Events (SSE)

## Basic Configuration

Create a `config.yaml` file:

```yaml
mode: server

server:
  listen: ":8080"

mcp:
  command: "python"
  args: ["-m", "my_mcp_server"]
```

Run the bridge:

```bash
mcp-bridge -c config.yaml
```

## HTTP Endpoints

The bridge exposes the following endpoints:

### POST /mcp - Send Requests

Send JSON-RPC requests to the MCP server:

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "clientInfo": {
        "name": "my-client",
        "version": "1.0.0"
      }
    },
    "id": 1
  }'
```

### GET /mcp - SSE Notifications

Subscribe to server notifications via Server-Sent Events:

```bash
curl -N http://localhost:8080/mcp \
  -H "Accept: text/event-stream"
```

### DELETE /mcp - Close Session

Close a session and release resources:

```bash
curl -X DELETE http://localhost:8080/mcp \
  -H "Mcp-Session-Id: session-123"
```

### GET /health - Health Check

Check if the bridge is running:

```bash
curl http://localhost:8080/health
# {"status":"healthy"}
```

## Server Configuration Options

### Listen Address

Configure the address and port to listen on:

```yaml
server:
  # Listen on all interfaces
  listen: ":8080"

  # Listen only on localhost
  listen: "127.0.0.1:8080"

  # Listen on specific IP
  listen: "192.168.1.100:8080"
```

### HTTP Timeouts

Configure HTTP server timeouts:

```yaml
server:
  listen: ":8080"
  read_timeout: 30s      # Time to read request headers and body
  write_timeout: 60s     # Time to write the response
  idle_timeout: 120s     # Keep-alive connection timeout
```

### Base Path

Change the base path for MCP endpoints:

```yaml
server:
  listen: ":8080"
  base_path: "/api/v1/mcp"  # Endpoints will be /api/v1/mcp, etc.
```

## MCP Server Configuration

### Basic Command

Specify the command to run your MCP server:

```yaml
mcp:
  command: "python"
  args: ["-m", "my_mcp_server"]
```

### Working Directory

Set the working directory for the subprocess:

```yaml
mcp:
  command: "./mcp-server"
  working_dir: "/opt/mcp-server"
```

### Environment Variables

Pass environment variables to the subprocess:

```yaml
mcp:
  command: "node"
  args: ["server.js"]
  env:
    NODE_ENV: "production"
    LOG_LEVEL: "info"
    DATABASE_URL: "${DATABASE_URL}"  # From bridge's environment
```

### Startup and Shutdown

Configure startup and shutdown behavior:

```yaml
mcp:
  command: "python"
  args: ["-m", "mcp_server"]
  startup_timeout: 10s    # Wait for server to start
  shutdown_timeout: 5s    # Wait for graceful shutdown
```

### Auto-Restart on Failure

Enable automatic restart when the subprocess crashes:

```yaml
mcp:
  command: "python"
  args: ["-m", "mcp_server"]
  restart_on_failure: true
  max_restarts: 5          # Maximum restart attempts
  restart_delay: 5s        # Delay between restarts
```

## Session Management

Sessions allow clients to maintain state across requests:

```yaml
session:
  enabled: true
  timeout: 30m           # Session expires after 30 minutes of inactivity
  max_sessions: 100      # Maximum concurrent sessions
  cleanup_interval: 5m   # How often to clean up expired sessions
```

Clients include the session ID in requests:

```bash
# First request creates a session
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}'

# Response includes session ID header
# Mcp-Session-Id: abc123

# Subsequent requests use the session
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: abc123" \
  -d '{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}'
```

## Complete Example

Here's a complete server mode configuration:

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
  args:
    - "-m"
    - "mcp_server"
    - "--config"
    - "/etc/mcp/config.json"
  working_dir: "/opt/mcp-server"
  env:
    PYTHONUNBUFFERED: "1"
    LOG_LEVEL: "info"
  startup_timeout: 10s
  shutdown_timeout: 5s
  restart_on_failure: true
  max_restarts: 5
  restart_delay: 5s

session:
  enabled: true
  timeout: 30m
  max_sessions: 100

auth:
  type: bearer
  bearer:
    tokens:
      - "${MCP_AUTH_TOKEN}"

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
  allow_credentials: true

logging:
  level: info
  format: json

health:
  enabled: true
  path: "/health"
```

## Next Steps

- [Authentication](authentication.md) - Secure your server with authentication
- [TLS Configuration](tls.md) - Enable HTTPS
- [Configuration Reference](configuration.md) - Full configuration options
