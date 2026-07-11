package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skpharma/docker-compose-manager/internal/docker"
)

// Screen represents different UI screens
type Screen int

const (
	// UI Constants
	maxVisibleItems = 10 // Maximum items shown in scrollable lists

	ScreenMainMenu Screen = iota
	ScreenContainerList
	ScreenContainerDetail // Show containers in a project (aptitude-style)
	ScreenActionMenu
	ScreenUpdateList
	ScreenUpdateModeSelect     // Choose: Pull only or Pull & Restart
	ScreenUpdateRestartConfirm // Select which projects to restart
	ScreenUpdateConfirm        // Old confirmation (kept for compatibility)
	ScreenUpdating
	ScreenLoading
	ScreenHelp // Help & Documentation screen
	ScreenConfirmExit
)

// Model represents the UI state
type Model struct {
	projects            []*docker.Project
	screen              Screen
	cursor              int
	selectedProject     *docker.Project
	selectedUpdates     map[int]bool // Projects selected for update
	selectedRestarts    map[int]bool // Projects selected for restart (subset of selectedUpdates)
	updateMode          string       // "pull" or "restart"
	message             string
	loading             bool
	err                 error
	quitting            bool
	updateProgress      string
	updatesTotal        int            // Total number of updates
	updatesCompleted    int            // Number of completed updates
	projectUpdateStatus map[int]string // Status for each project: "pending", "updating", "success", "failed"
	projectUpdateResult map[int]string // Result message for each project
	currentUpdateIndex  int            // Index of project currently being updated
	cacheAge            string         // How old is the cache
	checkingUpdates     bool           // Currently checking for updates
	currentCheckIndex   int            // Index of project currently being checked (-1 if none)
	cacheFile           string         // Path to cache file for saving
	viewportOffset      int            // Scroll offset for long lists
	width               int            // Terminal width
	height              int            // Terminal height
	debugMode           bool           // Enable debug logging
	viewRenderCount     int            // Count how many times View() is called
	filterUpdatesOnly   bool           // Update list: show only projects with updates
}

// updateIndices returns the real project indices shown in the update list, in
// display order, honouring the "updates only" filter.
func (m Model) updateIndices() []int {
	idxs := make([]int, 0, len(m.projects))
	for i, p := range m.projects {
		if m.filterUpdatesOnly && p.UpdateCount() == 0 {
			continue
		}
		idxs = append(idxs, i)
	}
	return idxs
}

// truncateMiddle truncates a string in the middle if it exceeds maxLen
// Keeps the last 3 characters visible with "..." in the middle
// Example: "super-lange-bookworm-version-mit-krass-vielen-zeichen" (maxLen=30)
//
//	-> "super-lange-bookworm-ver...hen"
func truncateMiddle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Keep last 3 chars
	if maxLen < 6 {
		// If maxLen is too small, just truncate normally
		return s[:maxLen]
	}

	suffix := s[len(s)-3:]
	// Calculate prefix length: maxLen - 3 (dots) - 3 (suffix)
	prefixLen := maxLen - 3 - 3
	prefix := s[:prefixLen]

	return prefix + "..." + suffix
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Responsive column layout (k9s-style) ────────────────────────────────────
//
// A row is a list of cells. Each cell has a minimum width, may "grow" to fill
// leftover space, and has a priority. When the terminal is too narrow to fit
// every cell, the cells with the highest priority number are dropped first
// (priority 0 is never dropped). This is how important columns survive on small
// screens instead of the whole table wrapping and garbling.

type cell struct {
	text  string                 // plain (unstyled) text
	min   int                    // minimum width in columns
	grow  bool                   // receives a share of any leftover width
	prio  int                    // higher = dropped first when space is tight (0 = never)
	paint func(...string) string // optional per-cell colouring (nil = plain)
}

const colGap = 2 // spaces between columns

// contentWidth is the usable width inside the bordered/padded box.
func (m Model) contentWidth() int {
	w := m.width
	if w < 20 {
		w = 80 // sane default before the first WindowSizeMsg arrives
	}
	// styleBox: rounded border (2) + horizontal padding (2*2) = 6
	avail := w - 6
	if avail < 24 {
		avail = 24
	}
	return avail
}

// visibleRows returns how many list rows fit given the terminal height,
// reserving space for the title, header, footer and summary lines.
func (m Model) visibleRows() int {
	h := m.height
	if h < 10 {
		h = 24 // sane default before the first WindowSizeMsg
	}
	rows := h - 11 // title + summary + header + separator + footer + borders
	if rows < 3 {
		rows = 3
	}
	if rows > 40 {
		rows = 40
	}
	return rows
}

// keptCells decides which cells fit into avail width (dropping by priority)
// and returns them together with their final widths, in original order.
func keptCells(avail int, cells []cell) ([]cell, []int) {
	keep := make([]bool, len(cells))
	for i := range cells {
		keep[i] = true
	}

	used := func() int {
		w, n := 0, 0
		for i, c := range cells {
			if keep[i] {
				w += c.min
				n++
			}
		}
		if n > 1 {
			w += (n - 1) * colGap
		}
		return w
	}

	// Drop highest-priority cells until it fits (never drop prio 0).
	for used() > avail {
		victim, maxPrio := -1, 0
		for i, c := range cells {
			if keep[i] && c.prio > maxPrio {
				maxPrio, victim = c.prio, i
			}
		}
		if victim == -1 {
			break
		}
		keep[victim] = false
	}

	// Distribute leftover space across growable, kept cells.
	widths := make([]int, len(cells))
	for i, c := range cells {
		widths[i] = c.min
	}
	leftover := avail - used()
	var growers []int
	for i, c := range cells {
		if keep[i] && c.grow {
			growers = append(growers, i)
		}
	}
	if leftover > 0 && len(growers) > 0 {
		per, rem := leftover/len(growers), leftover%len(growers)
		for _, i := range growers {
			widths[i] += per
			if rem > 0 {
				widths[i]++
				rem--
			}
		}
	}

	var out []cell
	var outW []int
	for i, c := range cells {
		if keep[i] {
			out = append(out, c)
			outW = append(outW, widths[i])
		}
	}
	return out, outW
}

