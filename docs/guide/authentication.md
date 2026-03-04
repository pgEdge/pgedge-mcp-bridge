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
    valid_tokens:
      - "token-abc123"
      - "token-def456"
```

**Using Environment Variables**

For security, use environment variables for tokens:

```yaml
auth:
  type: bearer
  bearer:
    valid_tokens:
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
server:
  auth:
    type: oauth
    oauth:
      # OIDC discovery (recommended)
      discovery_url: "https://auth.example.com"

      # JWKS URL for JWT validation
      jwks_url: "https://auth.example.com/.well-known/jwks.json"

      # Scopes to require
      scopes:
        - "mcp:read"
        - "mcp:write"

      # Fallback to introspection for opaque tokens
      introspection_url: "https://auth.example.com/oauth/introspect"
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

## OAuth Authorization Server

The MCP Bridge can act as an OAuth 2.0 Authorization Server, issuing access tokens for clients. This enables direct integration with tools like Claude Desktop's "Remote MCP Connector" feature without needing intermediate tools.

### When to Use

Use the OAuth Authorization Server when:

- Connecting Claude Desktop directly to the bridge
- You want the bridge to manage its own authentication
- You need to issue tokens for MCP access

### Built-in Mode

Built-in mode provides local user management with bcrypt password hashing:

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
        - username: reader
          password_env: "READER_PASSWORD"  # hashed at runtime
          scopes: ["mcp:read"]
    allowed_redirect_uris:
      - "https://claude.ai/api/mcp/auth_callback"
    scopes_supported:
      - "mcp:read"
      - "mcp:write"
```

**Generating Password Hashes**

Use the `htpasswd` utility or a bcrypt library:

```bash
# Using htpasswd (Apache utilities)
htpasswd -nbBC 10 "" 'mypassword' | tr -d ':\n'

# Using Python
python3 -c "import bcrypt; print(bcrypt.hashpw(b'mypassword', bcrypt.gensalt()).decode())"
```

### Federated Mode

Federated mode delegates authentication to an upstream identity provider (Google, Okta, etc.):

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

### OAuth Endpoints

When enabled, the authorization server exposes these endpoints:

| Endpoint | Description |
|----------|-------------|
| `/.well-known/oauth-authorization-server` | Server metadata (RFC 8414) |
| `/oauth/jwks` | JSON Web Key Set for token verification |
| `/oauth/authorize` | Authorization endpoint (login UI) |
| `/oauth/token` | Token endpoint (code exchange, refresh) |
| `/oauth/register` | Dynamic client registration (if enabled) |

### Using with Claude Desktop

1. Enable the OAuth server in your configuration
2. In Claude Desktop, go to Settings → Connectors
3. Add a new MCP Remote connector with your bridge URL
4. Claude Desktop will automatically discover OAuth endpoints and initiate the flow
5. Log in with your configured credentials
6. The bridge issues tokens that Claude Desktop uses for subsequent requests

### JWT Signing Keys

For production, generate a proper RSA or ECDSA key pair:

```bash
# RSA 2048-bit key
openssl genrsa -out jwt-private.pem 2048
openssl rsa -in jwt-private.pem -pubout -out jwt-public.pem

# ECDSA P-256 key
openssl ecparam -genkey -name prime256v1 -noout -out jwt-private.pem
openssl ec -in jwt-private.pem -pubout -out jwt-public.pem
```

For development only, you can use `generate_key: true` to create ephemeral keys:

```yaml
signing:
  algorithm: RS256
  generate_key: true  # WARNING: Keys lost on restart
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
log:
  level: debug
```

## Next Steps

- [TLS Configuration](tls.md) - Secure connections with TLS
- [Configuration Reference](configuration.md) - Full configuration options
