package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skpharma/docker-compose-manager/internal/docker"
)

// Screen represents different UI screens
type Screen int

const (
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
	updateProgress    string
	updatesTotal      int // Total number of updates
	updatesCompleted  int // Number of completed updates
	cacheAge          string // How old is the cache
	checkingUpdates   bool   // Currently checking for updates
	currentCheckIndex int    // Index of project currently being checked (-1 if none)
}

// NewModel creates a new UI model
func NewModel(projects []*docker.Project) Model {
	return Model{
		projects:          projects,
		screen:            ScreenMainMenu,
		cursor:            0,
		selectedUpdates:   make(map[int]bool),
		selectedRestarts:  make(map[int]bool),
		currentCheckIndex: -1,
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
			}

		case "down", "j":
			return m.handleDown()

		case "enter":
			return m.handleEnter()

		case " ", "space":
			return m.handleSpace()

		case "a", "A":
			return m.handleSelectAll()

		case "r", "R":
			return m.handleRefresh()

		case "1", "2", "3":
			// Direct menu selection in main menu
			if m.screen == ScreenMainMenu {
				num := int(msg.String()[0] - '0') // Convert char to int
				if num >= 1 && num <= 3 {
					m.cursor = num - 1
					return m.handleEnter()
				}
			}
		}

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

	case updateCompleteMsg:
		m.updatesCompleted++
		if msg.success {
			m.updateProgress += fmt.Sprintf("✓ %s updated successfully (%d/%d)\n", msg.projectName, m.updatesCompleted, m.updatesTotal)
		} else {
			m.updateProgress += styleError.Render(fmt.Sprintf("✗ %s failed: %v (%d/%d)\n", msg.projectName, msg.err, m.updatesCompleted, m.updatesTotal))
		}

		// Check if all updates are done
		if m.updatesCompleted >= m.updatesTotal {
			m.loading = false
			m.updateProgress += "\n" + styleSuccess.Render("All updates completed!")
			m.updateProgress += "\n\nPress any key to continue..."
		}
		return m, nil

	case allUpdatesCompleteMsg:
		// This message is no longer used, but kept for compatibility
		return m, nil

	case updatesCheckedMsg:
		m.checkingUpdates = false
		m.currentCheckIndex = -1
		m.cacheAge = m.calculateCacheAge()
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
		m.selectedUpdates = make(map[int]bool)
		return m, nil

	case ScreenUpdateModeSelect:
		m.screen = ScreenUpdateList
		m.cursor = 0
		return m, nil

	case ScreenUpdateRestartConfirm:
		m.screen = ScreenUpdateModeSelect
		m.cursor = 0
		return m, nil

	case ScreenUpdateConfirm:
		m.screen = ScreenUpdateList
		m.cursor = 0
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
			m.message = ""
			return m, nil

		case 1: // Perform Updates
			m.screen = ScreenUpdateList
			m.cursor = 0
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
		return m, m.performUpdates()

	case ScreenUpdateConfirm:
		if m.cursor == 0 { // Yes - perform updates
			m.screen = ScreenUpdating
			m.loading = true
			m.updateProgress = ""
			m.updatesTotal = len(m.selectedUpdates)
			m.updatesCompleted = 0
			return m, m.performUpdates()
		} else { // No - back to list
			m.screen = ScreenUpdateList
			m.cursor = 0
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
	if m.quitting {
		return styleInfo.Render("Goodbye!\n")
	}

	switch m.screen {
	case ScreenMainMenu:
		return m.viewMainMenu()
	case ScreenContainerList:
		return m.viewContainerList()
	case ScreenContainerDetail:
		return m.viewContainerDetail()
	case ScreenActionMenu:
		return m.viewActionMenu()
	case ScreenUpdateList:
		return m.viewUpdateList()
	case ScreenUpdateModeSelect:
		return m.viewUpdateModeSelect()
	case ScreenUpdateRestartConfirm:
		return m.viewUpdateRestartConfirm()
	case ScreenUpdateConfirm:
		return m.viewUpdateConfirm()
	case ScreenUpdating:
		return m.viewUpdating()
	case ScreenLoading:
		return m.viewLoading()
	case ScreenHelp:
		return m.viewHelp()
	case ScreenConfirmExit:
		return m.viewConfirmExit()
	}

	return "Unknown screen"
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

	b.WriteString(styleTitle.Render("Docker Compose Manager v2.0"))
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

	for i, project := range m.projects {
		cursor := " "
		name := project.Name
		status := project.StatusDisplay()

		// Pad the name to fixed width BEFORE styling
		paddedName := fmt.Sprintf("%-20s", name)

		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			paddedName = styleHighlight.Render(paddedName)
		}

		// Color status
		statusStyled := status
		if project.IsRunning() {
			statusStyled = styleSuccess.Render(status)
		} else {
			statusStyled = styleMuted.Render(status)
		}

		b.WriteString(fmt.Sprintf("%s %s %s\n", cursor, paddedName, statusStyled))
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Use ↑/↓ to navigate, Enter to select, Esc/q to go back"))

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

	// Show cache age and refresh status
	if m.checkingUpdates {
		b.WriteString(styleInfo.Render("⏳ Checking for updates..."))
		b.WriteString("\n\n")
	} else if m.cacheAge != "" {
		b.WriteString(styleMuted.Render(fmt.Sprintf("Cache updated: %s", m.cacheAge)))
		b.WriteString("\n\n")
	}

	for i, project := range m.projects {
		cursor := " "
		checkbox := "[ ]"
		name := project.Name

		if m.selectedUpdates[i] {
			checkbox = "[✓]"
		}

		paddedName := fmt.Sprintf("%-20s", name)

		// Show spinner if this project is currently being checked
		spinner := ""
		if m.checkingUpdates && m.currentCheckIndex == i {
			spinner = styleInfo.Render(" ⏳")
		}

		// Show version info for ALL images (not just those with updates)
		updateInfo := ""
		if len(project.ImageInfo) > 0 {
			imageCount := len(project.ImageInfo)
			updateCount := 0

			// Collect version details for ALL images
			var versions []string
			for _, img := range project.ImageInfo {
				// Extract just the image name without registry
				imgShortName := img.Name
				if strings.Contains(imgShortName, "/") {
					parts := strings.Split(imgShortName, "/")
					imgShortName = parts[len(parts)-1]
				}
				// Remove tag from name for display
				if strings.Contains(imgShortName, ":") {
					imgShortName = strings.Split(imgShortName, ":")[0]
				}

				// Show version for this image
				var versionInfo string
				if img.HasUpdate {
					updateCount++
					versionInfo = fmt.Sprintf("%s: %s → %s", imgShortName, img.CurrentVersion, img.LatestVersion)
				} else {
					// Show current version even if no update
					versionInfo = fmt.Sprintf("%s: %s", imgShortName, img.CurrentVersion)
				}
				versions = append(versions, versionInfo)
			}

			if updateCount > 0 {
				// Show version details
				if imageCount == 1 {
					updateInfo = styleSuccess.Render(fmt.Sprintf(" [%s]", versions[0]))
				} else {
					updateInfo = styleSuccess.Render(fmt.Sprintf(" [%s]", strings.Join(versions, "; ")))
				}
			} else if imageCount > 0 {
				// No updates but show versions anyway
				if imageCount == 1 {
					updateInfo = styleMuted.Render(fmt.Sprintf(" [%s]", versions[0]))
				} else {
					updateInfo = styleMuted.Render(fmt.Sprintf(" [%s]", strings.Join(versions, "; ")))
				}
			}
		}

		if m.cursor == i {
			cursor = styleHighlight.Render(">")
			paddedName = styleHighlight.Render(paddedName)
		}

		b.WriteString(fmt.Sprintf("%s %s %s%s%s\n", cursor, checkbox, paddedName, spinner, updateInfo))
	}

	b.WriteString("\n")
	selectedCount := len(m.selectedUpdates)
	if selectedCount > 0 {
		b.WriteString(styleInfo.Render(fmt.Sprintf("Selected: %d project(s)", selectedCount)))
		b.WriteString("\n")
	}
	b.WriteString(styleHelp.Render("Space to select, 'a' select all, 'r' refresh, Enter to continue, Esc/q to go back"))

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

// viewUpdating renders the updating progress screen
func (m Model) viewUpdating() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Updating Projects"))
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(styleInfo.Render("⏳ Performing updates...\n\n"))
	}

	if m.updateProgress != "" {
		b.WriteString(m.updateProgress)
	}

	return styleBox.Render(b.String())
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
		b.WriteString(styleHighlight.Render(fmt.Sprintf("%-20s ", "Image")))
		b.WriteString(styleHighlight.Render(fmt.Sprintf("%-12s ", "Tag")))
		b.WriteString(styleHighlight.Render(fmt.Sprintf("%-15s ", "Lokal")))
		b.WriteString(styleHighlight.Render("Repository"))
		b.WriteString("\n")
		b.WriteString(styleMuted.Render("  ──────  ────────────────────  ────────────  ───────────────  ──────────────"))
		b.WriteString("\n")

		for _, img := range m.selectedProject.ImageInfo {
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

			b.WriteString(statusStyle.Render(fmt.Sprintf("  %s     ", status)))
			b.WriteString(styleInfo.Render(fmt.Sprintf("%-20s  ", imgName)))
			b.WriteString(styleMuted.Render(fmt.Sprintf("%-12s  ", imgTag)))
			b.WriteString(fmt.Sprintf("%-15s  ", img.CurrentVersion))
			b.WriteString(fmt.Sprintf("%s\n", img.LatestVersion))
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
	projectName string
	success     bool
	err         error
}
type allUpdatesCompleteMsg struct{}
type updatesCheckedMsg struct{}
type projectCheckProgressMsg struct {
	index int // Index of project being checked
}
type projectCheckCompleteMsg struct {
	index int // Index of project that was just checked
}

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

// performUpdates performs updates for all selected projects
func (m Model) performUpdates() tea.Cmd {
	// Create commands for each project update
	var cmds []tea.Cmd

	for idx := range m.selectedUpdates {
		if idx >= len(m.projects) {
			continue
		}

		project := m.projects[idx]
		shouldRestart := m.selectedRestarts[idx]
		mode := m.updateMode

		// Create a command for this specific update
		cmd := func(p *docker.Project, restart bool, updateMode string) tea.Cmd {
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
					projectName: p.Name,
					success:     err == nil,
					err:         err,
				}
			}
		}(project, shouldRestart, mode)

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