// fitCell truncates or right-pads plain text to exactly w columns.
func fitCell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	vis := lipgloss.Width(s)
	if vis == w {
		return s
	}
	if vis < w {
		return s + strings.Repeat(" ", w-vis)
	}
	// Too long: truncate with an ellipsis.
	if w == 1 {
		return "…"
	}
	runes := []rune(s)
	// Trim runes until it fits (rune width ~1 for our content).
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > w {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// clip truncates plain text to at most w columns (with an ellipsis), without
// padding. Guarantees no single line exceeds the terminal width, which would
// otherwise make the lipgloss box grow and wrap in the real terminal.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > w {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// footer renders a help line, falling back to a shorter variant when the full
// text would not fit the terminal width, clipping as a final safety net.
func (m Model) footer(full, short string) string {
	avail := m.contentWidth()
	txt := full
	if lipgloss.Width(full) > avail {
		txt = short
	}
	return styleHelp.Render(clip(txt, avail))
}

// renderRow lays out cells into avail width. When highlight is true the whole
// row is rendered with the selection style and per-cell colours are suppressed
// (so the selected row reads as one block, k9s-style).
func renderRow(avail int, highlight bool, cells []cell) string {
	kept, widths := keptCells(avail, cells)
	parts := make([]string, 0, len(kept))
	for i, c := range kept {
		s := fitCell(c.text, widths[i])
		if !highlight && c.paint != nil {
			s = c.paint(s)
		}
		parts = append(parts, s)
	}
	line := strings.Join(parts, strings.Repeat(" ", colGap))
	if highlight {
		line = styleSelected.Render(line)
	}
	return line
}

// renderSeparator draws a dim rule matching the kept columns of a header row.
func renderSeparator(avail int, cells []cell) string {
	kept, widths := keptCells(avail, cells)
	parts := make([]string, 0, len(kept))
	for i := range kept {
		parts = append(parts, strings.Repeat("─", widths[i]))
	}
	return styleMuted.Render(strings.Join(parts, strings.Repeat(" ", colGap)))
}

// NewModel creates a new UI model
func NewModel(projects []*docker.Project, cacheFile string, debugMode bool) Model {
	return Model{
		projects:            projects,
		screen:              ScreenMainMenu,
		cursor:              0,
		selectedUpdates:     make(map[int]bool),
		selectedRestarts:    make(map[int]bool),
		projectUpdateStatus: make(map[int]string),
		projectUpdateResult: make(map[int]string),
		currentUpdateIndex:  -1,
		currentCheckIndex:   -1,
		cacheFile:           cacheFile,
		debugMode:           debugMode,
		viewRenderCount:     0,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "q", "esc":
			return m.handleBack()

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				// Adjust viewport if cursor moves above visible area
				if (m.screen == ScreenUpdateList || m.screen == ScreenContainerList) && m.cursor < m.viewportOffset {
					m.viewportOffset = m.cursor
				}
			}

		case "down", "j":
			return m.handleDown()

		case "enter":
			return m.handleEnter()

		case " ", "space":
			return m.handleSpace()

		case "a", "A":
			return m.handleSelectAll()

		case "o", "O":
			return m.handleSelectUpdates()

		case "f", "F":
			return m.handleToggleFilter()

		case "u", "U":
			return m.handleRefresh()

		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
			// Direct number selection for menus and lists
			num := int(msg.String()[0] - '0') // Convert char to int
			return m.handleNumberKey(num)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case operationMsg:
		m.loading = false
		m.message = string(msg)
		if m.selectedProject != nil {
			m.selectedProject.UpdateStatus()
		}
		return m, nil

	case errorMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case tickMsg:
		// Refresh view during updates to override docker output
		if m.loading {
			return m, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return tickMsg{}
			})
		}
		return m, nil

	case updateCompleteMsg:
		m.updatesCompleted++

		// Update status for this project
		if msg.success {
			m.projectUpdateStatus[msg.projectIndex] = "success"
			m.projectUpdateResult[msg.projectIndex] = "✓ OK"
		} else {
			m.projectUpdateStatus[msg.projectIndex] = "failed"
			m.projectUpdateResult[msg.projectIndex] = fmt.Sprintf("✗ %v", msg.err)
		}

		// Check if all updates are done
		if m.updatesCompleted >= m.updatesTotal {
			m.loading = false
		}
		return m, nil

	case allUpdatesCompleteMsg:
		// This message is no longer used, but kept for compatibility
		return m, nil

	case updatesCheckedMsg:
		m.checkingUpdates = false
		m.currentCheckIndex = -1
		m.cacheAge = m.calculateCacheAge()
		// Save updated cache to disk
		if err := docker.SaveToCache(m.projects, m.cacheFile); err != nil {
			m.message = fmt.Sprintf("Warning: failed to save cache: %v", err)
		}
		return m, nil

	case projectCheckProgressMsg:
		m.currentCheckIndex = msg.index
		return m, m.checkSingleProject(msg.index)

	case projectCheckCompleteMsg:
		// Move to next project
		nextIndex := msg.index + 1
		if nextIndex < len(m.projects) {
			// Check next project
			return m, func() tea.Msg {
				return projectCheckProgressMsg{index: nextIndex}
			}
		} else {
			// All done
			return m, func() tea.Msg {
				return updatesCheckedMsg{}
			}
		}
	}

	return m, nil
}

// handleBack handles back/escape navigation
func (m Model) handleBack() (tea.Model, tea.Cmd) {
	switch m.screen {
	case ScreenMainMenu:
		m.screen = ScreenConfirmExit
		m.cursor = 1 // Default to "No"
		return m, nil

	case ScreenContainerList:
		m.screen = ScreenMainMenu
		m.cursor = 0
		m.message = ""
		return m, nil

	case ScreenContainerDetail:
		m.screen = ScreenContainerList
		m.cursor = 0
		m.viewportOffset = 0
		m.message = ""
		return m, nil

	case ScreenActionMenu:
		m.screen = ScreenContainerDetail
		m.cursor = 0
		m.message = ""
		return m, nil

	case ScreenHelp:
		m.screen = ScreenMainMenu
		m.cursor = 0
		m.message = ""
		return m, nil

	case ScreenUpdateList:
		m.screen = ScreenMainMenu
		m.cursor = 0
		m.viewportOffset = 0
		m.selectedUpdates = make(map[int]bool)
		return m, nil

	case ScreenUpdateModeSelect:
		m.screen = ScreenUpdateList
		m.cursor = 0
		m.viewportOffset = 0
		return m, nil

	case ScreenUpdateRestartConfirm:
		m.screen = ScreenUpdateModeSelect
		m.cursor = 0
		return m, nil

	case ScreenUpdateConfirm:
		m.screen = ScreenUpdateList
		m.cursor = 0
		m.viewportOffset = 0
		return m, nil

	case ScreenLoading:
		m.screen = ScreenMainMenu
		m.cursor = 0
		return m, nil

	case ScreenConfirmExit:
		m.screen = ScreenMainMenu
		m.cursor = 0
		return m, nil
	}

	return m, nil
}

// handleDown handles cursor down
func (m Model) handleDown() (tea.Model, tea.Cmd) {
	switch m.screen {
	case ScreenMainMenu:
		if m.cursor < 2 {
			m.cursor++
		}

	case ScreenContainerList:
		if m.cursor < len(m.projects)-1 {
			m.cursor++
			// Adjust viewport if cursor moves below visible area
			if m.cursor >= m.viewportOffset+m.visibleRows() {
				m.viewportOffset = m.cursor - m.visibleRows() + 1
			}
		}

	case ScreenActionMenu:
		maxOptions := 1
		if m.selectedProject != nil && m.selectedProject.IsRunning() {
			maxOptions = 2
		}
		if m.cursor < maxOptions {
			m.cursor++
		}

	case ScreenUpdateList:
		if m.cursor < len(m.updateIndices())-1 {
			m.cursor++
			// Adjust viewport if cursor moves below visible area
			if m.cursor >= m.viewportOffset+m.visibleRows() {
				m.viewportOffset = m.cursor - m.visibleRows() + 1
			}
		}

	case ScreenUpdateModeSelect:
		if m.cursor < 1 {
			m.cursor++
		}

	case ScreenUpdateRestartConfirm:
		// Count how many projects are in selectedUpdates
		maxCursor := len(m.selectedUpdates) - 1
		if m.cursor < maxCursor {
			m.cursor++
		}

	case ScreenUpdateConfirm:
		if m.cursor < 1 {
			m.cursor++
		}

	case ScreenConfirmExit:
		if m.cursor < 1 {
			m.cursor++
		}
	}

	return m, nil
}

