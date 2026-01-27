# API Reference

HTTP API reference for the MCP Bridge in server mode.

## Endpoints

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

## GET /ready - Readiness Check

Check if the bridge is ready to serve requests (MCP subprocess is running).

### Request

No headers required. Authentication is not required for readiness checks.

### Response

**Ready (200 OK):**

```json
{
  "status": "ready"
}
```

**Not Ready (503 Service Unavailable):**

```json
{
  "status": "not_ready",
  "reason": "mcp_subprocess_not_running"
}
```

### Example

```bash
curl http://localhost:8080/ready
```

## OAuth Authorization Server Endpoints

These endpoints are available when `oauth_server.enabled: true` in the configuration.

### GET /.well-known/oauth-authorization-server

Returns OAuth 2.0 Authorization Server Metadata (RFC 8414).

**Response (200 OK):**

```json
{
  "issuer": "https://mcp.example.com",
  "authorization_endpoint": "https://mcp.example.com/oauth/authorize",
  "token_endpoint": "https://mcp.example.com/oauth/token",
  "jwks_uri": "https://mcp.example.com/oauth/jwks",
  "registration_endpoint": "https://mcp.example.com/oauth/register",
  "scopes_supported": ["mcp:read", "mcp:write"],
  "response_types_supported": ["code"],
  "response_modes_supported": ["query"],
  "grant_types_supported": ["authorization_code", "refresh_token"],
  "token_endpoint_auth_methods_supported": ["none", "client_secret_post"],
  "code_challenge_methods_supported": ["S256"]
}
```

**Example:**

```bash
curl https://mcp.example.com/.well-known/oauth-authorization-server
```

### GET /oauth/jwks

Returns the JSON Web Key Set for verifying tokens.

**Response (200 OK):**

```json
{
  "keys": [
    {
      "kty": "RSA",
      "use": "sig",
      "alg": "RS256",
      "kid": "key-1",
      "n": "...",
      "e": "AQAB"
    }
  ]
}
```

**Example:**

```bash
curl https://mcp.example.com/oauth/jwks
```

### GET /oauth/authorize

Displays the authorization page (login form or IdP redirect).

**Query Parameters:**

| Parameter | Required | Description |
|-----------|----------|-------------|
| `response_type` | Yes | Must be `code` |
| `client_id` | Yes | Client identifier |
| `redirect_uri` | Yes | Callback URL |
| `state` | Recommended | CSRF protection value |
| `code_challenge` | Yes | PKCE code challenge (S256) |
| `code_challenge_method` | Yes | Must be `S256` |
| `scope` | No | Space-separated scopes |

**Example:**

```
GET /oauth/authorize?response_type=code&client_id=my-client&redirect_uri=https://claude.ai/api/mcp/auth_callback&state=abc123&code_challenge=E9Melhoa...&code_challenge_method=S256
```

### POST /oauth/authorize

Processes the login form submission.

**Content-Type:** `application/x-www-form-urlencoded`

**Form Fields:**

| Field | Description |
|-------|-------------|
| `username` | User's login name |
| `password` | User's password |
| `csrf_token` | CSRF token from the form |

**Response:** Redirects to `redirect_uri` with authorization code.

### POST /oauth/token

Exchange authorization code for tokens or refresh tokens.

**Content-Type:** `application/x-www-form-urlencoded`

**Authorization Code Exchange:**

| Field | Required | Description |
|-------|----------|-------------|
| `grant_type` | Yes | `authorization_code` |
| `code` | Yes | Authorization code |
| `redirect_uri` | Yes | Must match authorize request |
| `client_id` | Yes | Client identifier |
| `code_verifier` | Yes | PKCE code verifier |

**Refresh Token:**

| Field | Required | Description |
|-------|----------|-------------|
| `grant_type` | Yes | `refresh_token` |
| `refresh_token` | Yes | Refresh token |
| `client_id` | Yes | Client identifier |

**Response (200 OK):**

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "dGhpcyBpcyBhIHJl...",
  "scope": "mcp:read mcp:write"
}
```

**Error Response (400 Bad Request):**

```json
{
  "error": "invalid_grant",
  "error_description": "Authorization code has expired"
}
```

**Example:**

```bash
curl -X POST https://mcp.example.com/oauth/token \
  -d "grant_type=authorization_code" \
  -d "code=AUTH_CODE" \
  -d "redirect_uri=https://claude.ai/api/mcp/auth_callback" \
  -d "client_id=my-client" \
  -d "code_verifier=VERIFIER"
```

### GET /oauth/callback

Callback endpoint for federated mode. Handles the redirect from the upstream IdP.

**Note:** This endpoint is only used in federated mode and is not called directly by clients.

**Query Parameters:**

| Parameter | Description |
|-----------|-------------|
| `code` | Authorization code from upstream IdP |
| `state` | State parameter to correlate with original request |
| `error` | Error code if authentication failed |
| `error_description` | Human-readable error description |

**Response:** Redirects to the original client's `redirect_uri` with either an authorization code or error.

### POST /oauth/register

Dynamic client registration (RFC 7591). Only available when `allow_dynamic_registration: true`.

**Content-Type:** `application/json`

**Request Body:**

```json
{
  "redirect_uris": ["https://example.com/callback"],
  "client_name": "My Application",
  "token_endpoint_auth_method": "none"
}
```

**Response (201 Created):**

```json
{
  "client_id": "generated-client-id",
  "client_secret": "generated-secret",
  "redirect_uris": ["https://example.com/callback"],
  "client_name": "My Application",
  "token_endpoint_auth_method": "none"
}
```

**Example:**

```bash
curl -X POST https://mcp.example.com/oauth/register \
  -H "Content-Type: application/json" \
  -d '{"redirect_uris": ["https://example.com/callback"], "client_name": "My App"}'
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
