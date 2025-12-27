package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ImageInfo stores version information for an image
type ImageInfo struct {
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"` // Current local image ID
	LatestVersion  string `json:"latest_version"`  // Latest available image ID
	HasUpdate      bool   `json:"has_update"`
}

// Project represents a Docker Compose project
type Project struct {
	Name              string                `json:"name"`
	Path              string                `json:"path"`
	ComposeFile       string                `json:"compose_file"`
	Status            string                `json:"status"`           // "stopped" or "running:N"
	RunningContainers int                   `json:"running_containers"`
	Images            []string              `json:"images"`
	ImageInfo         map[string]ImageInfo  `json:"image_info"` // Map of image name to version info
	HasUpdates        bool                  `json:"has_updates"`
	LastUpdated       time.Time             `json:"last_updated"`
}

// IsRunning checks if the project has running containers
func (p *Project) IsRunning() bool {
	return strings.HasPrefix(p.Status, "running:")
}

// StatusDisplay returns a human-readable status
func (p *Project) StatusDisplay() string {
	if p.IsRunning() {
		return fmt.Sprintf("Running (%d)", p.RunningContainers)
	}
	return "Stopped"
}

// FindProjects searches for docker-compose projects in a directory
func FindProjects(searchDir string, maxDepth int) ([]*Project, error) {
	var projects []*Project

	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip directories we can't access
		}

		// Check depth
		relPath, _ := filepath.Rel(searchDir, path)
		depth := len(strings.Split(relPath, string(os.PathSeparator)))
		if depth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Look for docker-compose files (support both old and new naming)
		if !info.IsDir() {
			name := info.Name()
			if name == "docker-compose.yml" || name == "docker-compose.yaml" || name == "compose.yml" || name == "compose.yaml" {
				// Skip control files
				if strings.Contains(name, "control") {
					return nil
				}

				projectDir := filepath.Dir(path)
				projectName := filepath.Base(projectDir)

				project := &Project{
					Name:        projectName,
					Path:        projectDir,
					ComposeFile: path,
					LastUpdated: time.Now(),
				}

				// Get container status
				if err := project.UpdateStatus(); err == nil {
					projects = append(projects, project)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to search for projects: %w", err)
	}

	if len(projects) == 0 {
		return nil, fmt.Errorf("no docker-compose projects found in %s", searchDir)
	}

	return projects, nil
}

// UpdateStatus updates the container status for this project
func (p *Project) UpdateStatus() error {
	cmd := exec.Command("docker", "compose", "ps", "--quiet")
	cmd.Dir = p.Path
	output, err := cmd.Output()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "ps", "--quiet")
		cmd.Dir = p.Path
		output, err = cmd.Output()
		if err != nil {
			p.Status = "stopped"
			p.RunningContainers = 0
			return nil
		}
	}

	// Count running containers
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		p.Status = "stopped"
		p.RunningContainers = 0
		return nil
	}

	cmd = exec.Command("docker", "compose", "ps", "--services", "--filter", "status=running")
	cmd.Dir = p.Path
	output, err = cmd.Output()
	if err != nil {
		cmd = exec.Command("docker-compose", "ps", "--services", "--filter", "status=running")
		cmd.Dir = p.Path
		output, _ = cmd.Output()
	}

	running := 0
	lines = strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		running = len(lines)
	}

	p.RunningContainers = running
	if running > 0 {
		p.Status = fmt.Sprintf("running:%d", running)
	} else {
		p.Status = "stopped"
	}

	return nil
}

// Start starts the containers
func (p *Project) Start() error {
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = p.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "up", "-d")
		cmd.Dir = p.Path
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to start: %s", string(output))
		}
	}

	// Update status
	return p.UpdateStatus()
}

// Stop stops the containers
func (p *Project) Stop() error {
	cmd := exec.Command("docker", "compose", "down")
	cmd.Dir = p.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "down")
		cmd.Dir = p.Path
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to stop: %s", string(output))
		}
	}

	// Update status
	p.Status = "stopped"
	p.RunningContainers = 0
	return nil
}

