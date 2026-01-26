# pgEdge MCP Bridge

The pgEdge MCP Bridge is a bidirectional transport proxy for the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/). It bridges between HTTP/SSE and stdio transports, enabling MCP clients and servers to communicate regardless of which transport they support.

- **HTTP → stdio**: Expose stdio-based MCP servers over HTTP for remote access
- **stdio → HTTP**: Connect stdio-based MCP clients to remote HTTP MCP servers

## What is MCP?

The Model Context Protocol (MCP) is an open protocol that enables AI assistants to securely connect to data sources and tools. MCP servers expose capabilities like database access, file operations, or API integrations that AI models can use through a standardized interface.

## Why Use the MCP Bridge?

By default, MCP uses stdio (standard input/output) for communication, which requires the client and server to run on the same machine. The MCP Bridge removes this limitation by bridging between transports:

- **Enabling Remote Access**: Expose local stdio MCP servers over HTTP, or connect local stdio clients to remote HTTP servers
- **Supporting Web Clients**: Allow browser-based applications to connect to MCP servers via HTTP/SSE
- **Centralizing MCP Servers**: Run MCP servers on dedicated infrastructure and share them across multiple clients
- **Adding Security Layers**: Implement authentication, TLS encryption, and access control

## Operating Modes

The bridge operates in two modes:

### Server Mode

In server mode, the bridge wraps a local stdio-based MCP server and exposes it over HTTP:

```
MCP Client (HTTP) → MCP Bridge → MCP Server (stdio)
```

Use server mode when you want to:

- Expose a local MCP server to remote clients
- Add HTTP-based authentication to an MCP server
- Enable browser-based clients to connect to MCP servers

### Client Mode

In client mode, the bridge connects to a remote HTTP MCP server and exposes it locally via stdio:

```
MCP Client (stdio) → MCP Bridge → Remote MCP Server (HTTP)
```

Use client mode when you want to:

- Connect local MCP clients to remote servers
- Use existing stdio-based MCP clients with HTTP servers
- Bridge network boundaries while maintaining local client compatibility

## Key Features

- **HTTP/SSE Transport**: Full support for JSON-RPC over HTTP with Server-Sent Events for notifications
- **Authentication**: Bearer token and OAuth 2.0/OIDC authentication
- **TLS Encryption**: HTTPS with optional mutual TLS (mTLS) for client certificates
- **Session Management**: Stateful sessions with automatic timeout and cleanup
- **CORS Support**: Configurable cross-origin resource sharing for browser clients
- **Process Management**: Automatic subprocess restart on failure
- **Health Endpoints**: Built-in health check endpoints for monitoring

## Quick Start

### Server Mode

1. Create a configuration file:

```yaml
mode: server

server:
  listen: ":8080"

mcp:
  command: "python"
  args: ["-m", "my_mcp_server"]
```

2. Run the bridge:

```bash
mcp-bridge -c config.yaml
```

3. Connect from an HTTP client:

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}'
```

### Client Mode

1. Create a configuration file:

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"

auth:
  type: bearer
  bearer:
    token: "${MCP_AUTH_TOKEN}"
```

2. Run the bridge:

```bash
export MCP_AUTH_TOKEN="your-token"
mcp-bridge -c config.yaml
```

3. The bridge now accepts MCP messages on stdin and writes responses to stdout.

## Next Steps

- [Installation](guide/installation.md) - Install the MCP Bridge
- [Server Mode Guide](guide/server-mode.md) - Configure and run in server mode
- [Client Mode Guide](guide/client-mode.md) - Configure and run in client mode
- [Configuration Reference](guide/configuration.md) - Full configuration options
