# CLI Reference

Command-line interface reference for the MCP Bridge.

## Synopsis

```
mcp-bridge [OPTIONS]
```

## Options

### -c, --config PATH

Path to the configuration file.

If not specified, the bridge searches for `config.yaml` in:

1. Current working directory
2. Directory containing the executable
3. `/etc/mcp-bridge/` (Linux/macOS)
4. `~/.config/mcp-bridge/` (Linux/macOS)

**Example:**

```bash
mcp-bridge -c /etc/mcp-bridge/config.yaml
mcp-bridge --config ./my-config.yaml
```

### -v, --version

Print version information and exit.

**Example:**

```bash
mcp-bridge --version
# mcp-bridge version 1.0.0 (commit: abc1234, built: 2024-01-01T00:00:00Z)
```

### -h, --help

Print help information and exit.

**Example:**

```bash
mcp-bridge --help
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | Error (configuration, startup, or runtime error) |

## Environment Variables

The bridge supports environment variable substitution in configuration files using `${VAR}` or `$VAR` syntax.

Common environment variables:

| Variable | Description |
|----------|-------------|
| `MCP_AUTH_TOKEN` | Bearer authentication token |
| `OAUTH_CLIENT_SECRET` | OAuth client secret |

**Example:**

```bash
export MCP_AUTH_TOKEN="my-secret-token"
mcp-bridge -c config.yaml
```

## Signals

The bridge responds to the following signals:

| Signal | Behavior |
|--------|----------|
| `SIGINT` (Ctrl+C) | Graceful shutdown |
| `SIGTERM` | Graceful shutdown |

During graceful shutdown, the bridge:

1. Stops accepting new connections
2. Closes active SSE connections
3. Stops the MCP subprocess (server mode)
4. Waits for pending requests to complete (with timeout)

## Examples

### Start in Server Mode

```bash
mcp-bridge -c server-config.yaml
```

### Start in Client Mode

```bash
export MCP_AUTH_TOKEN="my-token"
mcp-bridge -c client-config.yaml
```

### Use with Claude Desktop

Add to Claude Desktop's MCP configuration:

```json
{
  "mcpServers": {
    "remote-mcp": {
      "command": "mcp-bridge",
      "args": ["-c", "/path/to/client-config.yaml"],
      "env": {
        "MCP_AUTH_TOKEN": "your-token"
      }
    }
  }
}
```

### Run with Docker

```bash
docker run -v /path/to/config.yaml:/config.yaml \
  -e MCP_AUTH_TOKEN="my-token" \
  -p 8080:8080 \
  pgedge/mcp-bridge -c /config.yaml
```

### Systemd Service

Create `/etc/systemd/system/mcp-bridge.service`:

```ini
[Unit]
Description=MCP HTTP Bridge
After=network.target

[Service]
Type=simple
User=mcp
Group=mcp
ExecStart=/usr/local/bin/mcp-bridge -c /etc/mcp-bridge/config.yaml
Restart=on-failure
RestartSec=5
Environment=MCP_AUTH_TOKEN=your-token

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable mcp-bridge
sudo systemctl start mcp-bridge
```
