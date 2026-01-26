# Installation

This guide covers how to install the pgEdge MCP Bridge on your system.

## Requirements

- Go 1.24 or later (for building from source)
- A supported operating system: Linux, macOS, or Windows

## Download Pre-built Binaries

Pre-built binaries are available for each release on GitHub:

1. Go to the [Releases page](https://github.com/pgEdge/pgedge-mcp-bridge/releases)
2. Download the appropriate archive for your platform:
   - `pgedge-mcp-bridge_VERSION_linux_x86_64.tar.gz` - Linux (AMD64)
   - `pgedge-mcp-bridge_VERSION_linux_arm64.tar.gz` - Linux (ARM64)
   - `pgedge-mcp-bridge_VERSION_darwin_x86_64.tar.gz` - macOS (Intel)
   - `pgedge-mcp-bridge_VERSION_darwin_arm64.tar.gz` - macOS (Apple Silicon)
   - `pgedge-mcp-bridge_VERSION_windows_x86_64.zip` - Windows (AMD64)

3. Extract the archive:

```bash
# Linux/macOS
tar -xzf pgedge-mcp-bridge_VERSION_linux_x86_64.tar.gz

# Windows (PowerShell)
Expand-Archive -Path pgedge-mcp-bridge_VERSION_windows_x86_64.zip -DestinationPath .
```

4. Move the binary to a directory in your PATH:

```bash
# Linux/macOS
sudo mv mcp-bridge /usr/local/bin/

# Or for user-local installation
mv mcp-bridge ~/.local/bin/
```

5. Verify the installation:

```bash
mcp-bridge --version
```

## Build from Source

### Clone the Repository

```bash
git clone https://github.com/pgEdge/pgedge-mcp-bridge.git
cd pgedge-mcp-bridge
```

### Build with Make

The project includes a Makefile with common build targets:

```bash
# Build for your current platform
make build

# The binary will be in bin/mcp-bridge
./bin/mcp-bridge --version
```

### Build for All Platforms

To build binaries for all supported platforms:

```bash
make build-all
```

This creates binaries in `bin/` for:
- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64, arm64)

### Build Manually with Go

```bash
go build -o mcp-bridge ./cmd/mcp-bridge
```

### Install to GOPATH

```bash
go install ./cmd/mcp-bridge
```

## Verify Installation

After installation, verify the bridge is working:

```bash
# Check version
mcp-bridge --version

# Show help
mcp-bridge --help
```

## Configuration File Location

The bridge looks for configuration files in the following locations (in order):

1. Path specified with `-c` or `--config` flag
2. `./config.yaml` (current directory)
3. Directory containing the executable
4. `/etc/mcp-bridge/config.yaml` (Linux/macOS)
5. `~/.config/mcp-bridge/config.yaml` (Linux/macOS)

## Next Steps

- [Server Mode Guide](server-mode.md) - Run the bridge in server mode
- [Client Mode Guide](client-mode.md) - Run the bridge in client mode
- [Configuration Reference](configuration.md) - Full configuration options
