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
curl -fsSL https://github.com/gmcorenet/gmcore/releases/latest/download/gmcore-linux-amd64 -o /usr/local/bin/gmcore

# Make it executable
chmod +x /usr/local/bin/gmcore
```

### Windows

1. Download `gmcore-windows-amd64.exe` from the [releases page](https://github.com/gmcorenet/gmcore/releases)
2. Add it to your PATH

### Build from Source

```bash
git clone https://github.com/gmcorenet/gmcore.git
cd gmcore
go build -o gmcore .
sudo mv gmcore /usr/local/bin/
```

## Quick Start

```bash
# Create a new application
gmcore create myapp

# List installed applications
gmcore list

# Check application status
gmcore status
gmcore status myapp

# Get help
gmcore --help
```

## Commands

### Application Management

| Command | Description |
|---------|-------------|
| `gmcore create <appname>` | Create a new application |
| `gmcore create <appname> --version=<ver>` | Create with specific framework version |
| `gmcore remove <appname>` | Remove an application |
| `gmcore remove <appname> --purge` | Remove application and all data |
| `gmcore list` | List all installed applications |
| `gmcore status [appname]` | Show application status |

### CLI Management

| Command | Description |
|---------|-------------|
| `gmcore list-versions` | List available framework versions |
| `gmcore self-update` | Update CLI to latest version |
| `gmcore self-update <ver>` | Update to specific version |
| `gmcore version` | Show CLI version |
| `gmcore install` | Install CLI system-wide (requires root) |
| `gmcore uninstall` | Uninstall CLI |
| `gmcore uninstall --purge` | Uninstall and remove all apps |

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
gmcore <command>

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
sudo gmcore uninstall

# Remove CLI and all applications
sudo gmcore uninstall --purge
```

## License

MIT
