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

### How Updates Are Detected (lightweight, no pull)

The update check is **pull-free**. For each image it compares the *local image
config digest* (`docker image inspect --format '{{.Id}}'`) with the *registry's
config digest* obtained via `docker manifest inspect` — matched to the host
platform. If they differ, an update is available. Nothing is downloaded and no
container is started, so the check is fast and safe to run from cron.

Image states shown in the UI:
- `update` — a newer image exists in the registry
- `not-pulled` — the image is referenced but not present locally
- `unknown` — the registry could not be queried (offline, auth required, or a
  locally-built image) — deliberately **not** flagged as an update
- `ok` — up to date

### Cron Job for Automatic Update Checks

The cache is kept fresh by a cron job. `make install` prints the exact line; it
looks like this (note the explicit `--cache`, which guarantees cron and the TUI
use the **same** file, and logging instead of `>/dev/null`):

```bash
# Refresh available updates every 6 hours (run as the user that owns the cache)
0 */6 * * * /usr/local/bin/docker-compose-manager --update-cache \
    --cache /var/cache/docker-compose-manager/cache.json \
    /home/dockeruser/docker/ >> $HOME/.cache/dcm-cron.log 2>&1
```

The `--update-cache` mode:
- Refreshes the cache with available updates using the pull-free digest check
- Saves results incrementally after each project
- Makes the update counts appear instantly on the next TUI start

### Cache Location

Resolved deterministically (highest precedence first):
1. `--cache PATH` flag
2. `$DCM_CACHE` environment variable
3. `/var/cache/docker-compose-manager/cache.json` (if writable by the current user)
4. `~/.cache/docker-compose-manager/cache.json` (fallback)

> **Important:** cron and the interactive TUI must resolve to the **same** file.
> Historically this broke because a root cron wrote `/var/cache` while the user
> read `~/.cache` (split-brain), and the cron pointed at a non-existent binary
> path. `make install` fixes this by installing to `/usr/local/bin`, chowning
> the cache dir to the running user, and printing a cron line with explicit
> `--cache`.

```bash
sudo mkdir -p /var/cache/docker-compose-manager
sudo chown $USER:$USER /var/cache/docker-compose-manager
```

## Navigation

- **↑/↓ or k/j** - Navigate menu items
- **Space or 1-9/0** - Toggle selection (in update list)
- **a** - Select / deselect all visible projects (in update list)
- **o** - Select only projects that have updates (in update list)
- **f** - Filter: show only projects with updates (in update list)
- **u** - Refresh update check (in update list)
- **Enter** - Select item / Confirm
- **Esc or q** - Go back / Exit (with confirmation in main menu)
- **Ctrl+C** - Force quit

The lists are **responsive** (k9s-style): one row per project, a coloured status
glyph (⬆ update · ✓ ok · ○ stopped · ? unchecked), and columns that drop by
priority on narrow terminals so important information is never lost.

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

The cache stores project metadata and update state (see [Cache Location](#cache-location)):

- **First run**: Scans all projects (~2-5 seconds for 30+ projects)
- **Cached runs**: Instant startup (< 100ms) — update counts show immediately on the main menu
- **Cache lifetime**: 24 hours (`cacheMaxAge` in `cmd/main.go`); refreshed by the cron job

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
    cacheMaxAge      = 24 * time.Hour             // Cache lifetime
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
- Delete cache file: `rm /var/cache/docker-compose-manager/cache.json` (or `~/.cache/docker-compose-manager/cache.json`)
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
- [✓] Filter projects with updates (update screen: 'f' / 'o') - **DONE**
- [ ] Full-text search across projects

## Credits

Built with:
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling

---

**Note**: This is a complete rewrite of the bash version in Go for better reliability and portability.
