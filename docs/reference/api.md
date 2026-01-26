# API Reference

HTTP API reference for the MCP Bridge in server mode.

## Endpoints

All endpoints are relative to the configured `base_path` (default: `/mcp`).

## POST /mcp - Send Request

Send a JSON-RPC 2.0 request to the MCP server.

### Request

**Headers:**

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | Yes | Must be `application/json` |
| `Authorization` | Depends | Required if authentication is enabled |
| `Mcp-Session-Id` | No | Session ID for stateful connections |

**Body:**

JSON-RPC 2.0 request object.

```json
{
  "jsonrpc": "2.0",
  "method": "string",
  "params": {},
  "id": "string|number"
}
```

### Response

**Success (200 OK):**

```json
{
  "jsonrpc": "2.0",
  "result": {},
  "id": "string|number"
}
```

**Headers:**

| Header | Description |
|--------|-------------|
| `Mcp-Session-Id` | Session ID (on initialize request) |

**Error Responses:**

| Status | Description |
|--------|-------------|
| 400 | Bad Request - Invalid JSON or JSON-RPC format |
| 401 | Unauthorized - Missing or invalid authentication |
| 403 | Forbidden - Insufficient permissions |
| 404 | Not Found - Session not found |
| 503 | Service Unavailable - MCP server not running |

### Example

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer my-token" \
  -d '{
    "jsonrpc": "2.0",
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "clientInfo": {
        "name": "my-client",
        "version": "1.0.0"
      },
      "capabilities": {}
    },
    "id": 1
  }'
```

## GET /mcp - SSE Stream

Subscribe to server-initiated notifications via Server-Sent Events.

### Request

**Headers:**

| Header | Required | Description |
|--------|----------|-------------|
| `Accept` | Yes | Should be `text/event-stream` |
| `Authorization` | Depends | Required if authentication is enabled |
| `Mcp-Session-Id` | Recommended | Session ID to filter notifications |

### Response

**Success (200 OK):**

Content-Type: `text/event-stream`

Events are sent in SSE format:

```
event: message
data: {"jsonrpc":"2.0","method":"notifications/resources/updated","params":{}}

event: message
data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed","params":{}}
```

**Special Events:**

| Event | Description |
|-------|-------------|
| `message` | JSON-RPC notification |
| `close` | Connection being closed |

**Keep-alive:**

The server sends periodic comments to keep the connection alive:

```
: keepalive
```

### Example

```bash
curl -N http://localhost:8080/mcp \
  -H "Accept: text/event-stream" \
  -H "Authorization: Bearer my-token" \
  -H "Mcp-Session-Id: session-123"
```

## DELETE /mcp - Close Session

Close a session and release associated resources.

### Request

**Headers:**

| Header | Required | Description |
|--------|----------|-------------|
| `Authorization` | Depends | Required if authentication is enabled |
| `Mcp-Session-Id` | Yes | Session ID to close |

### Response

**Success (204 No Content):**

No body.

**Error Responses:**

| Status | Description |
|--------|-------------|
| 400 | Bad Request - Missing session ID |
| 401 | Unauthorized - Invalid authentication |
| 404 | Not Found - Session not found |

### Example

```bash
curl -X DELETE http://localhost:8080/mcp \
  -H "Authorization: Bearer my-token" \
  -H "Mcp-Session-Id: session-123"
```

## GET /health - Health Check

Check if the bridge is running and healthy.

### Request

No headers required. Authentication is not required for health checks.

### Response

**Healthy (200 OK):**

```json
{
  "status": "healthy"
}
```

**Unhealthy (503 Service Unavailable):**

```json
{
  "status": "unhealthy",
  "error": "MCP server not running"
}
```

### Example

```bash
curl http://localhost:8080/health
```

## Authentication

### Bearer Token

Include the token in the `Authorization` header:

```
Authorization: Bearer <token>
```

### OAuth

Include the OAuth access token in the `Authorization` header:

```
Authorization: Bearer <access_token>
```

## Error Format

Errors are returned as JSON-RPC 2.0 error responses:

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32600,
    "message": "Invalid Request",
    "data": {}
  },
  "id": null
}
```

### Standard Error Codes

| Code | Message | Description |
|------|---------|-------------|
| -32700 | Parse error | Invalid JSON |
| -32600 | Invalid Request | Invalid JSON-RPC request |
| -32601 | Method not found | Unknown method |
| -32602 | Invalid params | Invalid method parameters |
| -32603 | Internal error | Internal server error |

## Session Management

Sessions enable stateful communication between clients and the MCP server.

### Creating a Session

Sessions are created automatically when an `initialize` request is sent without a `Mcp-Session-Id` header. The response includes the new session ID:

```
Mcp-Session-Id: abc123-def456
```

### Using a Session

Include the session ID in subsequent requests:

```
Mcp-Session-Id: abc123-def456
```

### Session Expiration

Sessions expire after a period of inactivity (configurable via `session.timeout`). Expired sessions return 404 Not Found.

## Rate Limiting

The bridge does not implement rate limiting directly. Use a reverse proxy (nginx, HAProxy, etc.) for rate limiting in production.

## CORS

When CORS is enabled, the server handles preflight requests and includes appropriate CORS headers:

```
Access-Control-Allow-Origin: https://app.example.com
Access-Control-Allow-Methods: GET, POST, DELETE, OPTIONS
Access-Control-Allow-Headers: Authorization, Content-Type, Mcp-Session-Id
Access-Control-Expose-Headers: Mcp-Session-Id
Access-Control-Allow-Credentials: true
Access-Control-Max-Age: 3600
```
