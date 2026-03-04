# TLS Configuration

This guide covers configuring TLS (Transport Layer Security) for secure HTTPS connections in the MCP Bridge.

## Overview

TLS provides:

- **Encryption**: Protects data in transit from eavesdropping
- **Authentication**: Verifies server identity (and optionally client identity with mTLS)
- **Integrity**: Ensures data hasn't been tampered with

## Server Mode TLS

### Basic HTTPS Setup

Enable HTTPS with a certificate and private key:

```yaml
mode: server

server:
  listen: ":8443"

  tls:
    enabled: true
    cert_file: "/etc/mcp-bridge/tls/server.crt"
    key_file: "/etc/mcp-bridge/tls/server.key"
```

### TLS Version Configuration

Configure minimum and maximum TLS versions:

```yaml
server:
  tls:
    enabled: true
    cert_file: "/etc/mcp-bridge/tls/server.crt"
    key_file: "/etc/mcp-bridge/tls/server.key"
    min_version: "1.2"  # Minimum TLS 1.2 (recommended)
    max_version: "1.3"  # Maximum TLS 1.3
```

Supported versions: `"1.2"`, `"1.3"`

!!! warning
    TLS 1.0 and 1.1 are deprecated and not allowed in the configuration. Always use TLS 1.2 or higher.

## Mutual TLS (mTLS)

Mutual TLS requires clients to present certificates, providing two-way authentication.

### Server Configuration for mTLS

```yaml
server:
  tls:
    enabled: true
    cert_file: "/etc/mcp-bridge/tls/server.crt"
    key_file: "/etc/mcp-bridge/tls/server.key"
    client_ca: "/etc/mcp-bridge/tls/client-ca.crt"
    client_auth: "verify"
```

### Client Authentication Modes

| Mode | Description |
|------|-------------|
| `none` | No client certificate required |
| `request` | Request client certificate but don't require it |
| `require` | Require client certificate but don't verify |
| `verify` | Require and verify client certificate (full mTLS) |

### mTLS Example

```yaml
mode: server

server:
  listen: ":8443"

  tls:
    enabled: true
    cert_file: "/etc/mcp-bridge/tls/server.crt"
    key_file: "/etc/mcp-bridge/tls/server.key"
    client_ca: "/etc/mcp-bridge/tls/client-ca.crt"
    client_auth: "verify"
    min_version: "1.2"
```

## Client Mode TLS

### Custom CA Certificate

Use a custom CA to verify the server certificate:

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"

  tls:
    ca_cert: "/etc/ssl/certs/custom-ca.crt"
```

### Client Certificates for mTLS

Provide client certificates when the server requires mTLS:

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"

  tls:
    ca_cert: "/etc/ssl/certs/server-ca.crt"
    cert_file: "/etc/mcp-bridge/tls/client.crt"
    key_file: "/etc/mcp-bridge/tls/client.key"
```

### Server Name Indication (SNI)

Override the server name for certificate verification:

```yaml
client:
  tls:
    ca_cert: "/etc/ssl/certs/ca.crt"
    server_name: "mcp.example.com"
```

### Skip Certificate Verification

For development environments only:

```yaml
client:
  tls:
    insecure_skip_verify: true
```

!!! danger
    Never use `insecure_skip_verify: true` in production. It disables all certificate verification, making connections vulnerable to man-in-the-middle attacks.

## Generating Certificates

### Self-Signed CA and Server Certificate

```bash
# Generate CA private key
openssl genrsa -out ca.key 4096

# Generate CA certificate
openssl req -new -x509 -days 365 -key ca.key -out ca.crt \
  -subj "/CN=MCP Bridge CA"

# Generate server private key
openssl genrsa -out server.key 2048

# Generate server certificate signing request
openssl req -new -key server.key -out server.csr \
  -subj "/CN=mcp.example.com"

# Sign server certificate with CA
openssl x509 -req -days 365 -in server.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt \
  -extfile <(printf "subjectAltName=DNS:mcp.example.com,DNS:localhost,IP:127.0.0.1")
```

### Client Certificate for mTLS

```bash
# Generate client private key
openssl genrsa -out client.key 2048

# Generate client certificate signing request
openssl req -new -key client.key -out client.csr \
  -subj "/CN=mcp-client"

# Sign client certificate with CA
openssl x509 -req -days 365 -in client.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt
```

### Using Let's Encrypt

For production, use certificates from a trusted CA like Let's Encrypt:

```bash
# Using certbot
certbot certonly --standalone -d mcp.example.com

# Certificates will be in /etc/letsencrypt/live/mcp.example.com/
```

```yaml
server:
  tls:
    enabled: true
    cert_file: "/etc/letsencrypt/live/mcp.example.com/fullchain.pem"
    key_file: "/etc/letsencrypt/live/mcp.example.com/privkey.pem"
```

## Complete TLS Examples

### Production Server with TLS

```yaml
mode: server

server:
  listen: ":443"

  tls:
    enabled: true
    cert_file: "/etc/letsencrypt/live/mcp.example.com/fullchain.pem"
    key_file: "/etc/letsencrypt/live/mcp.example.com/privkey.pem"
    min_version: "1.2"

  auth:
    type: bearer
    bearer:
      valid_tokens:
        - "${MCP_AUTH_TOKEN}"

  mcp_server:
    command: "/usr/local/bin/mcp-server"
```

### mTLS Server

```yaml
mode: server

server:
  listen: ":8443"

  tls:
    enabled: true
    cert_file: "/etc/mcp-bridge/tls/server.crt"
    key_file: "/etc/mcp-bridge/tls/server.key"
    client_ca: "/etc/mcp-bridge/tls/client-ca.crt"
    client_auth: "verify"
    min_version: "1.2"

  mcp_server:
    command: "/usr/local/bin/mcp-server"
```

### mTLS Client

```yaml
mode: client

client:
  url: "https://mcp.example.com:8443/mcp"

  tls:
    ca_cert: "/etc/mcp-bridge/tls/server-ca.crt"
    cert_file: "/etc/mcp-bridge/tls/client.crt"
    key_file: "/etc/mcp-bridge/tls/client.key"
```

## Troubleshooting

### Certificate Errors

**"x509: certificate signed by unknown authority"**

- Add the CA certificate to `ca_cert` or system trust store
- Verify the certificate chain is complete

**"x509: certificate is not valid for"**

- Check that the server name matches the certificate's CN or SAN
- Use `server_name` to specify the expected name

**"tls: bad certificate"**

- Client certificate is missing or invalid
- Verify the client certificate is signed by the expected CA

### Debugging TLS

Enable debug logging:

```yaml
log:
  level: debug
```

Test with OpenSSL:

```bash
# Test server certificate
openssl s_client -connect mcp.example.com:443 -servername mcp.example.com

# Test with client certificate
openssl s_client -connect mcp.example.com:8443 \
  -cert client.crt -key client.key -CAfile ca.crt
```

## Next Steps

- [Authentication](authentication.md) - Configure authentication
- [Configuration Reference](configuration.md) - Full configuration options