// Restart restarts the containers
func (p *Project) Restart() error {
	cmd := exec.Command("docker", "compose", "restart")
	cmd.Dir = p.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "restart")
		cmd.Dir = p.Path
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to restart: %s", string(output))
		}
	}

	// Update status
	return p.UpdateStatus()
}

// GetImages returns the list of images used by this project
func (p *Project) GetImages() ([]string, error) {
	cmd := exec.Command("docker", "compose", "config", "--images")
	cmd.Dir = p.Path
	output, err := cmd.Output()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "config", "--images")
		cmd.Dir = p.Path
		output, err = cmd.Output()
		if err != nil {
			return nil, err
		}
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var images []string
	for _, line := range lines {
		if line != "" {
			images = append(images, line)
		}
	}

	p.Images = images
	return images, nil
}

// getRealVersion attempts to get the actual version for images tagged as "latest" or similar
func getRealVersion(imageName string, tagVersion string) string {
	// If not a generic tag, return as-is
	genericTags := []string{"latest", "stable", "edge", "main", "master"}
	isGeneric := false
	for _, tag := range genericTags {
		if tagVersion == tag {
			isGeneric = true
			break
		}
	}
	if !isGeneric {
		return tagVersion
	}

	// Try running container with --version flag first (with 5 second timeout)
	// This is more reliable than labels as labels might show base OS version
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", imageName, "--version")
	output, err := cmd.Output()
	if err == nil {
		// Parse version from output (usually first line, first word that looks like a version)
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) > 0 {
			// Look for version pattern in first few lines
			for _, line := range lines[:min(3, len(lines))] {
				// Match patterns like "Version: 18.7.1", "18.7.1", "v2.1.4", etc.
				line = strings.TrimSpace(line)
				if strings.Contains(strings.ToLower(line), "version:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						version := strings.TrimSpace(parts[1])
						if version != "" {
							return version
						}
					}
				}
				// Try to extract version number directly
				fields := strings.Fields(line)
				for _, field := range fields {
					field = strings.TrimPrefix(field, "v")
					field = strings.TrimPrefix(field, "V")
					// Check if it looks like a version (has digits and dots)
					if strings.Contains(field, ".") && len(field) > 0 && (field[0] >= '0' && field[0] <= '9') {
						return field
					}
				}
			}
		}
	}

	// Fallback: Try to get version from image labels
	cmd = exec.Command("docker", "image", "inspect", imageName, "--format", "{{json .Config.Labels}}")
	output, err = cmd.Output()
	if err == nil {
		var labels map[string]string
		if err := json.Unmarshal(output, &labels); err == nil {
			// Check standard OCI labels
			if version, ok := labels["org.opencontainers.image.version"]; ok && version != "" && version != tagVersion {
				return version
			}
			// Check common custom labels
			if version, ok := labels["version"]; ok && version != "" {
				return version
			}
			if version, ok := labels["VERSION"]; ok && version != "" {
				return version
			}
		}
	}

	// If all else fails, return the tag as-is
	return tagVersion
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetRunningContainerInfo gets version info from currently running containers
// This is used for display purposes when ImageInfo cache is not available
func (p *Project) GetRunningContainerInfo() error {
	if p.ImageInfo == nil {
		p.ImageInfo = make(map[string]ImageInfo)
	}

	// Get running containers for this project using docker ps
	// Format: container_name, image
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("label=com.docker.compose.project=%s", p.Name), "--format", "{{.Image}}")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	// Process each image
	for _, imageName := range lines {
		imageName = strings.TrimSpace(imageName)
		if imageName == "" {
			continue
		}

		// Extract version tag from image name
		tagVersion := "latest"
		if strings.Contains(imageName, ":") {
			parts := strings.Split(imageName, ":")
			tagVersion = parts[len(parts)-1]
		}

		// Try to get real version for generic tags
		currentVersion := getRealVersion(imageName, tagVersion)

		// Store in ImageInfo (without checking for updates)
		p.ImageInfo[imageName] = ImageInfo{
			Name:           imageName,
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion, // Same as current since we're not checking
			HasUpdate:      false,
		}
	}

	return nil
}

