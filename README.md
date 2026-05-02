# GMCore CLI

Cross-platform command-line tool for creating and managing GMCore applications.

## Features

- Create new GMCore applications with a single command
- List and manage installed applications
- Check application status
- Self-update to latest version
- Full support for Linux, macOS, and Windows

## Installation

### Linux / macOS

```bash
# Download the latest release
curl -fsSL https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-cli-linux-amd64 -o /usr/local/bin/gmcore-cli

# Make it executable
chmod +x /usr/local/bin/gmcore-cli
```

### Windows

1. Download `gmcore-cli-windows-amd64.exe` from the [releases page](https://github.com/gmcorenet/gmcore/releases)
2. Add it to your PATH

### Build from Source

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

# List installed applications
gmcore-cli list

# Check application status
gmcore-cli status
gmcore-cli status myapp

# Get help
gmcore-cli --help
```

## Commands

### Application Management

| Command | Description |
|---------|-------------|
| `gmcore-cli create <appname>` | Create a new application |
| `gmcore-cli create <appname> --version=<ver>` | Create with specific framework version |
| `gmcore-cli remove <appname>` | Remove an application |
| `gmcore-cli remove <appname> --purge` | Remove application and all data |
| `gmcore-cli list` | List all installed applications |
| `gmcore-cli status [appname]` | Show application status |

### CLI Management

| Command | Description |
|---------|-------------|
| `gmcore-cli list-versions` | List available framework versions |
| `gmcore-cli self-update` | Update CLI to latest version |
| `gmcore-cli self-update <ver>` | Update to specific version |
| `gmcore-cli version` | Show CLI version |
| `gmcore-cli install` | Install CLI system-wide (requires root) |
| `gmcore-cli uninstall` | Uninstall CLI |
| `gmcore-cli uninstall --purge` | Uninstall and remove all apps |

## Application Directory Structure

When you create an app, the following structure is created:

```
/opt/gmcore/<appname>/
├── bin/                    # Compiled binaries
├── cmd/app/               # Application entry point
├── config/                # Configuration files
├── public/                # Static files
├── src/                   # Source code
├── templates/             # Templates
├── var/
│   ├── cache/             # Cache files
│   ├── log/               # Log files
│   └── tmp/               # Temporary files
├── vendor/gmcore/         # Framework source
├── migrations/            # Database migrations
└── tests/                # Test files
```

## Running Your Application

```bash
# Navigate to your application
cd /opt/gmcore/myapp

# Run application commands
gmcore-cli <command>

# Build the application
go build -o bin/myapp cmd/app/main.go

# Run the application
./bin/myapp
```

## Requirements

- Linux, macOS, or Windows
- Git
- tar (Linux/macOS)
- Root/sudo for system-wide installation (Linux/macOS)

## Platform Support

| Platform | amd64 | arm64 |
|----------|-------|-------|
| Linux | Yes | Yes |
| macOS | Yes | Yes |
| Windows | Yes | No |

## Uninstall

```bash
# Remove just the CLI
sudo gmcore-cli uninstall

# Remove CLI and all applications
sudo gmcore-cli uninstall --purge
```

## License

MIT