// handleEnter handles enter key
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.screen {
	case ScreenMainMenu:
		switch m.cursor {
		case 0: // Manage Containers
			m.screen = ScreenContainerList
			m.cursor = 0
			m.viewportOffset = 0
			m.message = ""
			return m, nil

		case 1: // Perform Updates
			m.screen = ScreenUpdateList
			m.cursor = 0
			m.viewportOffset = 0
			m.message = ""
			m.selectedUpdates = make(map[int]bool)
			// Calculate cache age from projects' LastUpdated
			m.cacheAge = m.calculateCacheAge()
			return m, nil

		case 2: // Help & Documentation
			m.screen = ScreenHelp
			m.cursor = 0
			return m, nil
		}

	case ScreenContainerList:
		if m.cursor < len(m.projects) {
			m.selectedProject = m.projects[m.cursor]
			m.screen = ScreenContainerDetail
			m.cursor = 0
			m.message = ""
			return m, nil
		}

	case ScreenContainerDetail:
		// From container detail, go to action menu
		m.screen = ScreenActionMenu
		m.cursor = 0
		return m, nil

	case ScreenActionMenu:
		return m.handleAction()

	case ScreenUpdateList:
		// Enter in update list goes to mode selection
		if len(m.selectedUpdates) > 0 {
			m.screen = ScreenUpdateModeSelect
			m.cursor = 0
			return m, nil
		}

	case ScreenUpdateModeSelect:
		// Choose between pull only or pull & restart
		if m.cursor == 0 {
			// Pull only - go directly to updating
			m.updateMode = "pull"
			m.screen = ScreenUpdating
			m.loading = true
			m.updateProgress = ""
			m.updatesTotal = len(m.selectedUpdates)
			m.updatesCompleted = 0
			// Initialize status for all selected projects
			m.projectUpdateStatus = make(map[int]string)
			m.projectUpdateResult = make(map[int]string)
			for idx := range m.selectedUpdates {
				m.projectUpdateStatus[idx] = "pending"
				m.projectUpdateResult[idx] = ""
			}
			m.currentUpdateIndex = -1
			return m, m.performUpdates()
		} else {
			// Pull & Restart - go to restart confirmation
			m.updateMode = "restart"
			m.screen = ScreenUpdateRestartConfirm
			m.cursor = 0
			// Pre-select all projects for restart
			m.selectedRestarts = make(map[int]bool)
			for idx := range m.selectedUpdates {
				m.selectedRestarts[idx] = true
			}
			return m, nil
		}

	case ScreenUpdateRestartConfirm:
		// Confirm restart selection
		m.screen = ScreenUpdating
		m.loading = true
		m.updateProgress = ""
		m.updatesTotal = len(m.selectedUpdates)
		m.updatesCompleted = 0
		// Initialize status for all selected projects
		m.projectUpdateStatus = make(map[int]string)
		m.projectUpdateResult = make(map[int]string)
		for idx := range m.selectedUpdates {
			m.projectUpdateStatus[idx] = "pending"
			m.projectUpdateResult[idx] = ""
		}
		m.currentUpdateIndex = -1
		// Start ticker to refresh view during updates
		return m, tea.Batch(
			m.performUpdates(),
			tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return tickMsg{}
			}),
		)

	case ScreenUpdateConfirm:
		if m.cursor == 0 { // Yes - perform updates
			m.screen = ScreenUpdating
			m.loading = true
			m.updateProgress = ""
			m.updatesTotal = len(m.selectedUpdates)
			m.updatesCompleted = 0
			// Initialize status for all selected projects
			m.projectUpdateStatus = make(map[int]string)
			m.projectUpdateResult = make(map[int]string)
			for idx := range m.selectedUpdates {
				m.projectUpdateStatus[idx] = "pending"
				m.projectUpdateResult[idx] = ""
			}
			m.currentUpdateIndex = -1
			// Start ticker to refresh view during updates
			return m, tea.Batch(
				m.performUpdates(),
				tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
					return tickMsg{}
				}),
			)
		} else { // No - back to list
			m.screen = ScreenUpdateList
			m.cursor = 0
			m.viewportOffset = 0
			return m, nil
		}

	case ScreenUpdating:
		// Any key press when updates are complete goes back to main menu
		if !m.loading && m.updatesCompleted >= m.updatesTotal {
			m.screen = ScreenMainMenu
			m.cursor = 0
			m.selectedUpdates = make(map[int]bool)
			m.updateProgress = ""
			m.updatesTotal = 0
			m.updatesCompleted = 0
			return m, nil
		}

	case ScreenConfirmExit:
		if m.cursor == 0 { // Yes
			m.quitting = true
			return m, tea.Quit
		} else { // No
			m.screen = ScreenMainMenu
			m.cursor = 0
			return m, nil
		}
	}

	return m, nil
}

// handleAction handles container actions
func (m Model) handleAction() (tea.Model, tea.Cmd) {
	if m.selectedProject == nil {
		return m, nil
	}

	m.loading = true
	m.err = nil

	if m.selectedProject.IsRunning() {
		// Running: Stop or Restart
		switch m.cursor {
		case 0: // Stop
			return m, performOperation(m.selectedProject, "stop")
		case 1: // Restart
			return m, performOperation(m.selectedProject, "restart")
		}
	} else {
		// Stopped: Start
		if m.cursor == 0 {
			return m, performOperation(m.selectedProject, "start")
		}
	}

	return m, nil
}

// handleSpace handles space key for toggling selections
func (m Model) handleSpace() (tea.Model, tea.Cmd) {
	if m.screen == ScreenUpdateList {
		// Map cursor (display position) to the real project index.
		idxs := m.updateIndices()
		if m.cursor >= 0 && m.cursor < len(idxs) {
			real := idxs[m.cursor]
			if m.selectedUpdates[real] {
				delete(m.selectedUpdates, real)
			} else {
				m.selectedUpdates[real] = true
			}
		}
	} else if m.screen == ScreenUpdateRestartConfirm {
		// Toggle restart selection for current project
		// Map cursor (display index) to actual project index
		displayIndex := 0
		for i := range m.projects {
			if !m.selectedUpdates[i] {
				continue
			}
			if displayIndex == m.cursor {
				// Toggle this project
				if m.selectedRestarts[i] {
					delete(m.selectedRestarts, i)
				} else {
					m.selectedRestarts[i] = true
				}
				break
			}
			displayIndex++
		}
	}
	return m, nil
}

// handleSelectAll handles 'a' key for selecting/deselecting all visible projects
func (m Model) handleSelectAll() (tea.Model, tea.Cmd) {
	if m.screen == ScreenUpdateList {
		idxs := m.updateIndices()
		// If every visible project is already selected, clear; otherwise select all visible.
		allSelected := len(idxs) > 0
		for _, i := range idxs {
			if !m.selectedUpdates[i] {
				allSelected = false
				break
			}
		}
		if allSelected {
			for _, i := range idxs {
				delete(m.selectedUpdates, i)
			}
		} else {
			for _, i := range idxs {
				m.selectedUpdates[i] = true
			}
		}
	}
	return m, nil
}

// handleSelectUpdates handles 'o' key: select only projects that have updates.
func (m Model) handleSelectUpdates() (tea.Model, tea.Cmd) {
	if m.screen == ScreenUpdateList {
		m.selectedUpdates = make(map[int]bool)
		for i, p := range m.projects {
			if p.UpdateCount() > 0 {
				m.selectedUpdates[i] = true
			}
		}
	}
	return m, nil
}

// handleToggleFilter handles 'f' key: toggle "show only projects with updates".
func (m Model) handleToggleFilter() (tea.Model, tea.Cmd) {
	if m.screen == ScreenUpdateList {
		m.filterUpdatesOnly = !m.filterUpdatesOnly
		m.cursor = 0
		m.viewportOffset = 0
	}
	return m, nil
}

