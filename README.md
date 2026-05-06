# GMCore CLI

Cross-platform command-line tool for creating and managing GMCore applications.

## Features

- Create new GMCore applications from manifest recipes
- List and manage installed applications
- Check application status
- Start/stop/restart/reload applications
- Install/uninstall as system service
- Self-update to latest version
- List available framework versions
- Exposure defaults for direct mode or gateway-behind-UDS mode
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

# Lifecycle
gmcore start myapp
gmcore reload myapp
gmcore restart myapp
gmcore stop myapp

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
| `gmcore start [appname]` | Start an application |
| `gmcore stop [appname]` | Stop an application |
| `gmcore restart [appname]` | Restart an application |
| `gmcore reload [appname]` | Reload an application |

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

### Service Management (Linux)

| Command | Description |
|---------|-------------|
| `gmcore install <appname>` | Install app as systemd service |
| `gmcore uninstall <appname>` | Remove app service |

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
│   ├── run/               # PID/runtime files
│   ├── socket/            # UDS sockets
│   └── tmp/               # Temporary files
├── vendor/gmcore/         # Framework source
├── migrations/            # Database migrations
└── tests/                 # Test files
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

## Exposure Modes

Each app supports two runtime exposure modes:

- `direct`: app listens on TCP/IP host+port
- `internal`: app listens on UDS and is served behind `gateway`

`gateway` is a normal app at `/opt/gmcore/gateway` and is the internet edge reverse proxy on ports `80/443`.

Default files created for new apps:

- `config/transport.yaml`
- `config/exposure.yaml`
- `config/gateway.yaml` (gateway app)

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
