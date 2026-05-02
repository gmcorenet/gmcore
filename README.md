# GMCore CLI

Cross-platform command-line tool for creating and managing GMCore applications.

## Features

- Create new applications with a single command
- List available framework versions
- Self-update to latest version
- Cross-platform support (Linux, macOS, Windows)

## Installation

### Download pre-built binaries

| Platform | Architecture | Download |
|----------|-------------|----------|
| Linux | amd64 | [gmcore-cli-linux-amd64](https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-linux-amd64) |
| Linux | arm64 | [gmcore-cli-linux-arm64](https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-linux-arm64) |
| macOS | amd64 | [gmcore-cli-darwin-amd64](https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-darwin-amd64) |
| macOS | arm64 | [gmcore-cli-darwin-arm64](https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-darwin-arm64) |
| Windows | amd64 | [gmcore-cli-windows-amd64.exe](https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-windows-amd64.exe) |

```bash
# Example for Linux amd64
curl -fsSL https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-linux-amd64 -o /usr/local/bin/gmcore-cli
chmod +x /usr/local/bin/gmcore-cli
```

### Build from source

```bash
git clone https://github.com/gmcorenet/gmcore.git
cd gmcore
go build -o gmcore-cli .
sudo mv gmcore-cli /usr/local/bin/
```

## Quick Start

```bash
# Create a new application
gmcore-cli create myapp

# Create with specific framework version
gmcore-cli create myapp --version=1.0.0

# List available framework versions
gmcore-cli list-versions

# Check installed applications
gmcore-cli list

# Show application status
gmcore-cli status

# Update CLI to latest version
gmcore-cli self-update

# Show version
gmcore-cli version
```

## Managing Applications

```bash
# Remove an application
sudo gmcore-cli remove myapp

# Remove application and all data (purge)
sudo gmcore-cli remove myapp --purge

# Uninstall CLI
sudo gmcore-cli uninstall
```

## Requirements

- Linux, macOS, or Windows
- Git (for cloning applications)
- tar (Linux/macOS, for extraction during app creation)

## License

MIT