// handleRefresh handles 'r' key for manually refreshing update checks
func (m Model) handleRefresh() (tea.Model, tea.Cmd) {
	if m.screen == ScreenUpdateList && !m.checkingUpdates {
		m.checkingUpdates = true
		return m, m.checkForUpdates()
	}
	return m, nil
}

// handleNumberKey handles direct number selection (1-9, 0 for 10)
func (m Model) handleNumberKey(num int) (tea.Model, tea.Cmd) {
	// 0 represents 10
	if num == 0 {
		num = 10
	}

	switch m.screen {
	case ScreenMainMenu:
		// Main menu has 3 options
		if num >= 1 && num <= 3 {
			m.cursor = num - 1
			return m.handleEnter()
		}

	case ScreenContainerList:
		// Container list - select Nth visible project
		viewStart := m.viewportOffset
		viewEnd := m.viewportOffset + m.visibleRows()
		if viewEnd > len(m.projects) {
			viewEnd = len(m.projects)
		}
		visibleCount := viewEnd - viewStart

		if num >= 1 && num <= visibleCount {
			m.cursor = viewStart + num - 1
			return m.handleEnter()
		}

	case ScreenUpdateList:
		// Update list - toggle Nth visible project
		viewStart := m.viewportOffset
		viewEnd := m.viewportOffset + m.visibleRows()
		if viewEnd > len(m.updateIndices()) {
			viewEnd = len(m.updateIndices())
		}
		visibleCount := viewEnd - viewStart

		if num >= 1 && num <= visibleCount {
			m.cursor = viewStart + num - 1
			return m.handleSpace() // Toggle selection instead of Enter
		}

	case ScreenActionMenu:
		// Action menu - 1 or 2 options depending on state
		maxOptions := 1
		if m.selectedProject != nil && m.selectedProject.IsRunning() {
			maxOptions = 2
		}
		if num >= 1 && num <= maxOptions {
			m.cursor = num - 1
			return m.handleEnter()
		}

	case ScreenUpdateModeSelect:
		// Update mode: 2 options
		if num >= 1 && num <= 2 {
			m.cursor = num - 1
			return m.handleEnter()
		}

	case ScreenUpdateConfirm, ScreenConfirmExit:
		// Yes/No screens: 2 options
		if num >= 1 && num <= 2 {
			m.cursor = num - 1
			return m.handleEnter()
		}
	}

	return m, nil
}

// calculateCacheAge calculates how old the cached data is
func (m Model) calculateCacheAge() string {
	if len(m.projects) == 0 {
		return "unknown"
	}

	// Find the oldest LastUpdated time
	oldestTime := m.projects[0].LastUpdated
	for _, p := range m.projects {
		if p.LastUpdated.Before(oldestTime) {
			oldestTime = p.LastUpdated
		}
	}

	// Calculate age
	age := oldestTime
	if age.IsZero() {
		return "unknown"
	}

	// Format as human-readable string
	return age.Format("2006-01-02 15:04")
}

// View renders the UI
func (m Model) View() string {
	m.viewRenderCount++

	if m.quitting {
		return styleInfo.Render("Goodbye!\n")
	}

	var view string
	switch m.screen {
	case ScreenMainMenu:
		view = m.viewMainMenu()
	case ScreenContainerList:
		view = m.viewContainerList()
	case ScreenContainerDetail:
		view = m.viewContainerDetail()
	case ScreenActionMenu:
		view = m.viewActionMenu()
	case ScreenUpdateList:
		view = m.viewUpdateList()
	case ScreenUpdateModeSelect:
		view = m.viewUpdateModeSelect()
	case ScreenUpdateRestartConfirm:
		view = m.viewUpdateRestartConfirm()
	case ScreenUpdateConfirm:
		view = m.viewUpdateConfirm()
	case ScreenUpdating:
		view = m.viewUpdating()
	case ScreenLoading:
		view = m.viewLoading()
	case ScreenHelp:
		view = m.viewHelp()
	case ScreenConfirmExit:
		view = m.viewConfirmExit()
	default:
		view = "Unknown screen"
	}

	// Debug logging
	if m.debugMode {
		m.logViewToFile(view)
	}

	return view
}

// logViewToFile writes the view content to a debug log file
func (m Model) logViewToFile(view string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	logFile := filepath.Join(homeDir, "docker-compose-manager-debug.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	separator := strings.Repeat("=", 80)

	fmt.Fprintf(f, "\n%s\n", separator)
	fmt.Fprintf(f, "RENDER #%d - %s - Screen: %v - Viewport: %d - Cursor: %d\n",
		m.viewRenderCount, timestamp, m.screen, m.viewportOffset, m.cursor)
	fmt.Fprintf(f, "Width: %d, Height: %d\n", m.width, m.height)
	fmt.Fprintf(f, "%s\n", separator)
	fmt.Fprintf(f, "%s\n", view)
	fmt.Fprintf(f, "%s\n\n", separator)
}

// viewLoading renders a loading screen
func (m Model) viewLoading() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Checking for Updates"))
	b.WriteString("\n\n")
	b.WriteString(styleInfo.Render("⏳ Checking for available updates...\n\n"))
	b.WriteString("This may take a moment as we check the registry for each image.\n")
	return styleBox.Render(b.String())
}

// viewMainMenu renders the main menu
func (m Model) viewMainMenu() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Docker Compose Manager v2.1"))
	b.WriteString("\n\n")

	// Update summary badge — visible immediately at startup (from cache).
	b.WriteString(m.summaryBar())
	b.WriteString("\n\n")

	pu, _ := m.updateSummary()
	updatesLabel := "Perform Updates"
	if pu > 0 {
		updatesLabel = fmt.Sprintf("Perform Updates (%d available)", pu)
	}
	options := []string{
		"Manage Containers (Start/Stop/Restart)",
		updatesLabel,
		"Help & Documentation",
	}

	for i, option := range options {
		cursor := " "
		number := fmt.Sprintf("[%d]", i+1)

		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			option = styleHighlight.Render(option)
			number = styleHighlight.Render(number)
		} else if i == 1 && pu > 0 {
			option = styleUpdate.Render(option)
		}
		b.WriteString(fmt.Sprintf("%s %s %s\n", cursor, number, option))
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Use ↑/↓ or 1-3 to navigate, Enter to select, q to exit"))

	if m.message != "" {
		b.WriteString("\n\n")
		b.WriteString(styleInfo.Render(m.message))
	}

	return styleBox.Render(b.String())
}

