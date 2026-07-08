package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/skpharma/docker-compose-manager/internal/docker"
)

func fakeProjects() []*docker.Project {
	mk := func(name, status string, running, total int, imgs map[string]docker.ImageInfo) *docker.Project {
		return &docker.Project{
			Name:              name,
			Status:            status,
			RunningContainers: running,
			TotalServices:     total,
			ImageInfo:         imgs,
		}
	}
	img := func(name, ver, state string, upd bool) docker.ImageInfo {
		return docker.ImageInfo{Name: name, CurrentVersion: ver, LatestVersion: ver, HasUpdate: upd, State: state}
	}
	return []*docker.Project{
		mk("keycloak", "running:1", 1, 1, map[string]docker.ImageInfo{
			"quay.io/keycloak/keycloak:26.0": img("quay.io/keycloak/keycloak:26.0", "26.0", "update", true),
		}),
		mk("traefik", "running:1", 1, 1, map[string]docker.ImageInfo{
			"traefik:v3.1": img("traefik:v3.1", "v3.1", "ok", false),
		}),
		mk("shlink", "running:3", 3, 3, map[string]docker.ImageInfo{
			"shlinkio/shlink:stable": img("shlinkio/shlink:stable", "stable", "update", true),
			"mariadb:11":             img("mariadb:11", "11", "update", true),
			"redis:7":                img("redis:7", "7", "ok", false),
		}),
		mk("peertube-with-a-long-name", "stopped", 0, 2, map[string]docker.ImageInfo{
			"chocobozzz/peertube:production-bookworm": img("chocobozzz/peertube:production-bookworm", "production-bookworm", "ok", false),
			"myregistry.local/private/app:1.0":        img("myregistry.local/private/app:1.0", "1.0", "unknown", false),
		}),
		mk("uncheckedproj", "running:1", 1, 1, nil),
	}
}

func TestRenderAtWidths(t *testing.T) {
	for _, w := range []int{140, 100, 70, 60} {
		for _, sc := range []struct {
			name   string
			screen Screen
		}{
			{"MainMenu", ScreenMainMenu},
			{"ContainerList", ScreenContainerList},
			{"UpdateList", ScreenUpdateList},
		} {
			m := NewModel(fakeProjects(), "/tmp/cache.json", false)
			m.width = w
			m.height = 30
			m.screen = sc.screen
			out := m.View()
			fmt.Printf("\n########## %s @ width=%d ##########\n%s\n", sc.name, w, out)
			for _, line := range strings.Split(out, "\n") {
				if lw := lipgloss.Width(line); lw > w {
					t.Errorf("%s @ width=%d: line exceeds terminal width (%d > %d): %q", sc.name, w, lw, w, line)
				}
			}
		}
	}
}