// UpdateImageInfo updates the image version information for this project
func (p *Project) UpdateImageInfo() error {
	if p.ImageInfo == nil {
		p.ImageInfo = make(map[string]ImageInfo)
	}

	// Get images from compose file
	images, err := p.GetImages()
	if err != nil {
		return err
	}

	hasUpdates := false

	for _, imageName := range images {
		// Extract tag from image name (e.g., "postgres:15" -> "15")
		currentTag := "latest"
		if strings.Contains(imageName, ":") {
			parts := strings.Split(imageName, ":")
			currentTag = parts[len(parts)-1]
		}

		// Get current local image ID (to compare if update available)
		cmd := exec.Command("docker", "images", imageName, "--format", "{{.ID}}")
		output, err := cmd.Output()
		currentID := strings.TrimSpace(string(output))

		if err != nil || currentID == "" {
			// Image not pulled yet
			p.ImageInfo[imageName] = ImageInfo{
				Name:           imageName,
				CurrentVersion: currentTag,
				LatestVersion:  "not pulled",
				HasUpdate:      true,
			}
			hasUpdates = true
			continue
		}

		// Get latest image ID from registry (pull with timeout)
		// Use a 2-minute timeout per image to prevent hanging
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cmd = exec.CommandContext(ctx, "docker", "pull", "--quiet", imageName)
		cmd.Dir = p.Path
		output, err = cmd.CombinedOutput()
		latestID := ""

		if err == nil {
			// Get the ID of the pulled image
			cmd = exec.Command("docker", "images", imageName, "--format", "{{.ID}}")
			output, _ = cmd.Output()
			latestID = strings.TrimSpace(string(output))
		} else if ctx.Err() == context.DeadlineExceeded {
			// Timeout occurred
			p.ImageInfo[imageName] = ImageInfo{
				Name:           imageName,
				CurrentVersion: currentTag,
				LatestVersion:  "timeout",
				HasUpdate:      false,
			}
			continue
		}

		hasUpdate := currentID != latestID && latestID != ""

		// Always show tag names, not IDs
		// For "latest" or "stable" tags, we show the tag even if there's an update
		latestVersion := currentTag
		if hasUpdate {
			// For versioned tags like "1.2.3", show "newer available"
			// For latest/stable, just indicate update
			latestVersion = currentTag + " (newer)"
		}

		p.ImageInfo[imageName] = ImageInfo{
			Name:           imageName,
			CurrentVersion: currentTag,
			LatestVersion:  latestVersion,
			HasUpdate:      hasUpdate,
		}

		if hasUpdate {
			hasUpdates = true
		}
	}

	p.HasUpdates = hasUpdates
	return nil
}

// CheckForUpdates checks if updates are available for this project (lightweight)
func (p *Project) CheckForUpdates() (bool, error) {
	return p.HasUpdates, nil
}

// PullOnly pulls latest images without restarting containers
func (p *Project) PullOnly() error {
	// Pull latest images
	cmd := exec.Command("docker", "compose", "pull")
	cmd.Dir = p.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "pull")
		cmd.Dir = p.Path
		output, err = cmd.CombinedOutput()
		if err != nil {
			return cleanDockerError("pull", output, err)
		}
	}

	// Update status
	return p.UpdateStatus()
}

