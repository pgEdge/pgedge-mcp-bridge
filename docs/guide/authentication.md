# Authentication

The MCP Bridge supports multiple authentication methods to secure access to MCP servers. This guide covers configuring authentication for both server mode (validating incoming requests) and client mode (authenticating outgoing requests).

## Authentication Types

| Type | Description | Use Case |
|------|-------------|----------|
| `none` | No authentication | Development, internal networks |
| `bearer` | Bearer token authentication | Simple API key authentication |
| `oauth` | OAuth 2.0/OIDC | Enterprise SSO, fine-grained access control |

## No Authentication

Disable authentication for development or trusted networks:

```yaml
auth:
  type: none
```

!!! warning
    Never use `type: none` in production environments exposed to untrusted networks.

## Bearer Token Authentication

Bearer token authentication uses simple tokens passed in the `Authorization` header.

### Server Mode (Validating Tokens)

In server mode, configure valid tokens that clients can use:

```yaml
auth:
  type: bearer
  bearer:
    tokens:
      - "token-abc123"
      - "token-def456"
```

**Using Environment Variables**

For security, use environment variables for tokens:

```yaml
auth:
  type: bearer
  bearer:
    tokens:
      - "${API_TOKEN_1}"
      - "${API_TOKEN_2}"
```

**Remote Token Validation**

Validate tokens against a remote endpoint:

```yaml
auth:
  type: bearer
  bearer:
    validation_endpoint: "https://auth.example.com/validate"
```

The endpoint receives a POST request with:

```json
{
  "token": "the-bearer-token"
}
```

And should respond with:

```json
{
  "valid": true,
  "subject": "user-id",
  "scopes": ["mcp:read", "mcp:write"],
  "claims": {},
  "metadata": {}
}
```

### Client Mode (Sending Tokens)

In client mode, configure the token to send with requests:

```yaml
auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"
```

**Static Token**

```yaml
auth:
  type: bearer
  bearer:
    token: "my-secret-token"
```

**From Environment Variable**

```yaml
auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"
```

Then run:

```bash
export MCP_AUTH_TOKEN="my-secret-token"
mcp-bridge -c config.yaml
```

## OAuth 2.0 Authentication

OAuth 2.0 provides more sophisticated authentication with automatic token refresh, scopes, and integration with identity providers.

### Server Mode (Validating OAuth Tokens)

#### Using OIDC Discovery

Automatically configure from an OpenID Connect provider:

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://auth.example.com"
    audience: "mcp-bridge-api"
    required_scopes:
      - "mcp:read"
```

The bridge fetches configuration from `{discovery_url}/.well-known/openid-configuration`.

#### Using JWKS Directly

Validate JWT tokens using a JWKS endpoint:

```yaml
auth:
  type: oauth
  oauth:
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    audience: "mcp-bridge-api"
```

#### Token Introspection

For opaque tokens, use token introspection (RFC 7662):

```yaml
auth:
  type: oauth
  oauth:
    introspection_url: "https://auth.example.com/oauth/introspect"
    client_id: "mcp-bridge"
    client_secret: "${OAUTH_CLIENT_SECRET}"
```

#### Complete OAuth Server Configuration

```yaml
auth:
  type: oauth
  oauth:
    # OIDC discovery (recommended)
    discovery_url: "https://auth.example.com"

    # Token validation
    audience: "mcp-bridge-api"
    required_scopes:
      - "mcp:read"
      - "mcp:write"

    # Claim mappings
    claims:
      subject_claim: "sub"
      email_claim: "email"
      roles_claim: "roles"

    # Validation options
    validation:
      allowed_algorithms:
        - "RS256"
        - "ES256"
      clock_skew: 30s
      require_expiration: true
      max_token_age: 1h

    # JWKS caching
    jwks:
      refresh_interval: 1h
      fetch_timeout: 10s

    # Fallback to introspection
    introspection:
      enabled: true
      endpoint: "https://auth.example.com/oauth/introspect"
      client_id: "mcp-bridge"
      client_secret: "${OAUTH_CLIENT_SECRET}"
```

### Client Mode (Obtaining OAuth Tokens)

In client mode, the bridge obtains tokens using the OAuth client credentials flow.

#### Basic Client Credentials

```yaml
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

#### Using OIDC Discovery

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://auth.example.com"
    client_id: "my-client"
    client_secret: "${OAUTH_CLIENT_SECRET}"
    scopes:
      - "mcp:read"
```

#### With PKCE (Recommended)

PKCE (Proof Key for Code Exchange) adds security for client-side applications:

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://auth.example.com"
    client_id: "my-client"
    client_secret: "${OAUTH_CLIENT_SECRET}"
    use_pkce: true
```

#### Resource Indicators (RFC 8707)

Specify the target resource for the token:

```yaml
auth:
  type: oauth
  oauth:
    token_url: "https://auth.example.com/oauth/token"
    client_id: "my-client"
    client_secret: "${OAUTH_CLIENT_SECRET}"
    resource: "urn:mcp-server:production"
```

## Common OAuth Providers

### Auth0

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://your-tenant.auth0.com"
    audience: "https://api.example.com"
    required_scopes:
      - "read:mcp"
```

### Okta

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://your-org.okta.com/oauth2/default"
    audience: "api://mcp-bridge"
```

### Keycloak

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://keycloak.example.com/realms/myrealm"
    audience: "mcp-bridge"
```

### Google Cloud Identity

```yaml
auth:
  type: oauth
  oauth:
    discovery_url: "https://accounts.google.com"
    audience: "your-project-id.apps.googleusercontent.com"
```

## Security Best Practices

### Token Security

1. **Never commit tokens** to version control
2. **Use environment variables** for sensitive values
3. **Rotate tokens regularly**
4. **Use short-lived tokens** when possible

### Server Mode

1. **Always enable authentication** in production
2. **Use HTTPS** (TLS) in conjunction with authentication
3. **Limit token scopes** to minimum required permissions
4. **Log authentication failures** for security monitoring

### Client Mode

1. **Store credentials securely** (environment variables, secret managers)
2. **Use OAuth** for automatic token refresh
3. **Verify server certificates** (don't use `insecure_skip_verify`)

## Troubleshooting

### 401 Unauthorized

- Verify the token is correct and not expired
- Check that required scopes are present
- Ensure the audience matches

### 403 Forbidden

- Token is valid but lacks required permissions
- Check required scopes configuration

### Token Validation Failures

Enable debug logging to see detailed validation errors:

```yaml
logging:
  level: debug
```

## Next Steps

- [TLS Configuration](tls.md) - Secure connections with TLS
- [Configuration Reference](configuration.md) - Full configuration options