// viewContainerList renders the container list (one row per project, k9s-style)
func (m Model) viewContainerList() string {
	var b strings.Builder
	avail := m.contentWidth()

	b.WriteString(styleTitle.Render("Manage Containers"))
	b.WriteString("\n")
	b.WriteString(m.summaryBar())
	b.WriteString("\n\n")

	// Header row (same column spec as data rows so they align).
	header := func(nr, name, run, status string) []cell {
		return []cell{
			{text: nr, min: 4, prio: 0},
			{text: "", min: 1, prio: 0}, // glyph column
			{text: name, min: 12, grow: true, prio: 0},
			{text: run, min: 5, prio: 2},
			{text: status, min: 9, prio: 1},
		}
	}
	b.WriteString(renderRow(avail, false, header("#", " ", "RUN", "STATUS")))
	b.WriteString("\n")
	b.WriteString(renderSeparator(avail, header("#", " ", "RUN", "STATUS")))
	b.WriteString("\n")

	// Visible range.
	rows := m.visibleRows()
	viewStart := m.viewportOffset
	viewEnd := viewStart + rows
	if viewEnd > len(m.projects) {
		viewEnd = len(m.projects)
	}

	if viewStart > 0 {
		b.WriteString(styleMuted.Render("  ▲ more above"))
	}
	b.WriteString("\n")

	bodyLines := 0
	for i := viewStart; i < viewEnd; i++ {
		p := m.projects[i]
		nr := fmt.Sprintf("%d", i+1)
		cells := []cell{
			{text: nr, min: 4, prio: 0, paint: styleMuted.Render},
			{text: glyphChar(p), min: 1, prio: 0, paint: glyphPaint(p)},
			{text: p.Name, min: 12, grow: true, prio: 0},
			{text: runDisplay(p), min: 5, prio: 2, paint: statusPaint(p)},
			{text: projectStatusText(p), min: 9, prio: 1, paint: statusPaint(p)},
		}
		b.WriteString(renderRow(avail, m.cursor == i, cells))
		b.WriteString("\n")
		bodyLines++
	}

	// Keep the body height constant so an inline-mode redraw never leaves
	// ghost rows behind (see viewUpdateList for the same reasoning).
	for ; bodyLines < rows; bodyLines++ {
		b.WriteString("\n")
	}

	if viewEnd < len(m.projects) {
		b.WriteString(styleMuted.Render("  ▼ more below"))
	}
	b.WriteString("\n\n")
	b.WriteString(styleHelp.Render("↑/↓ or 1-9/0 navigate · Enter details · Esc/q back"))

	if m.message != "" {
		b.WriteString("\n\n")
		b.WriteString(styleInfo.Render(m.message))
	}

	return styleBox.Render(b.String())
}

// glyphPaint returns the colour function matching a project's glyph.
func glyphPaint(p *docker.Project) func(...string) string {
	switch {
	case !p.Checked():
		return styleMuted.Render
	case p.UpdateCount() > 0:
		return styleUpdate.Render
	case p.FullyUnknown():
		return styleMuted.Render
	case p.IsRunning():
		return styleSuccess.Render
	default:
		return styleMuted.Render
	}
}

// statusPaint returns the colour function for a project's status/run cells.
func statusPaint(p *docker.Project) func(...string) string {
	switch {
	case p.UpdateCount() > 0:
		return styleUpdate.Render
	case p.FullyUnknown():
		return styleMuted.Render
	case p.IsRunning():
		return styleSuccess.Render
	default:
		return styleMuted.Render
	}
}

// viewActionMenu renders the action menu
func (m Model) viewActionMenu() string {
	if m.selectedProject == nil {
		return "No project selected"
	}

	var b strings.Builder

	b.WriteString(styleTitle.Render(fmt.Sprintf("Action for %s", m.selectedProject.Name)))
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(styleInfo.Render("⏳ Processing...\n"))
	} else if m.err != nil {
		b.WriteString(styleError.Render(fmt.Sprintf("❌ Error: %v\n", m.err)))
	} else if m.message != "" {
		b.WriteString(styleSuccess.Render(fmt.Sprintf("✓ %s\n", m.message)))
	}

	b.WriteString("\n")

	var options []string
	if m.selectedProject.IsRunning() {
		options = []string{"Stop containers", "Restart containers"}
	} else {
		options = []string{"Start containers"}
	}

	for i, option := range options {
		cursor := " "
		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			option = styleHighlight.Render(option)
		}
		b.WriteString(fmt.Sprintf("%s %s\n", cursor, option))
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Use ↑/↓ to navigate, Enter to select, Esc/q to go back"))

	return styleBox.Render(b.String())
}

// viewConfirmExit renders the exit confirmation
func (m Model) viewConfirmExit() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Confirm Exit"))
	b.WriteString("\n\n")
	b.WriteString("Do you really want to exit?\n\n")

	options := []string{"Yes", "No"}
	for i, option := range options {
		cursor := " "
		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			option = styleHighlight.Render(option)
		}
		b.WriteString(fmt.Sprintf("%s %s\n", cursor, option))
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Use ↑/↓ to navigate, Enter to select"))

	return styleBox.Render(b.String())
}