// Update performs a pull and recreate for this project
func (p *Project) Update() error {
	// Pull latest images
	cmd := exec.Command("docker", "compose", "pull")
	cmd.Dir = p.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "pull")
		cmd.Dir = p.Path
		output, err = cmd.CombinedOutput()
		if err != nil {
			return cleanDockerError("pull", output, err)
		}
	}

	// Remove orphaned containers first (prevents KeyError: 'ContainerConfig')
	cmd = exec.Command("docker", "compose", "down", "--remove-orphans")
	cmd.Dir = p.Path
	cmd.Run() // Ignore errors, this is cleanup

	// Recreate containers with new images
	cmd = exec.Command("docker", "compose", "up", "-d", "--force-recreate", "--remove-orphans")
	cmd.Dir = p.Path
	output, err = cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "up", "-d", "--force-recreate", "--remove-orphans")
		cmd.Dir = p.Path
		output, err = cmd.CombinedOutput()
		if err != nil {
			return cleanDockerError("recreate", output, err)
		}
	}

	// Update status
	p.HasUpdates = false
	p.LastUpdated = time.Now()
	return p.UpdateStatus()
}

// cleanDockerError cleans up docker error messages for display
func cleanDockerError(operation string, output []byte, err error) error {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var relevantLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip progress bars and informational output
		if strings.Contains(line, "Pulling") ||
		   strings.Contains(line, "Downloaded") ||
		   strings.Contains(line, "Digest:") ||
		   strings.Contains(line, "Status:") ||
		   strings.Contains(line, "Waiting") ||
		   strings.Contains(line, "Extracting") {
			continue
		}

		// Skip Python traceback noise
		if strings.HasPrefix(line, "Traceback") ||
		   strings.HasPrefix(line, "File ") ||
		   strings.Contains(line, "raise error_to_reraise") ||
		   strings.Contains(line, "raise err from") ||
		   (strings.Contains(line, "line ") && strings.Contains(line, ".py")) {
			continue
		}

		// Extract just the error type and message from Python exceptions
		// e.g., "KeyError: 'ContainerConfig'" from full traceback
		if strings.Contains(line, "Error:") || strings.Contains(line, "Exception:") {
			// Extract error type and message
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				errorType := strings.TrimSpace(parts[0])
				errorMsg := strings.TrimSpace(parts[1])
				// Clean up the error type (remove path if present)
				if idx := strings.LastIndex(errorType, "."); idx != -1 {
					errorType = errorType[idx+1:]
				}
				relevantLines = []string{errorType + ": " + errorMsg}
				break // Found the actual error, stop processing
			}
		}

		// Look for actual error messages (but not tracebacks)
		if (strings.Contains(line, "Error") ||
		    strings.Contains(line, "error") ||
		    strings.Contains(line, "failed") ||
		    strings.Contains(line, "cannot")) &&
		   !strings.Contains(line, "Traceback") {
			relevantLines = append(relevantLines, line)
		}
	}

	// If no error lines found, show last non-empty line
	if len(relevantLines) == 0 {
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i] != "" {
				relevantLines = []string{lines[i]}
				break
			}
		}
	}

	// Show only the last/most relevant error line
	if len(relevantLines) > 1 {
		relevantLines = relevantLines[len(relevantLines)-1:]
	}

	if len(relevantLines) > 0 {
		errorMsg := relevantLines[0]
		// Remove docker-compose command echo if present
		if strings.Contains(errorMsg, "docker") && (strings.Contains(errorMsg, "Error") || strings.Contains(errorMsg, "error")) {
			if idx := strings.Index(errorMsg, "Error"); idx != -1 {
				errorMsg = errorMsg[idx:]
			} else if idx := strings.Index(errorMsg, "error"); idx != -1 {
				errorMsg = errorMsg[idx:]
			}
		}
		return fmt.Errorf("%s failed: %s", operation, errorMsg)
	}
	return fmt.Errorf("%s failed: %v", operation, err)
}

// SaveToCache saves projects to cache file
func SaveToCache(projects []*Project, cacheFile string) error {
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal projects: %w", err)
	}

	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// LoadFromCache loads projects from cache file
func LoadFromCache(cacheFile string, maxAge time.Duration) ([]*Project, error) {
	info, err := os.Stat(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("cache file not found")
	}

	// Check if cache is too old
	if time.Since(info.ModTime()) > maxAge {
		return nil, fmt.Errorf("cache expired")
	}

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache: %w", err)
	}

	var projects []*Project
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return projects, nil
}
