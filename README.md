# Docker Compose Manager

A modern, terminal-based UI for managing Docker Compose projects. Built with Go for portability and performance.

## Features

- 🚀 **Single Binary** - No runtime dependencies, just copy and run
- 🎨 **Beautiful TUI** - Modern terminal UI with intuitive navigation
- ⚡ **Fast** - JSON cache for instant startup (95%+ faster than bash version)
- 🐳 **Docker Compose v1 & v2** - Automatically detects and supports both versions
- 📦 **Container Management** - Start, stop, and restart containers with ease
- 🔄 **Update Management** - Pull latest images and recreate containers
- ✅ **Multi-Select Updates** - Select multiple projects to update at once
- 📊 **Progress Tracking** - Real-time feedback during updates
- 🔙 **Smart Navigation** - Escape/Back buttons work intuitively
- 🌐 **Cross-Platform** - Runs on Linux, macOS, ARM, x86

## Quick Start

### Prerequisites

- Docker and Docker Compose installed
- Linux or macOS (for pre-built binaries)
- Go 1.21+ (only for building from source)

### Installation

#### Option 1: Quick Install (Recommended)

Install the latest release with a single command:

```bash
curl -sSL https://raw.githubusercontent.com/tromm/docker-compose-manager/main/install.sh | sudo bash
```

This will:
- Detect your OS and architecture automatically
- Download the latest release binary
- Install to `/usr/local/bin/docker-compose-manager`
- Create cache directory at `/var/cache/docker-compose-manager`
- Optionally set up a cron job for automatic update checks

**Supported platforms:**
- Linux: AMD64, ARM64
- macOS: Intel (AMD64), Apple Silicon (ARM64)

#### Option 2: Manual Download