// viewUpdateList renders the update selection screen (one row per project).
func (m Model) viewUpdateList() string {
	var b strings.Builder
	avail := m.contentWidth()

	b.WriteString(styleTitle.Render("Select Projects to Update"))
	if m.filterUpdatesOnly {
		b.WriteString("  " + styleUpdate.Render("[filter: updates only]"))
	}
	b.WriteString("\n")
	if m.checkingUpdates {
		b.WriteString(styleInfo.Render("⏳ Checking registries for updates…"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(m.summaryBar())
		b.WriteString("\n")
		if n := m.unknownImageCount(); n > 0 {
			b.WriteString(styleMuted.Render(clip(fmt.Sprintf("  ⓘ %d image(s) unreachable (registry rate limit? try 'docker login')", n), m.contentWidth())))
		}
		b.WriteString("\n")
	}

	// Column spec (shared by header, separator and rows).
	spec := func(sel, glyph, name, upd, note string) []cell {
		return []cell{
			{text: sel, min: 3, prio: 0},
			{text: glyph, min: 1, prio: 0},
			{text: name, min: 12, grow: true, prio: 0},
			{text: upd, min: 5, prio: 1},
			{text: note, min: 10, grow: true, prio: 2},
		}
	}
	b.WriteString(renderRow(avail, false, spec("", " ", "PROJECT", "UPD", "IMAGES WITH UPDATES")))
	b.WriteString("\n")
	b.WriteString(renderSeparator(avail, spec("", " ", "PROJECT", "UPD", "IMAGES WITH UPDATES")))
	b.WriteString("\n")

	idxs := m.updateIndices()
	rows := m.visibleRows()
	viewStart := m.viewportOffset
	viewEnd := viewStart + rows
	if viewEnd > len(idxs) {
		viewEnd = len(idxs)
	}

	if viewStart > 0 {
		b.WriteString(styleMuted.Render("  ▲ more above"))
	}
	b.WriteString("\n")

	// Keep the body height constant (= rows) regardless of how many projects
	// are visible. In inline mode (no alt-screen) a shrinking frame leaves
	// ghost rows behind — most visibly the first entry appearing doubled when
	// toggling the "updates only" filter. Padding to a fixed height avoids it.
	bodyLines := 0
	if len(idxs) == 0 {
		b.WriteString(styleMuted.Render("  (no projects match the filter — press 'f' to show all)"))
		b.WriteString("\n")
		bodyLines++
	}

	for pos := viewStart; pos < viewEnd; pos++ {
		real := idxs[pos]
		p := m.projects[real]

		checkbox := "[ ]"
		if m.selectedUpdates[real] {
			checkbox = "[✓]"
		}

		glyph := glyphChar(p)
		if m.checkingUpdates && m.currentCheckIndex == real {
			glyph = "⏳"
		}

		upd, note, notePaint := updateCells(p)

		cells := []cell{
			{text: checkbox, min: 3, prio: 0, paint: checkboxPaint(m.selectedUpdates[real])},
			{text: glyph, min: 1, prio: 0, paint: glyphPaint(p)},
			{text: p.Name, min: 12, grow: true, prio: 0},
			{text: upd, min: 5, prio: 1, paint: statusPaint(p)},
			{text: note, min: 10, grow: true, prio: 2, paint: notePaint},
		}
		b.WriteString(renderRow(avail, m.cursor == pos, cells))
		b.WriteString("\n")
		bodyLines++
	}

	for ; bodyLines < rows; bodyLines++ {
		b.WriteString("\n")
	}

	if viewEnd < len(idxs) {
		b.WriteString(styleMuted.Render("  ▼ more below"))
	}
	b.WriteString("\n\n")

	selectedCount := len(m.selectedUpdates)
	if selectedCount > 0 {
		b.WriteString(styleInfo.Render(fmt.Sprintf("Selected: %d project(s)", selectedCount)))
	}
	b.WriteString("\n")
	b.WriteString(m.footer(
		"Space/1-9 select · a all · o only-updates · f filter · u refresh · Enter continue · Esc/q back",
		"Space select · a/o/f · u refresh · Enter ok · q back"))

	return styleBox.Render(b.String())
}

// updateCells returns the "UPD" count cell, a note describing updating images,
// and the note's colour function.
func updateCells(p *docker.Project) (upd, note string, notePaint func(...string) string) {
	if !p.Checked() {
		return "—", "not checked (press 'u')", styleMuted.Render
	}
	n := p.UpdateCount()
	total := p.ImageCount()
	unknown := p.UnknownCount()
	upd = fmt.Sprintf("%d/%d", n, total)
	if n == 0 {
		switch {
		case p.FullyUnknown():
			return "?", fmt.Sprintf("%d unreachable", unknown), styleMuted.Render
		case unknown > 0:
			return upd, fmt.Sprintf("up to date (%d unreachable)", unknown), styleMuted.Render
		default:
			return upd, "up to date", styleMuted.Render
		}
	}
	return upd, updatingImageNames(p), styleUpdate.Render
}

// updatingImageNames lists the base names of images that have an update.
func updatingImageNames(p *docker.Project) string {
	keys := make([]string, 0, len(p.ImageInfo))
	for k := range p.ImageInfo {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var names []string
	for _, k := range keys {
		if p.ImageInfo[k].HasUpdate {
			names = append(names, baseImageName(p.ImageInfo[k].Name))
		}
	}
	return strings.Join(names, ", ")
}

// baseImageName strips registry path and tag from an image reference.
func baseImageName(image string) string {
	name := image
	if i := strings.LastIndex(name, ":"); i >= 0 && !strings.Contains(name[i:], "/") {
		name = name[:i]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func checkboxPaint(selected bool) func(...string) string {
	if selected {
		return styleSuccess.Render
	}
	return styleMuted.Render
}

// viewUpdateModeSelect renders the update mode selection screen
func (m Model) viewUpdateModeSelect() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Select Update Mode"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("%d project(s) selected for update\n\n", len(m.selectedUpdates)))

	options := []string{
		"Pull Images Only (no restart)",
		"Pull & Restart Containers",
	}

	for i, option := range options {
		cursor := " "
		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			option = styleHighlight.Render(option)
		}
		b.WriteString(fmt.Sprintf("%s %s\n", cursor, option))
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Use ↑/↓ to navigate, Enter to select, Esc/q to go back"))

	return styleBox.Render(b.String())
}

// viewUpdateRestartConfirm renders the restart confirmation screen
func (m Model) viewUpdateRestartConfirm() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Select Projects to Restart"))
	b.WriteString("\n\n")
	b.WriteString(styleInfo.Render("All projects will be updated (pull images)."))
	b.WriteString("\n")
	b.WriteString(styleInfo.Render("Select which projects should also RESTART their containers:"))
	b.WriteString("\n\n")

	// Show only projects from selectedUpdates
	displayIndex := 0
	for i, project := range m.projects {
		if !m.selectedUpdates[i] {
			continue // Skip projects not selected for update
		}

		cursor := " "
		checkbox := "[ ]"
		name := project.Name

		if m.selectedRestarts[i] {
			checkbox = "[✓]"
		}

		paddedName := fmt.Sprintf("%-20s", name)

		if m.cursor == displayIndex {
			cursor = styleHighlight.Render(">")
			paddedName = styleHighlight.Render(paddedName)
		}

		b.WriteString(fmt.Sprintf("%s %s %s\n", cursor, checkbox, paddedName))
		displayIndex++
	}

	b.WriteString("\n")
	selectedCount := len(m.selectedRestarts)
	b.WriteString(styleInfo.Render(fmt.Sprintf("Will restart: %d project(s)", selectedCount)))
	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Space to toggle, Enter to continue, Esc/q to go back"))

	return styleBox.Render(b.String())
}

// viewUpdateConfirm renders the update confirmation screen (old, kept for compatibility)
func (m Model) viewUpdateConfirm() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Confirm Updates"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Update %d project(s)?\n\n", len(m.selectedUpdates)))

	// List selected projects
	for i := range m.selectedUpdates {
		if i < len(m.projects) {
			b.WriteString(fmt.Sprintf("  • %s\n", m.projects[i].Name))
		}
	}

	b.WriteString("\n")

	options := []string{"Yes", "No"}
	for i, option := range options {
		cursor := " "
		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			option = styleHighlight.Render(option)
		}
		b.WriteString(fmt.Sprintf("%s %s\n", cursor, option))
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Use ↑/↓ to navigate, Enter to select"))

	return styleBox.Render(b.String())
}

// cleanProgressText removes ANSI codes, carriage returns, and extra whitespace
func cleanProgressText(text string) string {
	// Remove carriage returns (used by docker for progress bars)
	text = strings.ReplaceAll(text, "\r", "")

	// Remove ANSI escape codes (colors, cursor movement, etc.)
	// Pattern: ESC [ ... m  or ESC [ ... (letter)
	for {
		start := strings.Index(text, "\x1b[")
		if start == -1 {
			break
		}

		// Find the end of the ANSI code
		end := start + 2
		for end < len(text) {
			ch := text[end]
			// ANSI codes end with a letter
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
				end++
				break
			}
			end++
		}

		// Remove the ANSI code
		text = text[:start] + text[end:]
	}

	// Split into lines and clean each one
	lines := strings.Split(text, "\n")
	cleanedLines := make([]string, 0, len(lines))

	for _, line := range lines {
		// Trim leading/trailing whitespace
		line = strings.TrimSpace(line)

		// Skip empty lines
		if line == "" {
			continue
		}

		// Ensure consistent indentation (if line starts with checkmark/cross, add 2 spaces)
		if strings.HasPrefix(line, "✓") || strings.HasPrefix(line, "✗") {
			line = "  " + line
		}

		cleanedLines = append(cleanedLines, line)
	}

	return strings.Join(cleanedLines, "\n")
}

