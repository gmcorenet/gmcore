# GMCore CLI

Cross-platform command-line tool for creating and managing GMCore applications.

## Features

- Create new applications with a single command
- List available framework versions
- Self-update to latest version
- Cross-platform support (Linux, macOS, Windows)

## Installation

### Linux / macOS

```bash
curl -fsSL https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-linux-amd64 -o /usr/local/bin/gmcore-cli
chmod +x /usr/local/bin/gmcore-cli
```

### Windows

Download the latest release from the [releases page](https://github.com/gmcorenet/gmcore/releases) and add it to your PATH.

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
