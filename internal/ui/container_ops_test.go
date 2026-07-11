package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/skpharma/docker-compose-manager/internal/docker"
)

// cProj builds a project with a realistic Status string (running:N iff running>0)
// so IsRunning() and the run-state helpers agree, as they do in production.
func cProj(name string, running, total int, upd bool) *docker.Project {
	state := "ok"
	if upd {
		state = "update"
	}
	imgs := map[string]docker.ImageInfo{
		name + ":1": {Name: name + ":1", CurrentVersion: "1", LatestVersion: "1", HasUpdate: upd, State: state},
	}
	status := "stopped"
	if running > 0 {
		status = fmt.Sprintf("running:%d", running)
	}
	return &docker.Project{Name: name, Status: status, RunningContainers: running, TotalServices: total, ImageInfo: imgs}
}

// The container-list STATUS column must reflect run-state, never update status.
func TestContainerStatusColumnIsRunState(t *testing.T) {
	cases := []struct {
		name           string
		running, total int
		upd            bool
		wantText       string
	}{
		{"running-with-update", 1, 1, true, "running"},
		{"running", 1, 1, false, "running"},
		{"stopped", 0, 2, false, "stopped"},
		{"partial", 1, 3, false, "partial"},
	}
	for _, c := range cases {
		p := cProj(c.name, c.running, c.total, c.upd)
		if got := runStateText(p); got != c.wantText {
			t.Errorf("%s: runStateText = %q, want %q", c.name, got, c.wantText)
		}
	}

	// And the rendered list must not contain the word "update" in place of status.
	m := NewModel([]*docker.Project{cProj("autoheal", 1, 1, true)}, "/tmp/c.json", false)
	m.width, m.height, m.screen = 100, 30, ScreenContainerList
	out := m.viewContainerList()
	if !strings.Contains(out, "running") {
		t.Errorf("expected 'running' in container list, got:\n%s", out)
	}
}

func TestContainerFilterCycle(t *testing.T) {
	projs := []*docker.Project{
		cProj("autoheal", 1, 1, false),
		cProj("traefik", 1, 1, false),
		cProj("keycloak", 0, 2, false),
	}
	m := NewModel(projs, "/tmp/c.json", false)
	m.screen = ScreenContainerList

	if got := len(m.containerIndices()); got != 3 {
		t.Fatalf("filter=all: want 3, got %d", got)
	}
	mi, _ := m.handleToggleFilter() // -> running
	m = mi.(Model)
	if m.containerFilter != 1 || len(m.containerIndices()) != 2 {
		t.Fatalf("filter=running: want 2 running, got filter=%d n=%d", m.containerFilter, len(m.containerIndices()))
	}
	mi, _ = m.handleToggleFilter() // -> stopped
	m = mi.(Model)
	if m.containerFilter != 2 || len(m.containerIndices()) != 1 {
		t.Fatalf("filter=stopped: want 1 stopped, got filter=%d n=%d", m.containerFilter, len(m.containerIndices()))
	}
	mi, _ = m.handleToggleFilter() // -> all
	m = mi.(Model)
	if m.containerFilter != 0 || len(m.containerIndices()) != 3 {
		t.Fatalf("filter wrap: want back to all (3), got filter=%d n=%d", m.containerFilter, len(m.containerIndices()))
	}
}

func TestOpTargetFallbackToCursor(t *testing.T) {
	projs := []*docker.Project{
		cProj("autoheal", 1, 1, false),
		cProj("traefik", 0, 1, false),
	}
	m := NewModel(projs, "/tmp/c.json", false)
	m.screen = ScreenContainerList
	m.containerFilter = 2 // stopped only -> only traefik (index 1) visible
	m.cursor = 0

	targets := m.containerOpTargets()
	if len(targets) != 1 || targets[0] != 1 {
		t.Fatalf("expected fallback target [1] (traefik), got %v", targets)
	}
}

func TestMassOpStateFlow(t *testing.T) {
	projs := []*docker.Project{
		cProj("autoheal", 1, 1, false),
		cProj("traefik", 1, 1, false),
		cProj("keycloak", 0, 2, false),
	}
	m := NewModel(projs, "/tmp/c.json", false)
	m.width, m.height, m.screen = 100, 30, ScreenContainerList

	m.selectedContainers[0] = true
	m.selectedContainers[1] = true
	mi, _ := m.handleContainerOp("stop")
	m = mi.(Model)
	if m.screen != ScreenContainerOpConfirm || len(m.opTargets) != 2 {
		t.Fatalf("expected confirm screen with 2 targets, got screen=%v n=%d", m.screen, len(m.opTargets))
	}

	mi, _ = m.handleEnter() // confirm -> op screen (Cmd returned, not executed here)
	m = mi.(Model)
	if m.screen != ScreenContainerOp || !m.loading || m.opTotal != 2 {
		t.Fatalf("expected op screen loading with total 2, got screen=%v loading=%v total=%d", m.screen, m.loading, m.opTotal)
	}

	mi, _ = m.Update(containerOpCompleteMsg{projectIndex: 0, success: true})
	m = mi.(Model)
	if !m.loading {
		t.Fatalf("expected still loading after 1/2 completions")
	}
	mi, _ = m.Update(containerOpCompleteMsg{projectIndex: 1, success: true})
	m = mi.(Model)
	if m.loading || m.opCompleted != 2 {
		t.Fatalf("expected done after 2/2, got loading=%v completed=%d", m.loading, m.opCompleted)
	}

	mi, _ = m.handleEnter() // dismiss -> back to list, selection cleared
	m = mi.(Model)
	if m.screen != ScreenContainerList || len(m.selectedContainers) != 0 {
		t.Fatalf("expected back to list with cleared selection, got screen=%v sel=%d", m.screen, len(m.selectedContainers))
	}
}

// Enter on the container list must still open details, mapped through the filter.
func TestContainerEnterOpensDetailsFiltered(t *testing.T) {
	projs := []*docker.Project{
		cProj("autoheal", 1, 1, false), // running
		cProj("keycloak", 0, 2, false), // stopped
	}
	m := NewModel(projs, "/tmp/c.json", false)
	m.screen = ScreenContainerList
	m.containerFilter = 2 // stopped only -> keycloak at display cursor 0
	m.cursor = 0

	mi, _ := m.handleEnter()
	m = mi.(Model)
	if m.screen != ScreenContainerDetail {
		t.Fatalf("expected detail screen, got %v", m.screen)
	}
	if m.selectedProject == nil || m.selectedProject.Name != "keycloak" {
		t.Fatalf("expected keycloak selected, got %v", m.selectedProject)
	}
}
