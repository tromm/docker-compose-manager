package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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
	ScreenContainerDetail       // Show containers in a project (aptitude-style)
	ScreenActionMenu
	ScreenUpdateList
	ScreenUpdateModeSelect      // Choose: Pull only or Pull & Restart
	ScreenUpdateRestartConfirm  // Select which projects to restart
	ScreenUpdateConfirm         // Old confirmation (kept for compatibility)
	ScreenUpdating
	ScreenLoading
	ScreenHelp                  // Help & Documentation screen
	ScreenConfirmExit
)

// Model represents the UI state
type Model struct {
	projects          []*docker.Project
	screen            Screen
	cursor            int
	selectedProject   *docker.Project
	selectedUpdates   map[int]bool // Projects selected for update
	selectedRestarts  map[int]bool // Projects selected for restart (subset of selectedUpdates)
	updateMode        string       // "pull" or "restart"
	message           string
	loading           bool
	err               error
	quitting          bool
	updateProgress       string
	updatesTotal         int               // Total number of updates
	updatesCompleted     int               // Number of completed updates
	projectUpdateStatus  map[int]string    // Status for each project: "pending", "updating", "success", "failed"
	projectUpdateResult  map[int]string    // Result message for each project
	currentUpdateIndex   int               // Index of project currently being updated
	cacheAge             string            // How old is the cache
	checkingUpdates      bool              // Currently checking for updates
	currentCheckIndex    int               // Index of project currently being checked (-1 if none)
	cacheFile            string            // Path to cache file for saving
	viewportOffset       int               // Scroll offset for long lists
	width                int               // Terminal width
	height               int               // Terminal height
	debugMode            bool              // Enable debug logging
	viewRenderCount      int               // Count how many times View() is called
}

// truncateMiddle truncates a string in the middle if it exceeds maxLen
// Keeps the last 3 characters visible with "..." in the middle
// Example: "super-lange-bookworm-version-mit-krass-vielen-zeichen" (maxLen=30)
//       -> "super-lange-bookworm-ver...hen"
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

// ColumnWidths holds the calculated column widths for the update list
type ColumnWidths struct {
	Project    int
	Image      int
	Tag        int
	Local      int
	Repository int
}