Download the binary for your platform from the [Releases](https://github.com/tromm/docker-compose-manager/releases) page:

```bash
# Example for Linux AMD64
curl -LO https://github.com/tromm/docker-compose-manager/releases/latest/download/docker-compose-manager-linux-amd64
chmod +x docker-compose-manager-linux-amd64
sudo mv docker-compose-manager-linux-amd64 /usr/local/bin/docker-compose-manager
```

#### Option 3: Build from Source

```bash
# Clone the repository
git clone https://github.com/tromm/docker-compose-manager.git
cd docker-compose-manager

# Build
make build

# Install system-wide (optional)
make install

# Run
./docker-compose-manager
```

### Usage

```bash
# Use default directory (/home/dockeruser/docker)
./docker-compose-manager

# Specify custom directory
./docker-compose-manager /path/to/docker/projects

# List all projects (non-interactive)
./docker-compose-manager --list

# Update cache with latest image versions (for cron)
./docker-compose-manager --update-cache
```

### Cron Job for Automatic Update Checks

To automatically check for updates in the background, add this to your crontab:

```bash
# Check for updates every 6 hours
0 */6 * * * /path/to/docker-compose-manager --update-cache

# Or check daily at 2 AM
0 2 * * * /path/to/docker-compose-manager --update-cache
```

The `--update-cache` mode:
- Runs with live progress display (perfect for cron jobs)
- Checks all images for available updates (2-minute timeout per image)
- Saves results incrementally to cache file after each project
- Next time you run the TUI, it will use cached update data

### Cache Location

The cache is stored in:
- **System-wide**: `/var/cache/docker-compose-manager/cache.json` (preferred, requires write permissions)
- **User-specific**: `~/.cache/docker-compose-manager/cache.json` (fallback if system cache not writable)

For system-wide installation with cron jobs, ensure the cache directory has proper permissions:
```bash
sudo mkdir -p /var/cache/docker-compose-manager
sudo chown $USER:$USER /var/cache/docker-compose-manager
```

## Navigation

- **↑/↓ or k/j** - Navigate menu items
- **Space** - Toggle selection (in update list)
- **a** - Select all / Deselect all (in update list)
- **r** - Refresh update check (in update list)
- **Enter** - Select item / Confirm
- **Esc or q** - Go back / Exit (with confirmation in main menu)
- **Ctrl+C** - Force quit

## Menu Structure

```
Main Menu
├── [1] Manage Containers
│   ├── Select Project
│   └── Choose Action (Start/Stop/Restart)
├── [2] Perform Updates
│   ├── Select Projects (multi-select with Space)
│   ├── Choose Update Mode
│   │   ├── Pull Images Only (no restart)
│   │   └── Pull Images & Restart Containers
│   ├── Confirm Restart Selection (if restart mode chosen)
│   └── View Progress
└── [3] Help & Documentation
```

**Direct Selection**: Press `1`, `2`, or `3` from the main menu to jump directly to that option.

## Performance

The cache system stores project metadata in `~/.docker-compose-manager-cache.json`:

- **First run**: Scans all projects (~2-5 seconds for 30+ projects)
- **Cached runs**: Instant startup (< 100ms)
- **Cache lifetime**: 1 hour (configurable)

## Building for Different Platforms

```bash
# Linux (amd64)
make build-linux

# Linux (arm64) - for Raspberry Pi, ARM servers
make build-linux-arm

# macOS (Intel)
make build-macos

# macOS (Apple Silicon)
make build-macos-arm

# All platforms
make build-all
```

## Project Structure

```
.
├── cmd/
│   └── main.go           # Entry point
├── internal/
│   ├── docker/
│   │   └── project.go    # Docker Compose operations
│   └── ui/
│       └── model.go      # Bubbletea TUI
├── go.mod
├── Makefile
└── README.md
```

## Why Go Instead of Bash?

### Problems with Bash Version:
- `set -e` causes unpredictable exits
- Complex error handling
- Difficult to test
- Platform-specific quirks
- Requires external tools (dialog/whiptail, jq)

### Benefits of Go Version:
- ✅ Explicit error handling - no mysterious exits
- ✅ Single binary - no dependencies
- ✅ Cross-platform - works everywhere
- ✅ Better performance
- ✅ Easier to maintain and extend
- ✅ Built-in JSON support
- ✅ Beautiful TUI with bubbletea

## Development

### Running Locally

```bash
# Run without building
go run ./cmd

# Run with custom directory
go run ./cmd /path/to/projects

# Build and run
make run
```

### Testing

```bash
# Format code
go fmt ./...

# Check for issues
go vet ./...

# Run tests (when added)
go test ./...
```

## Configuration

Default values can be changed in `cmd/main.go`:

```go
const (
    defaultSearchDir = "/home/dockeruser/docker"  // Default search directory
    defaultMaxDepth  = 10                          // Max recursion depth
    cacheMaxAge      = 1 * time.Hour              // Cache lifetime
)
```

## Troubleshooting

### "No docker-compose projects found"
- Check that the directory contains `docker-compose.yml` or `docker-compose.yaml` files
- Try specifying the directory explicitly: `./docker-compose-manager /your/path`

### "Permission denied" errors
- Ensure Docker is running
- Check that your user is in the `docker` group: `sudo usermod -aG docker $USER`
- Log out and back in for group changes to take effect

### Cache issues
- Delete cache file: `rm ~/.docker-compose-manager-cache.json`
- Cache auto-refreshes after 1 hour

## License

MIT License - see LICENSE file for details

## Contributing

Pull requests welcome! Please ensure:
- Code is formatted (`go fmt`)
- No lint errors (`go vet`)
- Commit messages are clear

## Creating a Release

Releases are automated via GitHub Actions. To create a new release:

```bash
# Make sure all changes are committed
git add .
git commit -m "Release preparation"

# Create and push a version tag
make release VERSION=v1.0.0

# Or manually:
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

GitHub Actions will automatically:
- Build binaries for all platforms (Linux/macOS, AMD64/ARM64)
- Create checksums
- Create a GitHub release with all binaries attached
- Include the install.sh script

Users can then install via:
```bash
curl -sSL https://raw.githubusercontent.com/tromm/docker-compose-manager/main/install.sh | sudo bash
```

## Roadmap

- [✓] Update management (pull images, recreate containers) - **DONE**
- [✓] Multi-select for updates - **DONE**
- [✓] Cache-based update checking with cron support - **DONE**
- [✓] Manual refresh in update screen - **DONE**
- [ ] Project details view (show images, volumes, networks)
- [ ] Logs viewer
- [ ] Container restart policies management
- [ ] Export/import configuration
- [ ] Multi-project operations (start/stop all)
- [ ] Search/filter projects

## Credits

Built with:
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling

---

**Note**: This is a complete rewrite of the bash version in Go for better reliability and portability.
