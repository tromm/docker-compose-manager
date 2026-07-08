package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/skpharma/docker-compose-manager/internal/docker"
	"github.com/skpharma/docker-compose-manager/internal/ui"
)

const (
	defaultSearchDir = "/home/dockeruser/docker"
	defaultMaxDepth  = 10
	cacheMaxAge      = 24 * time.Hour // Cache valid for 24 hours
)

// getCacheFile resolves the cache file path deterministically so that the cron
// job (`--update-cache`) and the interactive TUI always agree on ONE file.
//
// Precedence:
//  1. explicit --cache PATH  (passed in via `explicit`)
//  2. $DCM_CACHE env var
//  3. system-wide /var/cache/docker-compose-manager/cache.json (if writable)
//  4. user cache ~/.cache/docker-compose-manager/cache.json
//
// To avoid the historic "split-brain" cache (cron writing /var/cache as root
// while the user reads ~/.cache), install.sh chowns the system cache dir to the
// user that runs both the cron job and the TUI.
func getCacheFile(explicit string) string {
	if explicit != "" {
		ensureParentDir(explicit)
		return explicit
	}
	if env := os.Getenv("DCM_CACHE"); env != "" {
		ensureParentDir(env)
		return env
	}

	// System-wide cache, but only if this user can actually write it.
	systemCacheDir := "/var/cache/docker-compose-manager"
	systemCacheFile := filepath.Join(systemCacheDir, "cache.json")
	if err := os.MkdirAll(systemCacheDir, 0o755); err == nil {
		testFile := filepath.Join(systemCacheDir, ".test")
		if f, err := os.Create(testFile); err == nil {
			f.Close()
			os.Remove(testFile)
			return systemCacheFile
		}
	}

	// Fall back to user cache directory.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to get home directory: %v\n", err)
		os.Exit(1)
	}
	userCacheDir := filepath.Join(homeDir, ".cache", "docker-compose-manager")
	if err := os.MkdirAll(userCacheDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create cache directory: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(userCacheDir, "cache.json")
}

// ensureParentDir creates the parent directory of a cache file path.
func ensureParentDir(path string) {
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0o755)
	}
}

func main() {
	// Check for flags
	listMode := false
	updateCacheMode := false
	debugMode := false
	searchDir := defaultSearchDir
	cachePath := "" // explicit --cache PATH override

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--list" || arg == "-l" {
			listMode = true
		} else if arg == "--update-cache" {
			updateCacheMode = true
		} else if arg == "--debug" || arg == "-d" {
			debugMode = true
		} else if arg == "--cache" {
			// consume next arg as the cache file path
			if i+1 < len(os.Args) {
				cachePath = os.Args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "--cache=") {
			cachePath = strings.TrimPrefix(arg, "--cache=")
		} else if arg == "--help" || arg == "-h" {
			fmt.Println("Docker Compose Manager")
			fmt.Println("\nUsage:")
			fmt.Println("  docker-compose-manager [OPTIONS] [DIRECTORY]")
			fmt.Println("\nOptions:")
			fmt.Println("  -l, --list         List all projects and their status (non-interactive)")
			fmt.Println("  --update-cache     Refresh cache with available image updates (for cron)")
			fmt.Println("  --cache PATH       Use an explicit cache file (or set $DCM_CACHE)")
			fmt.Println("  -d, --debug        Enable debug logging to ~/docker-compose-manager-debug.log")
			fmt.Println("  -h, --help         Show this help message")
			fmt.Println("\nExamples:")
			fmt.Println("  docker-compose-manager")
			fmt.Println("  docker-compose-manager /path/to/docker")
			fmt.Println("  docker-compose-manager --list")
			fmt.Println("  docker-compose-manager --debug")
			fmt.Println("  docker-compose-manager --update-cache  # For cron job")
			os.Exit(0)
		} else {
			searchDir = arg
		}
	}

	// Resolve the cache file path deterministically (see getCacheFile).
	cacheFile := getCacheFile(cachePath)

	// Check if search directory exists
	if _, err := os.Stat(searchDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: directory does not exist: %s\n", searchDir)
		os.Exit(1)
	}

	// Try to load from cache first
	var projects []*docker.Project
	projects, err := docker.LoadFromCache(cacheFile, cacheMaxAge)

	if err != nil {
		// Cache miss or expired - scan for projects
		fmt.Println("🔍 Scanning for Docker Compose projects...")
		fmt.Printf("   Directory: %s\n", searchDir)

		projects, err = docker.FindProjects(searchDir, defaultMaxDepth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("✓ Found %d projects\n", len(projects))

		// Save to cache
		if err := docker.SaveToCache(projects, cacheFile); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save cache: %v\n", err)
		}

		// Small delay so user can see the message
		time.Sleep(500 * time.Millisecond)
	}

	// Update cache mode - check for updates and save to cache (for cron)
	if updateCacheMode {
		fmt.Printf("🔍 Checking for updates...\n")
		fmt.Printf("Cache location: %s\n", cacheFile)
		fmt.Printf("Found %d projects\n\n", len(projects))

		for i, p := range projects {
			fmt.Printf("[%d/%d] %-20s ", i+1, len(projects), p.Name)
			os.Stdout.Sync() // Flush output immediately

			err := p.UpdateImageInfo()
			if err != nil {
				fmt.Printf("❌ error: %v\n", err)
			} else {
				updateCount := 0
				for _, img := range p.ImageInfo {
					if img.HasUpdate {
						updateCount++
					}
				}
				if updateCount > 0 {
					fmt.Printf("✓ %d update(s) available\n", updateCount)
				} else {
					fmt.Printf("✓ up to date\n")
				}
			}

			// Save cache incrementally after each project
			if err := docker.SaveToCache(projects, cacheFile); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save cache: %v\n", err)
			}
		}

		fmt.Printf("\n✓ Cache updated successfully: %s\n", cacheFile)
		os.Exit(0)
	}

	// List mode - just print projects and exit
	if listMode {
		fmt.Println("\nDocker Compose Projects:")
		fmt.Println("========================")
		for i, p := range projects {
			status := "Stopped"
			if p.IsRunning() {
				status = fmt.Sprintf("Running (%d)", p.RunningContainers)
			}
			fmt.Printf("%2d. %-20s  %s\n", i+1, p.Name, status)
			fmt.Printf("    Path: %s\n", p.Path)
		}
		fmt.Printf("\nTotal: %d projects\n", len(projects))
		os.Exit(0)
	}

	// Create and run the TUI
	if debugMode {
		homeDir, _ := os.UserHomeDir()
		logFile := filepath.Join(homeDir, "docker-compose-manager-debug.log")
		fmt.Printf("🐛 Debug mode enabled - logging to: %s\n", logFile)
		time.Sleep(1 * time.Second)
	}

	model := ui.NewModel(projects, cacheFile, debugMode)

	// Use inline mode instead of alt screen to avoid diff-rendering artifacts
	// This forces a full redraw on every update
	p := tea.NewProgram(
		model,
		// REMOVED: tea.WithAltScreen() - causes rendering artifacts
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
