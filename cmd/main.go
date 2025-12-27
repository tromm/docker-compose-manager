package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/skpharma/docker-compose-manager/internal/docker"
	"github.com/skpharma/docker-compose-manager/internal/ui"
)

const (
	defaultSearchDir = "/home/dockeruser/docker"
	defaultMaxDepth  = 10
	cacheMaxAge      = 1 * time.Hour
)

// getCacheFile returns the cache file path
// Tries system-wide cache first, falls back to user cache
func getCacheFile() string {
	// Try system-wide cache first (requires root or proper permissions)
	systemCacheDir := "/var/cache/docker-compose-manager"
	systemCacheFile := filepath.Join(systemCacheDir, "cache.json")

	// Check if we can write to system cache
	if err := os.MkdirAll(systemCacheDir, 0755); err == nil {
		// Try to create a test file to verify write permissions
		testFile := filepath.Join(systemCacheDir, ".test")
		if f, err := os.Create(testFile); err == nil {
			f.Close()
			os.Remove(testFile)
			return systemCacheFile
		}
	}

	// Fall back to user cache directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to get home directory: %v\n", err)
		os.Exit(1)
	}

	userCacheDir := filepath.Join(homeDir, ".cache", "docker-compose-manager")
	if err := os.MkdirAll(userCacheDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create cache directory: %v\n", err)
		os.Exit(1)
	}

	return filepath.Join(userCacheDir, "cache.json")
}

func main() {
	// Get cache file path
	// Try system-wide cache first (/var/cache), fall back to user cache (~/.cache)
	cacheFile := getCacheFile()

	// Check for flags
	listMode := false
	updateCacheMode := false
	searchDir := defaultSearchDir

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--list" || arg == "-l" {
			listMode = true
		} else if arg == "--update-cache" {
			updateCacheMode = true
		} else if arg == "--help" || arg == "-h" {
			fmt.Println("Docker Compose Manager")
			fmt.Println("\nUsage:")
			fmt.Println("  docker-compose-manager [OPTIONS] [DIRECTORY]")
			fmt.Println("\nOptions:")
			fmt.Println("  -l, --list         List all projects and their status (non-interactive)")
			fmt.Println("  --update-cache     Update cache with latest image versions (for cron)")
			fmt.Println("  -h, --help         Show this help message")
			fmt.Println("\nExamples:")
			fmt.Println("  docker-compose-manager")
			fmt.Println("  docker-compose-manager /path/to/docker")
			fmt.Println("  docker-compose-manager --list")
			fmt.Println("  docker-compose-manager --update-cache  # For cron job")
			os.Exit(0)
		} else {
			searchDir = arg
		}
	}

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
	model := ui.NewModel(projects)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