// calculateColumnWidths returns optimal column widths based on terminal width
func (m Model) calculateColumnWidths() ColumnWidths {
	// Fixed columns and padding:
	// "  " (2) + cursor (1) + " " (1) + Nr (4) + " " (1) + Sel (4) + " " (1) = 14 chars
	// Padding: after Project (2), Image (2), Tag (2), Local (2), Repo (2) = 10 spaces
	fixedOverhead := 14 + 10

	// Default widths for small terminals
	// Project und Image: 50% der Breite von Tag/Lokal/Repo
	cw := ColumnWidths{
		Project:    12,  // 50% von 25
		Image:      12,  // 50% von 25
		Tag:        25,
		Local:      25,
		Repository: 35,
	}

	// If we have terminal width info, calculate widths ONCE and keep them fixed
	if m.width > 80 {
		availableWidth := m.width - fixedOverhead

		// Distribute: Project=10%, Image=10%, Tag=20%, Local=25%, Repository=35%
		cw.Project = max(12, availableWidth*10/100)
		cw.Image = max(12, availableWidth*10/100)
		cw.Tag = max(25, availableWidth*20/100)
		cw.Local = max(25, availableWidth*25/100)
		cw.Repository = max(35, availableWidth*35/100)
	}

	return cw
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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
			if m.cursor >= m.viewportOffset+maxVisibleItems {
				m.viewportOffset = m.cursor - maxVisibleItems + 1
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
		if m.cursor < len(m.projects)-1 {
			m.cursor++
			// Adjust viewport if cursor moves below visible area
			if m.cursor >= m.viewportOffset+maxVisibleItems {
				m.viewportOffset = m.cursor - maxVisibleItems + 1
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
		// Toggle selection for current project
		if m.selectedUpdates[m.cursor] {
			delete(m.selectedUpdates, m.cursor)
		} else {
			m.selectedUpdates[m.cursor] = true
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

// handleSelectAll handles 'a' key for selecting/deselecting all
func (m Model) handleSelectAll() (tea.Model, tea.Cmd) {
	if m.screen == ScreenUpdateList {
		// If all selected, deselect all. Otherwise select all
		if len(m.selectedUpdates) == len(m.projects) {
			m.selectedUpdates = make(map[int]bool)
		} else {
			for i := range m.projects {
				m.selectedUpdates[i] = true
			}
		}
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
		viewEnd := m.viewportOffset + maxVisibleItems
		if viewEnd > len(m.projects) {
			viewEnd = len(m.projects)
		}
		visibleCount := viewEnd - viewStart

		if num >= 1 && num <= visibleCount {
			m.cursor = viewStart + num - 1
			return m.handleEnter()
		}

	case ScreenUpdateList:
		// Update list - select Nth visible project
		viewStart := m.viewportOffset
		viewEnd := m.viewportOffset + maxVisibleItems
		if viewEnd > len(m.projects) {
			viewEnd = len(m.projects)
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

	options := []string{
		"Manage Containers (Start/Stop/Restart)",
		"Perform Updates",
		"Help & Documentation",
	}

	for i, option := range options {
		cursor := " "
		number := fmt.Sprintf("[%d]", i+1)

		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			option = styleHighlight.Render(option)
			number = styleHighlight.Render(number)
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

// viewContainerList renders the container list
func (m Model) viewContainerList() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Manage Containers"))
	b.WriteString("\n\n")

	// Calculate visible range for scrolling
	viewStart := m.viewportOffset
	viewEnd := m.viewportOffset + maxVisibleItems
	if viewEnd > len(m.projects) {
		viewEnd = len(m.projects)
	}

	// Scroll indicators (fixed space to prevent layout shift)
	if viewStart > 0 {
		b.WriteString(styleMuted.Render("▲ More above - scroll up\n"))
	} else {
		b.WriteString("\n")
	}

	for i := viewStart; i < viewEnd; i++ {
		project := m.projects[i]
		cursor := " "
		name := project.Name
		status := project.StatusDisplay()

		// Display absolute position number (1-based index)
		displayNum := i + 1
		number := fmt.Sprintf("[%d]", displayNum)

		// Pad the name to fixed width BEFORE styling
		paddedName := fmt.Sprintf("%-20s", name)

		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			number = styleHighlight.Render(number)
			paddedName = styleHighlight.Render(paddedName)
		}

		// Color status
		statusStyled := status
		if project.IsRunning() {
			statusStyled = styleSuccess.Render(status)
		} else {
			statusStyled = styleMuted.Render(status)
		}

		b.WriteString(fmt.Sprintf("%s %s %s %s\n", cursor, number, paddedName, statusStyled))
	}

	// Bottom scroll indicator (fixed space)
	if viewEnd < len(m.projects) {
		b.WriteString(styleMuted.Render("▼ More below - scroll down\n"))
	} else {
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Use ↑/↓ or 1-9/0 to navigate, Enter to select, Esc/q to go back"))

	if m.message != "" {
		b.WriteString("\n\n")
		b.WriteString(styleInfo.Render(m.message))
	}

	return styleBox.Render(b.String())
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

// viewUpdateList renders the update selection screen
func (m Model) viewUpdateList() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Select Projects to Update"))
	b.WriteString("\n\n")

	// Show cache age and refresh status - PLAIN TEXT
	if m.checkingUpdates {
		b.WriteString("⏳ Checking for updates...")
		b.WriteString("\n\n")
	} else if m.cacheAge != "" {
		b.WriteString(fmt.Sprintf("Cache updated: %s", m.cacheAge))
		b.WriteString("\n\n")
	}

	// Get dynamic column widths
	cw := m.calculateColumnWidths()

	// Table header - PLAIN TEXT (no styleHighlight)
	headerLine := fmt.Sprintf("  %s %-4s %-4s %-*s  %-*s  %-*s  %-*s  %-*s",
		" ", "Nr", "Sel",
		cw.Project, "Project",
		cw.Image, "Image",
		cw.Tag, "Tag",
		cw.Local, "Lokal",
		cw.Repository, "Repository")
	b.WriteString(headerLine)
	b.WriteString("\n")

	// Separator line - PLAIN TEXT (no styleMuted)
	separatorLine := fmt.Sprintf("  %s ──── ──── %s  %s  %s  %s  %s",
		" ", // cursor column
		strings.Repeat("─", cw.Project),
		strings.Repeat("─", cw.Image),
		strings.Repeat("─", cw.Tag),
		strings.Repeat("─", cw.Local),
		strings.Repeat("─", cw.Repository))
	b.WriteString(separatorLine)
	b.WriteString("\n")

	// Calculate visible range for scrolling
	viewStart := m.viewportOffset
	viewEnd := m.viewportOffset + maxVisibleItems
	if viewEnd > len(m.projects) {
		viewEnd = len(m.projects)
	}

	// Show scroll indicators - PLAIN TEXT
	if viewStart > 0 {
		b.WriteString("  ▲ More above - scroll up")
		b.WriteString("\n")
	} else {
		b.WriteString("\n") // Empty line to maintain layout
	}

	for i := viewStart; i < viewEnd; i++ {
		project := m.projects[i]
		cursor := " "
		checkbox := "[ ]"

		if m.selectedUpdates[i] {
			checkbox = "[✓]"
		}

		// Display absolute position number (1-based index)
		displayNum := i + 1
		number := fmt.Sprintf("[%d]", displayNum)

		// Show spinner if this project is currently being checked
		spinner := ""
		if m.checkingUpdates && m.currentCheckIndex == i {
			spinner = " ⏳"
		}

		// Determine if this item is highlighted
		isHighlighted := (m.cursor == i)
		if isHighlighted {
			cursor = "›"
		}

		// Get image info
		if len(project.ImageInfo) == 0 {
			// No image info - show project name only with message to refresh
			// Match exact header format
			projectNameTrunc := truncateMiddle(project.Name, cw.Project)
			line := fmt.Sprintf("  %s %-4s %-4s %-*s  ", cursor, number, checkbox, cw.Project, projectNameTrunc)
			if spinner != "" {
				line += spinner + " "
			}
			line += styleMuted.Render("(press 'u' to update cache)")

			if isHighlighted {
				// Only highlight the table columns, not the message
				tablepart := fmt.Sprintf("  %s %-4s %-4s %-*s  ", cursor, number, checkbox, cw.Project, projectNameTrunc)
				line = styleHighlight.Render(tablepart)
				if spinner != "" {
					line += spinner + " "
				}
				line += styleMuted.Render("(press 'u' to update cache)")
			}
			b.WriteString(line)
			b.WriteString("\n")
		} else {
			// Check if project has any updates
			hasUpdates := false
			for _, img := range project.ImageInfo {
				if img.HasUpdate {
					hasUpdates = true
					break
				}
			}

			// Sort image names for consistent display order
			imageNames := make([]string, 0, len(project.ImageInfo))
			for imgName := range project.ImageInfo {
				imageNames = append(imageNames, imgName)
			}
			sort.Strings(imageNames)

			// Show first image on same line as project name
			firstImg := true
			imgCount := 0
			for _, imgKey := range imageNames {
				img := project.ImageInfo[imgKey]
				// Extract image name and tag
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

				// Prepare all version data BEFORE building lines
				localVersion := truncateMiddle(img.CurrentVersion, cw.Local)
				repoVersion := truncateMiddle(img.LatestVersion, cw.Repository)
				imgNameTrunc := truncateMiddle(imgName, cw.Image)
				imgTagTrunc := truncateMiddle(imgTag, cw.Tag)

				// Build complete line
				var line string
				if firstImg {
					// First image: show complete line with project name
					updateIndicator := "  "
					if hasUpdates {
						updateIndicator = "⬆ "
					}
					// Build COMPLETE line: 2sp + cursor(1) + sp + number(4) + sp + checkbox(4) + sp + name(16) + indicator(2) + 2sp + image(20) + 2sp + tag(12) + 2sp + local(15) + 2sp + repo
					projectNameTrunc := truncateMiddle(project.Name, cw.Project-2) // -2 for update indicator
					line = fmt.Sprintf("  %s %-4s %-4s %-*s%s  %-*s  %-*s  %-*s  %-*s",
						cursor, number, checkbox,
						cw.Project-2, projectNameTrunc, updateIndicator,
						cw.Image, imgNameTrunc,
						cw.Tag, imgTagTrunc,
						cw.Local, localVersion,
						cw.Repository, repoVersion)

					// Add spinner to first image if updating
					if imgCount == 0 && spinner != "" {
						line += spinner
					}

					// Apply highlighting if selected
					if isHighlighted {
						line = styleHighlight.Render(line)
					}
					firstImg = false
				} else {
					// Additional images: empty project columns + version info
					line = fmt.Sprintf("  %s %-4s %-4s %-*s  %-*s  %-*s  %-*s  %-*s",
						" ", "", "",
						cw.Project, "",
						cw.Image, imgNameTrunc,
						cw.Tag, imgTagTrunc,
						cw.Local, localVersion,
						cw.Repository, repoVersion)
				}

				b.WriteString(line)
				b.WriteString("\n")
				imgCount++
			}

			// Add separator line after each project (except last in viewport)
			if i < viewEnd-1 {
				projectSep := fmt.Sprintf("  ┄┄┄┄ %s  %s  %s  %s  %s",
					strings.Repeat("─", cw.Project),
					strings.Repeat("─", cw.Image),
					strings.Repeat("─", cw.Tag),
					strings.Repeat("─", cw.Local),
					strings.Repeat("─", cw.Repository))
				b.WriteString(styleMuted.Render(projectSep))
				b.WriteString("\n")
			}
		}
	}

	// Show scroll down indicator - PLAIN TEXT
	b.WriteString("\n")
	if viewEnd < len(m.projects) {
		b.WriteString("  ▼ More below - scroll down")
		b.WriteString("\n")
	} else {
		b.WriteString("\n") // Empty line to maintain layout
	}

	b.WriteString("\n")
	selectedCount := len(m.selectedUpdates)
	if selectedCount > 0 {
		b.WriteString(fmt.Sprintf("Selected: %d project(s)", selectedCount))
		b.WriteString("\n")
	} else {
		// Always reserve space for the "Selected" line to maintain consistent height
		b.WriteString("\n")
	}
	b.WriteString(styleHelp.Render("1-9/0 or Space to select, 'a' select all, 'u' update cache, Enter to continue, Esc/q to go back"))

	return styleBox.Render(b.String())
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

	// Overall progress bar
	progressPercent := 0
	if m.updatesTotal > 0 {
		progressPercent = (m.updatesCompleted * 100) / m.updatesTotal
	}
	progressBar := renderProgressBar(progressPercent, 50)
	b.WriteString(fmt.Sprintf("Overall Progress: %s %3d%% (%d/%d)\n\n",
		progressBar, progressPercent, m.updatesCompleted, m.updatesTotal))

	// Table header (no separator line)
	b.WriteString(styleMuted.Render(fmt.Sprintf("%-4s  %-30s  %-30s  %s\n",
		"Nr.", "Name", "Ergebnis", "Status")))
	b.WriteString("\n")

	// Table rows - show all selected projects
	rowNum := 1
	for i, project := range m.projects {
		if !m.selectedUpdates[i] {
			continue
		}

		status := m.projectUpdateStatus[i]
		result := m.projectUpdateResult[i]
		statusIcon := ""

		switch status {
		case "pending":
			statusIcon = ""
		case "updating":
			statusIcon = "⏳" // Double-width emoji (2 chars)
		case "success":
			statusIcon = "✓ " // Single-width + space to match hourglass width
		case "failed":
			statusIcon = "✗ " // Single-width + space to match hourglass width
		}

		// Truncate long names and results
		name := project.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		if len(result) > 30 {
			result = result[:27] + "..."
		}

		// Format columns
		numStr := fmt.Sprintf("%-4d", rowNum)
		nameStr := fmt.Sprintf("%-30s", name)

		// Result column: color the text, then pad to 30 chars
		// Use utf8.RuneCountInString to count visible characters, not bytes
		var resultStr string
		if result == "" {
			resultStr = strings.Repeat(" ", 30)
		} else if status == "success" {
			visibleLen := utf8.RuneCountInString(result)
			resultStr = styleSuccess.Render(result) + strings.Repeat(" ", 30-visibleLen)
		} else if status == "failed" {
			visibleLen := utf8.RuneCountInString(result)
			resultStr = styleError.Render(result) + strings.Repeat(" ", 30-visibleLen)
		} else {
			visibleLen := utf8.RuneCountInString(result)
			resultStr = result + strings.Repeat(" ", 30-visibleLen)
		}

		// Status column: just the icon without padding (no fixed width needed)
		var statusStr string
		if statusIcon == "" {
			statusStr = ""
		} else if status == "success" {
			statusStr = styleSuccess.Render(statusIcon)
		} else if status == "failed" {
			statusStr = styleError.Render(statusIcon)
		} else if status == "updating" {
			statusStr = styleHighlight.Render(statusIcon)
		} else {
			statusStr = statusIcon
		}

		// Build line
		b.WriteString(numStr)
		b.WriteString("  ")
		b.WriteString(nameStr)
		b.WriteString("  ")
		b.WriteString(resultStr)
		b.WriteString("  ")
		b.WriteString(statusStr)
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
		// Column headers
		b.WriteString(styleHighlight.Render("  Status  "))
		b.WriteString(styleHighlight.Render(fmt.Sprintf("%-20s  ", "Image")))
		b.WriteString(styleHighlight.Render(fmt.Sprintf("%-12s  ", "Tag")))
		b.WriteString(styleHighlight.Render(fmt.Sprintf("%-15s  ", "Lokal")))
		b.WriteString(styleHighlight.Render("Repository"))
		b.WriteString("\n")
		b.WriteString(styleMuted.Render("  ──────  ────────────────────  ────────────  ───────────────  ──────────────"))
		b.WriteString("\n")

		// Sort image names for consistent display order
		imageNames := make([]string, 0, len(m.selectedProject.ImageInfo))
		for imgName := range m.selectedProject.ImageInfo {
			imageNames = append(imageNames, imgName)
		}
		sort.Strings(imageNames)

		for _, imgKey := range imageNames {
			img := m.selectedProject.ImageInfo[imgKey]
			// Extract image name and tag
			imgName := img.Name
			imgTag := "latest"

			if strings.Contains(imgName, "/") {
				parts := strings.Split(imgName, "/")
				imgName = parts[len(parts)-1]
			}

			// Extract tag from image name
			if strings.Contains(imgName, ":") {
				parts := strings.Split(imgName, ":")
				imgName = parts[0]
				imgTag = parts[1]
			}

			// Show image with version
			status := "✓"
			statusStyle := styleSuccess

			if img.HasUpdate {
				status = "⬆"
				statusStyle = styleHighlight
			}

			b.WriteString(statusStyle.Render(fmt.Sprintf("  %-6s  ", status)))
			b.WriteString(styleInfo.Render(fmt.Sprintf("%-20s  ", truncateMiddle(imgName, 20))))
			b.WriteString(styleMuted.Render(fmt.Sprintf("%-12s  ", truncateMiddle(imgTag, 12))))
			b.WriteString(fmt.Sprintf("%-15s  ", truncateMiddle(img.CurrentVersion, 15)))
			b.WriteString(fmt.Sprintf("%s\n", truncateMiddle(img.LatestVersion, 15)))
		}
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Press Esc/q to go back, Enter to select action"))

	return styleBox.Render(b.String())
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
)

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
