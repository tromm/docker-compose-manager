package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// isRateLimited reports whether a docker error message indicates the registry
// throttled the request (Docker Hub anonymous pull/manifest limits).
func isRateLimited(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "toomanyrequests") ||
		strings.Contains(m, "429") ||
		strings.Contains(m, "rate limit")
}

// ImageInfo stores version information for an image
type ImageInfo struct {
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"` // Human-readable current version (tag or label)
	LatestVersion  string `json:"latest_version"`  // Human-readable latest version (best effort)
	HasUpdate      bool   `json:"has_update"`
	State          string `json:"state,omitempty"` // "ok" | "update" | "not-pulled" | "local" | "unknown"
}

// Project represents a Docker Compose project
type Project struct {
	Name              string               `json:"name"`
	Path              string               `json:"path"`
	ComposeFile       string               `json:"compose_file"`
	Status            string               `json:"status"` // "stopped" or "running:N"
	RunningContainers int                  `json:"running_containers"`
	TotalServices     int                  `json:"total_services"` // Number of services defined in compose file
	Images            []string             `json:"images"`
	ImageInfo         map[string]ImageInfo `json:"image_info"` // Map of image name to version info
	HasUpdates        bool                 `json:"has_updates"`
	LastUpdated       time.Time            `json:"last_updated"`
}

// UpdateCount returns how many images in this project have an update available.
func (p *Project) UpdateCount() int {
	n := 0
	for _, img := range p.ImageInfo {
		if img.HasUpdate {
			n++
		}
	}
	return n
}

// ImageCount returns how many images this project has version info for.
func (p *Project) ImageCount() int {
	return len(p.ImageInfo)
}

// UnknownCount returns how many images could not be checked against the registry.
func (p *Project) UnknownCount() int {
	n := 0
	for _, img := range p.ImageInfo {
		if img.State == "unknown" {
			n++
		}
	}
	return n
}

// FullyUnknown reports that the project has images but none could be verified
// (all unreachable) and none have a known update — so it must not be shown as
// "up to date".
func (p *Project) FullyUnknown() bool {
	return len(p.ImageInfo) > 0 && p.UpdateCount() == 0 && p.UnknownCount() == len(p.ImageInfo)
}

// Checked reports whether update information has been gathered for this project.
func (p *Project) Checked() bool {
	return len(p.ImageInfo) > 0
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

	p.TotalServices = p.countServices()

	return nil
}