// viewUpdating renders the updating progress screen
func (m Model) viewUpdating() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Updating Projects"))
	b.WriteString("\n\n")

	avail := m.contentWidth()

	// Overall progress bar (width scales with the terminal).
	progressPercent := 0
	if m.updatesTotal > 0 {
		progressPercent = (m.updatesCompleted * 100) / m.updatesTotal
	}
	barWidth := avail - 24
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 50 {
		barWidth = 50
	}
	progressBar := renderProgressBar(progressPercent, barWidth)
	b.WriteString(clip(fmt.Sprintf("Overall Progress: %s %3d%% (%d/%d)",
		progressBar, progressPercent, m.updatesCompleted, m.updatesTotal), avail))
	b.WriteString("\n\n")

	spec := func(nr, name, result, status string) []cell {
		return []cell{
			{text: nr, min: 4, prio: 0},
			{text: name, min: 12, grow: true, prio: 0},
			{text: result, min: 8, grow: true, prio: 1},
			{text: status, min: 3, prio: 0},
		}
	}
	b.WriteString(renderRow(avail, false, spec("#", "NAME", "RESULT", "ST")))
	b.WriteString("\n")
	b.WriteString(renderSeparator(avail, spec("#", "NAME", "RESULT", "ST")))
	b.WriteString("\n")

	rowNum := 1
	for i, project := range m.projects {
		if !m.selectedUpdates[i] {
			continue
		}

		status := m.projectUpdateStatus[i]
		result := m.projectUpdateResult[i]

		var statusIcon string
		var paint func(...string) string
		switch status {
		case "updating":
			statusIcon, paint = "⏳", styleHighlight.Render
		case "success":
			statusIcon, paint = "✓", styleSuccess.Render
		case "failed":
			statusIcon, paint = "✗", styleError.Render
		default:
			statusIcon, paint = "·", styleMuted.Render
		}

		cells := []cell{
			{text: fmt.Sprintf("%d", rowNum), min: 4, prio: 0, paint: styleMuted.Render},
			{text: project.Name, min: 12, grow: true, prio: 0},
			{text: result, min: 8, grow: true, prio: 1, paint: paint},
			{text: statusIcon, min: 3, prio: 0, paint: paint},
		}
		b.WriteString(renderRow(avail, false, cells))
		b.WriteString("\n")

		rowNum++
	}

	// Status message at the bottom
	b.WriteString("\n")
	if m.loading {
		// Count how many are currently updating
		updatingCount := 0
		for idx := range m.selectedUpdates {
			if m.projectUpdateStatus[idx] == "updating" {
				updatingCount++
			}
		}
		b.WriteString(styleInfo.Render(fmt.Sprintf("⏳ Updating %d project(s) in parallel...", updatingCount)))
	} else {
		b.WriteString(styleSuccess.Render("✓ All updates completed!"))
		b.WriteString("\n\n")
		b.WriteString(styleHelp.Render("Press any key to continue..."))
	}

	return styleBox.Render(b.String())
}

// renderProgressBar creates a text-based progress bar
func renderProgressBar(percent, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := (percent * width) / 100
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return bar
}

// viewHelp renders the help and documentation screen
func (m Model) viewHelp() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Help & Documentation"))
	b.WriteString("\n\n")

	b.WriteString(styleHighlight.Render("🎯 Overview"))
	b.WriteString("\n")
	b.WriteString("Docker Compose Manager is a terminal UI for managing Docker Compose projects.\n")
	b.WriteString("It provides an easy way to start, stop, restart containers and update images.\n\n")

	b.WriteString(styleHighlight.Render("⌨️  Keyboard Shortcuts"))
	b.WriteString("\n")
	b.WriteString("  ↑/↓ or k/j      Navigate menu items\n")
	b.WriteString("  1, 2, 3         Direct menu selection (main menu only)\n")
	b.WriteString("  Enter           Select item / Confirm action\n")
	b.WriteString("  Space           Toggle selection (in update/restart lists)\n")
	b.WriteString("  a               Select all / Deselect all (in lists)\n")
	b.WriteString("  r               Refresh update check (in update screen)\n")
	b.WriteString("  Esc or q        Go back to previous screen\n")
	b.WriteString("  Ctrl+C          Force quit application\n\n")

	b.WriteString(styleHighlight.Render("📋 Features"))
	b.WriteString("\n")
	b.WriteString("  • Container Management: Start, stop, restart individual projects\n")
	b.WriteString("  • Update Management: Pull latest images with optional restart\n")
	b.WriteString("  • Dual Update Modes:\n")
	b.WriteString("    - Pull Images Only: Download new images without restarting\n")
	b.WriteString("    - Pull & Restart: Download and recreate containers\n")
	b.WriteString("  • Cache System: Fast startup with background update checks\n")
	b.WriteString("  • Version Display: Shows current and available versions\n")
	b.WriteString("  • Multi-Select: Update multiple projects at once\n\n")

	b.WriteString(styleHighlight.Render("🔄 Update Workflow"))
	b.WriteString("\n")
	b.WriteString("  1. Select \"Perform Updates\" from main menu\n")
	b.WriteString("  2. Select projects to update (Space to toggle, 'a' for all)\n")
	b.WriteString("  3. Choose update mode (pull only or pull & restart)\n")
	b.WriteString("  4. If restart mode: confirm which containers to restart\n")
	b.WriteString("  5. View progress and results\n\n")

	b.WriteString(styleHighlight.Render("💡 Tips"))
	b.WriteString("\n")
	b.WriteString("  • Use 'r' in update screen to manually refresh available updates\n")
	b.WriteString("  • Cache age shown in update screen - use --update-cache flag for cron\n")
	b.WriteString("  • Press Enter on a project to view its containers and versions\n")
	b.WriteString("  • Red ✗ indicates update failures, green ✓ indicates success\n\n")

	b.WriteString(styleHelp.Render("Press Esc or q to return to main menu"))

	return styleBox.Render(b.String())
}

// viewContainerDetail renders detailed view of containers in a project
func (m Model) viewContainerDetail() string {
	if m.selectedProject == nil {
		return styleError.Render("No project selected")
	}

	// If ImageInfo is empty, try to get running container info
	if len(m.selectedProject.ImageInfo) == 0 {
		m.selectedProject.GetRunningContainerInfo()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render(fmt.Sprintf("Project: %s", m.selectedProject.Name)))
	b.WriteString("\n\n")

	b.WriteString(styleInfo.Render(fmt.Sprintf("Path: %s", m.selectedProject.Path)))
	b.WriteString("\n")
	b.WriteString(styleInfo.Render(fmt.Sprintf("Compose File: %s", m.selectedProject.ComposeFile)))
	b.WriteString("\n")
	b.WriteString(styleInfo.Render(fmt.Sprintf("Status: %s", m.selectedProject.Status)))
	b.WriteString("\n\n")

	// Show containers/images
	b.WriteString(styleHighlight.Render("📦 Containers & Images"))
	b.WriteString("\n\n")

	if len(m.selectedProject.ImageInfo) == 0 {
		b.WriteString(styleMuted.Render("No containers running or unable to fetch container information."))
		b.WriteString("\n")
	} else {
		avail := m.contentWidth()
		spec := func(st, name, tag, ver, upd string) []cell {
			return []cell{
				{text: st, min: 1, prio: 0},
				{text: name, min: 12, grow: true, prio: 0},
				{text: tag, min: 8, prio: 2},
				{text: ver, min: 8, grow: true, prio: 1},
				{text: upd, min: 8, prio: 1},
			}
		}
		b.WriteString(renderRow(avail, false, spec(" ", "IMAGE", "TAG", "VERSION", "UPDATE")))
		b.WriteString("\n")
		b.WriteString(renderSeparator(avail, spec(" ", "IMAGE", "TAG", "VERSION", "UPDATE")))
		b.WriteString("\n")

		imageNames := make([]string, 0, len(m.selectedProject.ImageInfo))
		for imgName := range m.selectedProject.ImageInfo {
			imageNames = append(imageNames, imgName)
		}
		sort.Strings(imageNames)

		for _, imgKey := range imageNames {
			img := m.selectedProject.ImageInfo[imgKey]
			imgName := img.Name
			imgTag := "latest"
			if strings.Contains(imgName, "/") {
				parts := strings.Split(imgName, "/")
				imgName = parts[len(parts)-1]
			}
			if strings.Contains(imgName, ":") {
				parts := strings.Split(imgName, ":")
				imgName = parts[0]
				imgTag = parts[1]
			}

			glyph, updText, paint := imageStatusCells(img)
			cells := []cell{
				{text: glyph, min: 1, prio: 0, paint: paint},
				{text: imgName, min: 12, grow: true, prio: 0, paint: styleInfo.Render},
				{text: imgTag, min: 8, prio: 2, paint: styleMuted.Render},
				{text: img.CurrentVersion, min: 8, grow: true, prio: 1},
				{text: updText, min: 8, prio: 1, paint: paint},
			}
			b.WriteString(renderRow(avail, false, cells))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Press Esc/q to go back, Enter to select action"))

	return styleBox.Render(b.String())
}

// imageStatusCells returns a glyph, an "UPDATE" column label, and the colour for
// an image based on its State.
func imageStatusCells(img docker.ImageInfo) (glyph, updText string, paint func(...string) string) {
	switch img.State {
	case "update":
		return "⬆", "available", styleUpdate.Render
	case "not-pulled":
		return "⬇", "not pulled", styleUpdate.Render
	case "unknown":
		return "?", "unknown", styleMuted.Render
	default:
		if img.HasUpdate {
			return "⬆", "available", styleUpdate.Render
		}
		return "✓", "ok", styleSuccess.Render
	}
}

// Styles
var (
	styleBox = lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63"))

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

	styleHighlight = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)

	styleSuccess = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	styleError = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	styleInfo = lipgloss.NewStyle().
			Foreground(lipgloss.Color("111"))

	styleMuted = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	styleHelp = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	// styleSelected highlights the row under the cursor (k9s-style selection).
	styleSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)

	// styleUpdate marks images/projects with an available update (amber).
	styleUpdate = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	// styleHeader styles table column headers.
	styleHeader = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Bold(true)
)

