# Client Mode

In client mode, the MCP Bridge connects to a remote HTTP MCP server and exposes it locally via stdio. This allows local MCP clients that only support stdio to connect to remote HTTP-based MCP servers.

## How It Works

```
Local MCP Client              MCP Bridge               Remote MCP Server
    |                              |                              |
    |-- stdin (JSON-RPC) --------->|                              |
    |                              |-- POST /mcp (request) ------>|
    |                              |<---- HTTP Response ----------|
    |<---- stdout (response) ------|                              |
    |                              |                              |
    |                              |-- GET /mcp (SSE) ----------->|
    |<---- stdout (notifications) -|<---- SSE stream -------------|
```

The bridge:

1. Reads JSON-RPC messages from stdin
2. Forwards them as HTTP requests to the remote server
3. Returns responses on stdout
4. Maintains an SSE connection for notifications

## Basic Configuration

Create a `config.yaml` file:

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"
```

Run the bridge:

```bash
mcp-bridge -c config.yaml
```

The bridge reads from stdin and writes to stdout, making it compatible with any stdio-based MCP client.

## Usage with MCP Clients

### With Claude Desktop

Add the bridge to your Claude Desktop configuration:

```json
{
  "mcpServers": {
    "remote-server": {
      "command": "mcp-bridge",
      "args": ["-c", "/path/to/config.yaml"]
    }
  }
}
```

### With Other Stdio Clients

Any MCP client that supports stdio can use the bridge:

```bash
# Pipe messages through the bridge
echo '{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}' | mcp-bridge -c config.yaml
```

## Client Configuration Options

### Server URL

Specify the remote MCP server URL:

```yaml
client:
  url: "https://mcp.example.com/mcp"
```

The URL should include the full path to the MCP endpoint.

### Timeouts

Configure connection and request timeouts:

```yaml
client:
  url: "https://mcp.example.com/mcp"
  timeout: 30s             # Request timeout
  connect_timeout: 10s     # Connection establishment timeout
  keepalive_interval: 30s  # SSE keepalive interval
```

### Connection Pool

Configure HTTP connection pooling:

```yaml
client:
  url: "https://mcp.example.com/mcp"
  max_idle_conns: 10       # Maximum idle connections
  idle_conn_timeout: 90s   # Idle connection timeout
```

### Response Limits

Limit response sizes to prevent memory exhaustion:

```yaml
client:
  url: "https://mcp.example.com/mcp"
  max_response_size: 10485760  # 10 MB
```

## Retry Configuration

Configure automatic retry on connection failures:

```yaml
client:
  url: "https://mcp.example.com/mcp"

retry:
  enabled: true
  max_attempts: 5          # Maximum retry attempts (0 for infinite)
  initial_delay: 1s        # Initial delay between retries
  max_delay: 30s           # Maximum delay (with exponential backoff)
  multiplier: 2.0          # Backoff multiplier
  jitter: true             # Add random jitter to prevent thundering herd
```

With this configuration, retries will occur at approximately:
- Attempt 1: 1s delay
- Attempt 2: 2s delay
- Attempt 3: 4s delay
- Attempt 4: 8s delay
- Attempt 5: 16s delay (capped at 30s)

## Authentication

### Bearer Token

Use a bearer token for authentication:

```yaml
client:
  url: "https://mcp.example.com/mcp"

auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"  # From environment variable
```

Run with the token set:

```bash
export MCP_AUTH_TOKEN="your-secret-token"
mcp-bridge -c config.yaml
```

### OAuth 2.0

Use OAuth client credentials flow:

```yaml
client:
  url: "https://mcp.example.com/mcp"

auth:
  type: oauth
  oauth:
    token_url: "https://auth.example.com/oauth/token"
    client_id: "my-client"
    client_secret: "${OAUTH_CLIENT_SECRET}"
    scopes:
      - "mcp:read"
      - "mcp:write"
```

## TLS Configuration

### Custom CA Certificate

Use a custom CA certificate for server verification:

```yaml
client:
  url: "https://mcp.example.com/mcp"

tls:
  ca_cert: "/path/to/ca.crt"
```

### Client Certificates (mTLS)

Use client certificates for mutual TLS:

```yaml
client:
  url: "https://mcp.example.com/mcp"

tls:
  ca_cert: "/path/to/ca.crt"
  cert_file: "/path/to/client.crt"
  key_file: "/path/to/client.key"
```

### Skip Verification (Development Only)

For development environments with self-signed certificates:

```yaml
client:
  url: "https://localhost:8443/mcp"

tls:
  insecure_skip_verify: true  # NOT recommended for production
```

## Session Management

Enable sessions to maintain state with the server:

```yaml
client:
  url: "https://mcp.example.com/mcp"

session:
  enabled: true
  # Optionally specify a persistent session ID
  # session_id: "my-persistent-session"
```

## Logging

Configure logging for debugging:

```yaml
client:
  url: "https://mcp.example.com/mcp"

logging:
  level: debug  # debug, info, warn, error
  format: text  # text or json
```

Logs are written to stderr so they don't interfere with the stdio MCP communication.

## Complete Example

Here's a complete client mode configuration:

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"
  timeout: 30s
  connect_timeout: 10s
  keepalive_interval: 30s
  max_response_size: 10485760

auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"

retry:
  enabled: true
  max_attempts: 5
  initial_delay: 1s
  max_delay: 30s
  multiplier: 2.0
  jitter: true

session:
  enabled: true

tls:
  ca_cert: "/etc/ssl/certs/ca-certificates.crt"

logging:
  level: info
  format: text

stdio:
  buffer_size: 65536
  read_timeout: 0s  # No timeout for stdin
```

## Troubleshooting

### Connection Refused

If you see "connection refused" errors:

1. Verify the server URL is correct
2. Check if the server is running
3. Verify network connectivity and firewall rules

### Certificate Errors

If you see TLS certificate errors:

1. Verify the server's certificate is valid
2. Use `ca_cert` to specify a custom CA if needed
3. For development, you can use `insecure_skip_verify: true`

### Authentication Failures

If you see 401 or 403 errors:

1. Verify your token or credentials are correct
2. Check that the token hasn't expired
3. Verify the required scopes are configured

### Debug Mode

Enable debug logging to see detailed information:

```yaml
logging:
  level: debug
```

## Next Steps

- [Authentication](authentication.md) - Configure authentication options
- [TLS Configuration](tls.md) - Secure connections with TLS
- [Configuration Reference](configuration.md) - Full configuration options