// countServices returns the number of services defined in the compose file.
// Best effort: returns 0 if it cannot be determined.
func (p *Project) countServices() int {
	cmd := exec.Command("docker", "compose", "config", "--services")
	cmd.Dir = p.Path
	output, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("docker-compose", "config", "--services")
		cmd.Dir = p.Path
		output, err = cmd.Output()
		if err != nil {
			return 0
		}
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
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

// genericTag reports whether a tag is a rolling/generic tag (latest, stable, …)
// rather than a concrete version, in which case we try to resolve a real version
// from image labels.
func genericTag(tag string) bool {
	genericPrefixes := []string{"latest", "stable", "edge", "main", "master", "production", "nightly", "dev", "rc", "develop", "release"}
	for _, prefix := range genericPrefixes {
		if tag == prefix || strings.HasPrefix(tag, prefix+"-") {
			return true
		}
	}
	return false
}

// resolveVersion returns a human-readable version string for an image.
// Lightweight: uses the tag directly for concrete tags, and falls back to
// locally available image labels for generic tags. It never starts a container.
func resolveVersion(imageName string, tagVersion string) string {
	if !genericTag(tagVersion) {
		return tagVersion
	}

	// Generic tag: try to read a real version from the local image's labels.
	if v := versionFromLabels(imageName); v != "" {
		return v
	}
	return tagVersion
}

// versionFromLabels reads standard/common version labels from a local image.
// Returns "" if the image is not present locally or has no version label.
func versionFromLabels(imageName string) string {
	cmd := exec.Command("docker", "image", "inspect", imageName, "--format", "{{json .Config.Labels}}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	var labels map[string]string
	if err := json.Unmarshal(output, &labels); err != nil {
		return ""
	}
	for _, key := range []string{"org.opencontainers.image.version", "version", "VERSION"} {
		if v, ok := labels[key]; ok && v != "" {
			return strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
		}
	}
	return ""
}

// getLocalConfigDigest returns the local image config digest (docker "Id"),
// e.g. "sha256:abcd…". Empty string means the image is not present locally.
func getLocalConfigDigest(imageName string) string {
	cmd := exec.Command("docker", "image", "inspect", imageName, "--format", "{{.Id}}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// manifestEntry mirrors the relevant parts of `docker manifest inspect --verbose`.
// The command returns either a single object (single-arch) or an array (multi-arch).
type manifestEntry struct {
	Descriptor struct {
		Digest   string `json:"digest"`
		Platform struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		} `json:"platform"`
	} `json:"Descriptor"`
	SchemaV2Manifest *manifestConfig `json:"SchemaV2Manifest"`
	OCIManifest      *manifestConfig `json:"OCIManifest"`
}

type manifestConfig struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
}

func (e manifestEntry) configDigest() string {
	if e.SchemaV2Manifest != nil && e.SchemaV2Manifest.Config.Digest != "" {
		return e.SchemaV2Manifest.Config.Digest
	}
	if e.OCIManifest != nil {
		return e.OCIManifest.Config.Digest
	}
	return ""
}

// getRemoteConfigDigest queries the registry for the config digest of an image
// tag WITHOUT pulling it, using `docker manifest inspect --verbose`. For
// multi-arch images the entry matching the host platform is selected.
// Returns an error when the registry cannot be reached / requires auth, so the
// caller can distinguish "no update" from "could not check".
func getRemoteConfigDigest(imageName string) (string, error) {
	var output []byte
	var lastErr error

	// Retry transient failures once; do NOT retry hard rate limits (pointless
	// within the window and only makes throttling worse).
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, "docker", "manifest", "inspect", "--verbose", imageName)
		// manifest inspect must not be treated as experimental-gated on older CLIs.
		cmd.Env = append(os.Environ(), "DOCKER_CLI_EXPERIMENTAL=enabled")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		cancel()
		if err == nil {
			output = out
			break
		}
		lastErr = fmt.Errorf("manifest inspect failed: %v: %s", err, strings.TrimSpace(stderr.String()))
		if isRateLimited(stderr.String()) {
			return "", lastErr
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if output == nil {
		return "", lastErr
	}

	// Try array (multi-arch) first, then single object.
	var entries []manifestEntry
	if err := json.Unmarshal(output, &entries); err != nil {
		var single manifestEntry
		if err := json.Unmarshal(output, &single); err != nil {
			return "", fmt.Errorf("could not parse manifest: %w", err)
		}
		entries = []manifestEntry{single}
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("empty manifest")
	}

	// Prefer the entry matching the host platform.
	for _, e := range entries {
		if e.Descriptor.Platform.Architecture == runtime.GOARCH && e.Descriptor.Platform.OS == runtime.GOOS {
			if d := e.configDigest(); d != "" {
				return d, nil
			}
		}
	}
	// Fall back to the first entry with a config digest.
	for _, e := range entries {
		if d := e.configDigest(); d != "" {
			return d, nil
		}
	}
	return "", fmt.Errorf("no config digest in manifest")
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

		// Try to get real version for generic tags (label-based, no container start)
		currentVersion := resolveVersion(imageName, tagVersion)

		// Store in ImageInfo (without checking for updates)
		p.ImageInfo[imageName] = ImageInfo{
			Name:           imageName,
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion, // Same as current since we're not checking
			HasUpdate:      false,
			State:          "ok",
		}
	}

	return nil
}

// UpdateImageInfo refreshes image version information for this project using a
// lightweight, pull-free check: it compares the local image config digest against
// the registry's config digest (via `docker manifest inspect`). It never pulls
// images and never starts containers, so it is safe and fast for cron use.
func (p *Project) UpdateImageInfo() error {
	if p.ImageInfo == nil {
		p.ImageInfo = make(map[string]ImageInfo)
	}

	images, err := p.GetImages()
	if err != nil {
		return err
	}

	hasUpdates := false

	for _, imageName := range images {
		currentTag := "latest"
		if idx := strings.LastIndex(imageName, ":"); idx >= 0 && !strings.Contains(imageName[idx:], "/") {
			currentTag = imageName[idx+1:]
		}

		currentVersion := resolveVersion(imageName, currentTag)
		localDigest := getLocalConfigDigest(imageName)

		// Image not present locally at all → must be pulled.
		if localDigest == "" {
			p.ImageInfo[imageName] = ImageInfo{
				Name:           imageName,
				CurrentVersion: currentVersion,
				LatestVersion:  "not pulled",
				HasUpdate:      true,
				State:          "not-pulled",
			}
			hasUpdates = true
			continue
		}

		remoteDigest, err := getRemoteConfigDigest(imageName)
		if err != nil {
			// Can't reach registry (offline, auth required, locally-built image).
			// Don't raise a false alarm — mark as "unknown".
			p.ImageInfo[imageName] = ImageInfo{
				Name:           imageName,
				CurrentVersion: currentVersion,
				LatestVersion:  currentVersion,
				HasUpdate:      false,
				State:          "unknown",
			}
			continue
		}

		hasUpdate := remoteDigest != "" && remoteDigest != localDigest
		info := ImageInfo{
			Name:           imageName,
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion,
			HasUpdate:      hasUpdate,
			State:          "ok",
		}
		if hasUpdate {
			info.State = "update"
			info.LatestVersion = "update available"
			hasUpdates = true
		}
		p.ImageInfo[imageName] = info
	}

	p.HasUpdates = hasUpdates
	p.LastUpdated = time.Now()
	return nil
}

// CheckForUpdates checks if updates are available for this project (lightweight)
func (p *Project) CheckForUpdates() (bool, error) {
	return p.HasUpdates, nil
}

// PullOnly pulls latest images without restarting containers
func (p *Project) PullOnly() error {
	// Pull latest images
	cmd := exec.Command("docker", "compose", "pull", "--quiet")
	cmd.Dir = p.Path
	cmd.Stdin = nil                                                           // Prevent docker from detecting TTY
	cmd.Env = append(os.Environ(), "COMPOSE_ANSI=never", "DOCKER_BUILDKIT=0") // Disable ANSI and buildkit output
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "pull", "--quiet")
		cmd.Dir = p.Path
		cmd.Stdin = nil
		cmd.Env = append(os.Environ(), "COMPOSE_ANSI=never", "DOCKER_BUILDKIT=0")
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
	cmd := exec.Command("docker", "compose", "pull", "--quiet")
	cmd.Dir = p.Path
	cmd.Stdin = nil                                                           // Prevent docker from detecting TTY
	cmd.Env = append(os.Environ(), "COMPOSE_ANSI=never", "DOCKER_BUILDKIT=0") // Disable ANSI and buildkit output
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "pull", "--quiet")
		cmd.Dir = p.Path
		cmd.Stdin = nil
		cmd.Env = append(os.Environ(), "COMPOSE_ANSI=never", "DOCKER_BUILDKIT=0")
		output, err = cmd.CombinedOutput()
		if err != nil {
			return cleanDockerError("pull", output, err)
		}
	}

	// Remove orphaned containers first (prevents KeyError: 'ContainerConfig')
	cmd = exec.Command("docker", "compose", "down", "--remove-orphans")
	cmd.Dir = p.Path
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "COMPOSE_ANSI=never")
	cmd.Run() // Ignore errors, this is cleanup

	// Recreate containers with new images
	cmd = exec.Command("docker", "compose", "up", "-d", "--force-recreate", "--remove-orphans")
	cmd.Dir = p.Path
	cmd.Stdin = nil                                                           // Prevent docker from detecting TTY
	cmd.Env = append(os.Environ(), "COMPOSE_ANSI=never", "DOCKER_BUILDKIT=0") // Disable ANSI and buildkit output
	output, err = cmd.CombinedOutput()
	if err != nil {
		// Try docker-compose (v1)
		cmd = exec.Command("docker-compose", "up", "-d", "--force-recreate", "--remove-orphans")
		cmd.Dir = p.Path
		cmd.Stdin = nil
		cmd.Env = append(os.Environ(), "COMPOSE_ANSI=never", "DOCKER_BUILDKIT=0")
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