// ── Project status helpers ───────────────────────────────────────────────────

// glyphChar returns the plain single-column status glyph for a project:
//
//	⬆ update available · ✓ running & current · ○ stopped · ? not yet checked
//
// Colour is applied separately via glyphPaint so the layout engine can measure
// the plain width correctly.
func glyphChar(p *docker.Project) string {
	switch {
	case !p.Checked():
		return "?"
	case p.UpdateCount() > 0:
		return "⬆"
	case p.FullyUnknown():
		return "?"
	case p.IsRunning():
		return "✓"
	default:
		return "○"
	}
}

// projectStatusText returns a short status word for a project.
func projectStatusText(p *docker.Project) string {
	switch {
	case !p.Checked():
		return "unchecked"
	case p.UpdateCount() > 0:
		return "update"
	case p.FullyUnknown():
		return "unknown"
	case p.IsRunning():
		return "ok"
	default:
		return "stopped"
	}
}

// runDisplay returns "running/total" (e.g. "1/1" or "0/2").
func runDisplay(p *docker.Project) string {
	if p.TotalServices > 0 {
		return fmt.Sprintf("%d/%d", p.RunningContainers, p.TotalServices)
	}
	return fmt.Sprintf("%d", p.RunningContainers)
}

// updateSummary returns totals across all projects: number of projects with
// updates and total images with updates.
func (m Model) updateSummary() (projectsWithUpdates, imageUpdates int) {
	for _, p := range m.projects {
		n := p.UpdateCount()
		if n > 0 {
			projectsWithUpdates++
			imageUpdates += n
		}
	}
	return
}

// unknownImageCount counts images whose registry state could not be determined.
func (m Model) unknownImageCount() int {
	n := 0
	for _, p := range m.projects {
		for _, img := range p.ImageInfo {
			if img.State == "unknown" {
				n++
			}
		}
	}
	return n
}

// summaryBar renders the k9s-style header line: counts + cache freshness.
func (m Model) summaryBar() string {
	pu, iu := m.updateSummary()
	anyChecked := false
	for _, p := range m.projects {
		if p.Checked() {
			anyChecked = true
			break
		}
	}

	// Build plain status text plus its colour, so we can measure widths using
	// the plain text and only apply colour once it is known to fit.
	var statusPlain string
	var statusStyle lipgloss.Style
	switch {
	case !anyChecked:
		statusPlain, statusStyle = "updates: not checked ('u' to refresh)", styleMuted
	case pu == 0:
		statusPlain, statusStyle = "✓ all up to date", styleSuccess
	default:
		statusPlain, statusStyle = fmt.Sprintf("⬆ %d project(s), %d image(s) to update", pu, iu), styleUpdate
	}

	countPlain := fmt.Sprintf("%d projects · ", len(m.projects))
	rightPlain := "cache: " + m.calculateCacheAge()
	avail := m.contentWidth()

	leftW := lipgloss.Width(countPlain) + lipgloss.Width(statusPlain)
	// Full form: left + gap + right-aligned cache age.
	if leftW+2+lipgloss.Width(rightPlain) <= avail {
		gap := avail - leftW - lipgloss.Width(rightPlain)
		if gap < 2 {
			gap = 2
		}
		return countPlain + statusStyle.Render(statusPlain) + strings.Repeat(" ", gap) + styleMuted.Render(rightPlain)
	}
	// Tight: drop the cache age; clip the status if still too long.
	if leftW <= avail {
		return countPlain + statusStyle.Render(statusPlain)
	}
	return clip(countPlain+statusPlain, avail)
}

// Messages

type operationMsg string
type errorMsg struct{ err error }
type updateCompleteMsg struct {
	projectIndex int
	projectName  string
	success      bool
	err          error
}
type allUpdatesCompleteMsg struct{}
type updatesCheckedMsg struct{}
type projectCheckProgressMsg struct {
	index int // Index of project being checked
}
type projectCheckCompleteMsg struct {
	index int // Index of project that was just checked
}
type tickMsg struct{} // Tick message to refresh view during updates

// performOperation performs a container operation asynchronously
func performOperation(project *docker.Project, operation string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch operation {
		case "start":
			err = project.Start()
		case "stop":
			err = project.Stop()
		case "restart":
			err = project.Restart()
		}

		if err != nil {
			return errorMsg{err: err}
		}

		return operationMsg(fmt.Sprintf("Successfully %sed %s", operation, project.Name))
	}
}

// performUpdates performs updates for all selected projects in parallel
func (m Model) performUpdates() tea.Cmd {
	var cmds []tea.Cmd

	for idx := range m.selectedUpdates {
		if idx >= len(m.projects) {
			continue
		}

		// Mark as updating immediately
		m.projectUpdateStatus[idx] = "updating"

		project := m.projects[idx]
		shouldRestart := m.selectedRestarts[idx]
		mode := m.updateMode

		// Create a command for this specific update
		cmd := func(idx int, p *docker.Project, restart bool, updateMode string) tea.Cmd {
			return func() tea.Msg {
				var err error

				// Determine operation based on mode
				if updateMode == "pull" {
					// Pull only mode - never restart
					err = p.PullOnly()
				} else if restart {
					// Restart mode + project selected for restart
					err = p.Update()
				} else {
					// Restart mode but project NOT selected for restart - pull only
					err = p.PullOnly()
				}

				return updateCompleteMsg{
					projectIndex: idx,
					projectName:  p.Name,
					success:      err == nil,
					err:          err,
				}
			}
		}(idx, project, shouldRestart, mode)

		cmds = append(cmds, cmd)
	}

	// Run all updates in parallel
	return tea.Batch(cmds...)
}

// checkForUpdates starts checking for available updates (sequential)
func (m Model) checkForUpdates() tea.Cmd {
	// Start with the first project
	return func() tea.Msg {
		return projectCheckProgressMsg{index: 0}
	}
}

// checkSingleProject checks a single project for updates
func (m Model) checkSingleProject(index int) tea.Cmd {
	return func() tea.Msg {
		if index >= 0 && index < len(m.projects) {
			m.projects[index].UpdateImageInfo()
		}
		return projectCheckCompleteMsg{index: index}
	}
}
