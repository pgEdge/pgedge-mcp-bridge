# pgEdge MCP Bridge

[![CI](https://github.com/pgEdge/pgedge-mcp-bridge/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/pgEdge/pgedge-mcp-bridge/actions/workflows/ci.yml?query=branch%3Amain)
[![CI - Documentation](https://github.com/pgEdge/pgedge-mcp-bridge/actions/workflows/ci-docs.yml/badge.svg?branch=main)](https://github.com/pgEdge/pgedge-mcp-bridge/actions/workflows/ci-docs.yml?query=branch%3Amain)
[![Release](https://github.com/pgEdge/pgedge-mcp-bridge/actions/workflows/release.yml/badge.svg)](https://github.com/pgEdge/pgedge-mcp-bridge/actions/workflows/release.yml)

A bidirectional transport proxy for the
[Model Context Protocol (MCP)](https://modelcontextprotocol.io/). It bridges
between HTTP/SSE and stdio transports, enabling MCP clients and servers to
communicate regardless of which transport they support.

- **Server mode** (HTTP to stdio) -- expose a local MCP server over HTTP for
  remote access
- **Client mode** (stdio to HTTP) -- connect a local stdio client to a remote
  HTTP MCP server

Full documentation is available at
[pgEdge MCP Bridge Docs](https://pgedge.github.io/pgedge-mcp-bridge/).

## Quick Start

### Install

Download a pre-built binary from the
[Releases](https://github.com/pgEdge/pgedge-mcp-bridge/releases) page, or
build from source:

```bash
git clone https://github.com/pgEdge/pgedge-mcp-bridge.git
cd pgedge-mcp-bridge
make build          # outputs bin/mcp-bridge
```

Requires Go 1.24+.

### Server Mode

Create `config.yaml`:

```yaml
mode: server

server:
  listen: ":8080"
  mcp_server:
    command: "python"
    args: ["-m", "my_mcp_server"]
```

Run the bridge:

```bash
bin/mcp-bridge -c config.yaml
```

Test with curl:

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}'
```

### Client Mode

Create `config.yaml`:

```yaml
mode: client

client:
  url: "https://mcp.example.com/mcp"
  auth:
    type: bearer
    bearer:
      token: "${MCP_AUTH_TOKEN}"
```

Run the bridge (it reads MCP messages on stdin, writes responses to stdout):

```bash
export MCP_AUTH_TOKEN="your-token"
bin/mcp-bridge -c config.yaml
```

## Development

### Prerequisites

- Go 1.24+
- [golangci-lint](https://golangci-lint.run/) (install via `make install-tools`)

### Build & Test

```bash
make build           # compile binary to bin/
make test            # run tests with race detector
make test-coverage   # run tests and generate HTML coverage report
make lint            # run golangci-lint
make fmt             # format code with gofmt
make check           # run fmt-check, vet, lint, and test
```

### Project Layout

```
cmd/mcp-bridge/      # application entry point
internal/
  auth/              # authentication (bearer, OAuth client)
  authserver/        # built-in OAuth 2.0 authorization server
  client/            # client-mode HTTP transport and SSE reader
  config/            # configuration loading and validation
  cors/              # CORS middleware
  logging/           # structured logging
  process/           # subprocess management
  protocol/          # JSON-RPC and MCP protocol types
  server/            # server-mode HTTP handler and session management
  tls/               # TLS configuration helpers
pkg/version/         # build version info
docs/                # full documentation (MkDocs)
examples/            # example configuration files
```

### Useful Make Targets

| Target                  | Description                                     |
|-------------------------|-------------------------------------------------|
| `make build`            | Build the binary                                |
| `make build-all`        | Cross-compile for linux/darwin/windows           |
| `make test`             | Run tests with race detector                    |
| `make test-coverage`    | Generate HTML coverage report in `coverage/`    |
| `make lint`             | Run golangci-lint                               |
| `make check`            | Run all checks (format, vet, lint, test)        |
| `make clean`            | Remove build artifacts                          |
| `make help`             | Show all available targets                      |

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Make your changes and ensure `make check` passes
4. Commit and push your branch
5. Open a pull request against `main`

## License

Released under [The PostgreSQL License](LICENCE.md).
