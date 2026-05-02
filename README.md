# GMCore CLI

Developer tool for creating and managing GMCore applications.

## Installation

Download the binary for your platform from the [releases page](https://github.com/gmcorenet/cli/releases):

| Platform | Architecture | File |
|----------|-------------|------|
| Linux | amd64 | `gmcore-cli-linux-amd64` |
| Linux | arm64 | `gmcore-cli-linux-arm64` |
| macOS | amd64 | `gmcore-cli-darwin-amd64` |
| macOS | arm64 | `gmcore-cli-darwin-arm64` |
| Windows | amd64 | `gmcore-cli-windows-amd64.exe` |

```bash
# Example for Linux amd64
curl -fsSL https://github.com/gmcorenet/cli/releases/latest/download/gmcore-cli-linux-amd64 -o /usr/local/bin/gmcore-cli
chmod +x /usr/local/bin/gmcore-cli
```

## Usage

```bash
# Create a new application
gmcore-cli create myapp

# Create with specific version
gmcore-cli create myapp --version=1.0.0

# List available versions
gmcore-cli list-versions

# Show version
gmcore-cli version
```

## Requirements

- Linux, macOS, or Windows
- tar (for extraction during app creation)

