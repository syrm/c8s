package tui

import (
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/syrm/c8s/internal/model"
)

// View type constants.
type currentView int

const (
	viewProjectList currentView = iota
	viewContainerList
	viewContainerLog
)

// Timing constants.
const (
	refreshPauseDuration     = 5 * time.Second
	statusMessageDuration    = 5 * time.Second
	resourceWarningThreshold = 80.0
)

// Pending action constants.
const (
	actionStopping   = "stopping"
	actionStarting   = "starting"
	actionRestarting = "restarting"
	actionRemoving   = "removing"
)

// Model is the main Bubble Tea model for c8s.
type Model struct {
	// Current view
	view currentView

	// Navigation state
	projectID        string
	projectName      string
	containerID      string
	containerName    string
	containerService string

	// Data
	projects   []model.Project
	containers []model.Container
	logs       []string

	// Selection cursors
	projectCursor   int
	containerCursor int

	// Sorting
	projectSort   sortState[projectSortColumn]
	containerSort sortState[containerSortColumn]

	// Search / filter
	searching       bool
	searchInput     textinput.Model
	searchQuery     string
	logFilter       string
	logFilterActive bool
	logFilterInput  textinput.Model

	// Log view state
	logViewport   viewport.Model
	logPaused     bool
	showTimestamp bool
	disappeared   bool

	// Help (bubbles/help component)
	showHelp  bool
	helpModel help.Model

	// Spinner for pending actions
	spinner spinner.Model

	// Progress bar for initial loading
	progress progress.Model
	loading  bool

	// Sparkline history per project/container ID
	sparklines map[string]*metricHistory

	// Last refresh time for timer display
	lastRefresh time.Time

	// Status bar
	statusMsg string

	// Screen dimensions
	width  int
	height int

	// Refresh pause
	projectPaused   bool
	containerPaused bool
	pauseResumeTime time.Time

	// Communication
	requestData chan model.RequestData

	// Lifecycle
	closing bool
	logger  *slog.Logger
}

// New creates a new Model for the Bubble Tea TUI.
func New(logger *slog.Logger) *Model {
	si := textinput.New()
	si.Placeholder = "filter..."
	si.Prompt = "Filter: "

	lf := textinput.New()
	lf.Placeholder = "filter logs..."
	lf.Prompt = "Filter: "

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true
	vp.LeftGutterFunc = func(info viewport.GutterContext) string {
		if info.Soft {
			return gutterSoftStyle.Render("     │ ")
		}
		if info.Index >= info.TotalLines {
			return gutterStyle.Render("   ~ │ ")
		}
		return gutterStyle.Render(fmt.Sprintf("%4d │ ", info.Index+1))
	}

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = spinnerStyle

	h := help.New()
	h.ShowAll = true

	prog := progress.New(
		progress.WithDefaultBlend(),
		progress.WithoutPercentage(),
	)

	m := &Model{
		view:           viewProjectList,
		projectSort:    sortState[projectSortColumn]{column: projectSortCPU, asc: false},
		containerSort:  sortState[containerSortColumn]{column: containerSortCPU, asc: false},
		searchInput:    si,
		logFilterInput: lf,
		logViewport:    vp,
		helpModel:      h,
		spinner:        sp,
		progress:       prog,
		loading:        true,
		sparklines:     make(map[string]*metricHistory),
		requestData:    make(chan model.RequestData),
		logger:         logger,
	}
	return m
}

// GetRequestData returns the request channel for the Docker layer.
func (m *Model) GetRequestData() <-chan model.RequestData {
	return m.requestData
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), m.spinner.Tick, tea.RequestWindowSize)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logViewport.SetWidth(msg.Width - 11) // account for gutter
		m.logViewport.SetHeight(msg.Height - 6)
		m.progress.SetWidth(msg.Width - 10)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd

	case tickMsg:
		return m.handleTick()

	case projectsMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		m.projects = msg
		m.updateSparklines()
		return m, nil

	case containersMsg:
		m.lastRefresh = time.Now()
		m.containers = msg
		m.updateContainerSparklines()
		return m, nil

	case containerLogMsg:
		return m.handleContainerLogMsg(msg)

	case statusMsg:
		m.statusMsg = string(msg)
		return m, clearStatusCmd(statusMessageDuration)

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case shellFinishedMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Shell exited with error: %v", msg.err)
			return m, clearStatusCmd(statusMessageDuration)
		}
		return m, nil

	case actionResultMsg:
		if msg.err != nil {
			m.statusMsg = msg.message
			return m, clearStatusCmd(statusMessageDuration)
		}
		return m, nil
	}

	return m, nil
}

// updateSparklines records current CPU/MEM values for all projects.
func (m *Model) updateSparklines() {
	for _, p := range m.projects {
		key := string(p.ID)
		h, ok := m.sparklines[key]
		if !ok {
			h = &metricHistory{}
			m.sparklines[key] = h
		}
		h.push(p.CPUPercentage, p.MemoryPercentage)
	}
}

// updateContainerSparklines records current CPU/MEM values for all containers.
func (m *Model) updateContainerSparklines() {
	for _, c := range m.containers {
		key := string(c.ID)
		h, ok := m.sparklines[key]
		if !ok {
			h = &metricHistory{}
			m.sparklines[key] = h
		}
		h.push(c.CPUPercentage, c.MemoryPercentage)
	}
}

// handleKeyMsg handles keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// If searching, handle search input
	if m.searching {
		return m.handleSearchInput(msg)
	}

	// If filtering logs, handle filter input
	if m.logFilterActive {
		return m.handleLogFilterInput(msg)
	}

	// If help is shown, only allow closing it
	if m.showHelp {
		if msg.String() == "esc" || msg.String() == "q" || msg.String() == "?" {
			m.showHelp = false
		}
		return m, nil
	}

	// If disappeared modal shown, handle OK
	if m.disappeared {
		if msg.String() == "enter" || msg.String() == "esc" {
			m.disappeared = false
			m.view = viewContainerList
			m.logPaused = false
			m.logFilter = ""
			m.logs = nil
		}
		return m, nil
	}

	// Quit
	if msg.String() == "q" || msg.String() == "ctrl+c" {
		m.closing = true
		close(m.requestData)
		return m, tea.Quit
	}

	switch m.view {
	case viewProjectList:
		return m.handleProjectKeys(msg)
	case viewContainerList:
		return m.handleContainerKeys(msg)
	case viewContainerLog:
		return m.handleLogKeys(msg)
	}

	return m, nil
}

// handleProjectKeys handles keys in project list view.
func (m *Model) handleProjectKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.projectCursor > 0 {
			m.projectCursor--
			m.pauseProjectRefresh()
		}
	case "down", "j":
		filtered := m.filteredProjects()
		if m.projectCursor < len(filtered)-1 {
			m.projectCursor++
			m.pauseProjectRefresh()
		}
	case "enter", "right", "l":
		return m.enterContainerView()
	case "/":
		m.searching = true
		m.searchInput.SetValue(m.searchQuery)
		m.searchInput.Focus()
		return m, nil
	case "c":
		if m.searchQuery != "" {
			m.searchQuery = ""
			m.projectCursor = 0
		}
	case "N":
		m.projectSort.toggle(projectSortName, true)
	case "C":
		m.projectSort.toggle(projectSortCPU, false)
	case "M":
		m.projectSort.toggle(projectSortMemory, false)
	case "O":
		m.projectSort.toggle(projectSortContainers, false)
	case "?":
		m.showHelp = true
	}
	return m, nil
}

// handleContainerKeys handles keys in container list view.
func (m *Model) handleContainerKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.containerCursor > 0 {
			m.containerCursor--
			m.pauseContainerRefresh()
		}
	case "down", "j":
		filtered := m.filteredContainers()
		if m.containerCursor < len(filtered)-1 {
			m.containerCursor++
			m.pauseContainerRefresh()
		}
	case "enter", "right", "l":
		return m.enterLogView()
	case "esc", "left", "h":
		return m.exitContainerView()
	case "/":
		m.searching = true
		m.searchInput.SetValue(m.searchQuery)
		m.searchInput.Focus()
		return m, nil
	case "c":
		if m.searchQuery != "" {
			m.searchQuery = ""
			m.containerCursor = 0
		}
	case "N":
		m.containerSort.toggle(containerSortName, true)
	case "C":
		m.containerSort.toggle(containerSortCPU, false)
	case "M":
		m.containerSort.toggle(containerSortMemory, false)
	case "S":
		m.containerSort.toggle(containerSortStatus, true)
	case "s":
		return m.handleShell()
	case "x":
		return m.handleStop()
	case "r":
		return m.handleRestart()
	case "d":
		return m.handleRemove()
	case "?":
		m.showHelp = true
	}
	return m, nil
}

// handleLogKeys handles keys in log view.
func (m *Model) handleLogKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "left", "h":
		return m.exitLogView()
	case "p":
		m.logPaused = !m.logPaused
	case "t":
		m.showTimestamp = !m.showTimestamp
	case "/":
		m.logFilterActive = true
		m.logFilterInput.SetValue(m.logFilter)
		m.logFilterInput.Focus()
		return m, nil
	case "c":
		if m.logFilter != "" {
			m.logFilter = ""
		}
	case "n":
		m.logViewport.HighlightNext()
	case "shift+n":
		m.logViewport.HighlightPrevious()
	case "?":
		m.showHelp = true
	default:
		// Pass through to viewport for scrolling
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleSearchInput handles text input for search.
func (m *Model) handleSearchInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searchQuery = m.searchInput.Value()
		m.searching = false
		m.searchInput.Blur()
		if m.view == viewProjectList {
			m.projectCursor = 0
		} else {
			m.containerCursor = 0
		}
		return m, nil
	case "esc":
		m.searching = false
		m.searchInput.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	// Live filter
	m.searchQuery = m.searchInput.Value()
	if m.view == viewProjectList {
		m.projectCursor = 0
	} else {
		m.containerCursor = 0
	}
	return m, cmd
}

// handleLogFilterInput handles text input for log filter.
func (m *Model) handleLogFilterInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.logFilter = m.logFilterInput.Value()
		m.logFilterActive = false
		m.logFilterInput.Blur()
		return m, nil
	case "esc":
		m.logFilterActive = false
		m.logFilterInput.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.logFilterInput, cmd = m.logFilterInput.Update(msg)
	return m, cmd
}

// handleTick handles periodic data refresh.
func (m *Model) handleTick() (tea.Model, tea.Cmd) {
	if m.closing {
		return m, nil
	}

	// Check pause expiry
	if !m.pauseResumeTime.IsZero() && time.Now().After(m.pauseResumeTime) {
		m.projectPaused = false
		m.containerPaused = false
		m.pauseResumeTime = time.Time{}
	}

	var fetchCmd tea.Cmd
	switch m.view {
	case viewProjectList:
		if !m.projectPaused {
			fetchCmd = m.fetchProjectsCmd()
		}
	case viewContainerList:
		if !m.containerPaused {
			fetchCmd = m.fetchContainersCmd()
		}
	case viewContainerLog:
		if !m.logPaused {
			fetchCmd = m.fetchContainerLogCmd()
		}
	}

	return m, tea.Batch(tickCmd(), fetchCmd)
}

// handleContainerLogMsg handles a container log response.
func (m *Model) handleContainerLogMsg(msg containerLogMsg) (tea.Model, tea.Cmd) {
	if !msg.found {
		if m.view == viewContainerLog && !m.disappeared {
			m.disappeared = true
		}
		return m, nil
	}

	m.disappeared = false
	if len(msg.container.Logs) > 0 {
		m.logs = msg.container.Logs
		m.updateLogViewport()
	}
	return m, nil
}

// enterContainerView navigates from project list to container list.
func (m *Model) enterContainerView() (tea.Model, tea.Cmd) {
	filtered := m.filteredProjects()
	if m.projectCursor >= len(filtered) {
		return m, nil
	}

	project := filtered[m.projectCursor]
	m.projectID = string(project.ID)
	m.projectName = project.Name
	m.view = viewContainerList
	m.containerCursor = 0
	m.searchQuery = ""
	m.projectPaused = false

	return m, m.fetchContainersCmd()
}

// exitContainerView navigates back to project list.
func (m *Model) exitContainerView() (tea.Model, tea.Cmd) {
	m.view = viewProjectList
	m.containerID = ""
	m.searchQuery = ""
	m.containerPaused = false
	return m, m.fetchProjectsCmd()
}

// enterLogView navigates from container list to log view.
func (m *Model) enterLogView() (tea.Model, tea.Cmd) {
	filtered := m.filteredContainers()
	if m.containerCursor >= len(filtered) {
		return m, nil
	}

	container := filtered[m.containerCursor]
	m.containerID = string(container.ID)
	m.containerName = container.Name
	m.containerService = container.Service
	m.view = viewContainerLog
	m.logPaused = false
	m.logFilter = ""
	m.logs = nil
	m.showTimestamp = false
	m.disappeared = false

	return m, m.fetchContainerLogCmd()
}

// exitLogView navigates back to container list.
func (m *Model) exitLogView() (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.stopLogCollectionCmd(m.containerID)}
	m.view = viewContainerList
	m.containerID = ""
	m.logPaused = false
	m.logFilter = ""
	m.logs = nil
	m.disappeared = false
	cmds = append(cmds, m.fetchContainersCmd())
	return m, tea.Batch(cmds...)
}

// handleShell opens a shell in the selected container.
func (m *Model) handleShell() (tea.Model, tea.Cmd) {
	filtered := m.filteredContainers()
	container := getSelectedContainer(filtered, m.containerCursor)
	if container == nil || container.Status != model.StatusRunning {
		return m, nil
	}
	if !isValidContainerID(string(container.ID)) {
		m.statusMsg = "Invalid container ID"
		return m, clearStatusCmd(statusMessageDuration)
	}
	return m, shellCmd(string(container.ID))
}

// handleStop stops the selected container.
func (m *Model) handleStop() (tea.Model, tea.Cmd) {
	filtered := m.filteredContainers()
	container := getSelectedContainer(filtered, m.containerCursor)
	if container == nil || container.Status != model.StatusRunning {
		return m, nil
	}
	if !isValidContainerID(string(container.ID)) {
		m.statusMsg = "Invalid container ID"
		return m, clearStatusCmd(statusMessageDuration)
	}
	return m, tea.Batch(
		m.setPendingActionCmd(container.ID, actionStopping),
		containerActionCmd(string(container.ID), "stop", "Stop container timed out", "Failed to stop container: %v"),
	)
}

// handleRestart restarts the selected container.
func (m *Model) handleRestart() (tea.Model, tea.Cmd) {
	filtered := m.filteredContainers()
	container := getSelectedContainer(filtered, m.containerCursor)
	if container == nil {
		return m, nil
	}
	if !isValidContainerID(string(container.ID)) {
		m.statusMsg = "Invalid container ID"
		return m, clearStatusCmd(statusMessageDuration)
	}

	if container.Status == model.StatusRunning {
		return m, tea.Batch(
			m.setPendingActionCmd(container.ID, actionRestarting),
			containerActionCmd(string(container.ID), "restart", "Restart container timed out", "Failed to restart container: %v"),
		)
	}
	return m, tea.Batch(
		m.setPendingActionCmd(container.ID, actionStarting),
		containerActionCmd(string(container.ID), "start", "Start container timed out", "Failed to start container: %v"),
	)
}

// handleRemove removes the selected container.
func (m *Model) handleRemove() (tea.Model, tea.Cmd) {
	filtered := m.filteredContainers()
	container := getSelectedContainer(filtered, m.containerCursor)
	if container == nil || container.Status == model.StatusRunning {
		return m, nil
	}
	if !isValidContainerID(string(container.ID)) {
		m.statusMsg = "Invalid container ID"
		return m, clearStatusCmd(statusMessageDuration)
	}
	return m, tea.Batch(
		m.setPendingActionCmd(container.ID, actionRemoving),
		containerActionCmd(string(container.ID), "rm", "Remove container timed out", "Failed to remove container: %v"),
	)
}

// pauseProjectRefresh pauses project refresh temporarily.
func (m *Model) pauseProjectRefresh() {
	m.projectPaused = true
	m.pauseResumeTime = time.Now().Add(refreshPauseDuration)
}

// pauseContainerRefresh pauses container refresh temporarily.
func (m *Model) pauseContainerRefresh() {
	m.containerPaused = true
	m.pauseResumeTime = time.Now().Add(refreshPauseDuration)
}

// filteredProjects returns sorted and filtered projects.
func (m *Model) filteredProjects() []model.Project {
	projects := make([]model.Project, len(m.projects))
	copy(projects, m.projects)
	projects = filterProjects(projects, m.searchQuery)

	slices.SortStableFunc(projects, func(a, b model.Project) int {
		return compareProjects(a, b, m.projectSort.column, m.projectSort.asc)
	})
	return projects
}

// filteredContainers returns sorted and filtered containers for the current project.
func (m *Model) filteredContainers() []model.Container {
	var filtered []model.Container
	for _, c := range m.containers {
		if string(c.Project.ID) != m.projectID {
			continue
		}
		if !fuzzyMatch(c.Service, m.searchQuery) {
			continue
		}
		filtered = append(filtered, c)
	}

	slices.SortStableFunc(filtered, func(a, b model.Container) int {
		return compareContainers(a, b, m.containerSort.column, m.containerSort.asc)
	})
	return filtered
}

// updateLogViewport updates the viewport content with formatted logs.
func (m *Model) updateLogViewport() {
	var builder strings.Builder
	for _, line := range m.logs {
		if m.logFilter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(m.logFilter)) {
			continue
		}
		formatted := colorizeLogLine(line, m.showTimestamp)
		builder.WriteString(formatted)
		if !strings.HasSuffix(formatted, "\n") {
			builder.WriteString("\n")
		}
	}
	content := builder.String()
	m.logViewport.SetContent(content)

	// Highlight search matches in viewport
	if m.logFilter != "" {
		highlights := findHighlightRanges(content, m.logFilter)
		m.logViewport.SetHighlights(highlights)
	} else {
		m.logViewport.SetHighlights(nil)
	}

	if !m.logPaused {
		m.logViewport.GotoBottom()
	}
}

// findHighlightRanges finds all byte offset ranges of query matches in content.
func findHighlightRanges(content, query string) [][]int {
	if query == "" {
		return nil
	}
	escaped := regexp.QuoteMeta(query)
	re, err := regexp.Compile("(?i)" + escaped)
	if err != nil {
		return nil
	}
	matches := re.FindAllStringIndex(content, -1)
	result := make([][]int, len(matches))
	for i, m := range matches {
		result[i] = []int{m[0], m[1]}
	}
	return result
}

// View implements tea.Model — renders the entire UI.
func (m *Model) View() tea.View {
	if m.width == 0 {
		v := tea.NewView(m.spinner.View() + " Loading...")
		v.AltScreen = true
		return v
	}

	// Show loading progress on first load
	if m.loading {
		loadView := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			progressStyle.Render(
				m.spinner.View()+" Loading containers...\n\n"+
					m.progress.ViewAs(0.3),
			),
		)
		v := tea.NewView(loadView)
		v.AltScreen = true
		return v
	}

	var content string

	switch {
	case m.showHelp:
		content = m.viewHelp()
	case m.disappeared:
		content = m.viewDisappeared()
	default:
		switch m.view {
		case viewProjectList:
			content = m.viewProjectList()
		case viewContainerList:
			content = m.viewContainerList()
		case viewContainerLog:
			content = m.viewLogView()
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// viewProjectList renders the project list.
func (m *Model) viewProjectList() string {
	projects := m.filteredProjects()

	// Tabs + Header
	tabs := m.renderTabs()
	header := m.renderHeader()

	// Sort indicators
	sortCol := m.projectSort.column
	sortAsc := m.projectSort.asc

	nameInd := m.sortIndicator(sortCol == projectSortName, sortAsc)
	cpuInd := m.sortIndicator(sortCol == projectSortCPU, sortAsc)
	memInd := m.sortIndicator(sortCol == projectSortMemory, sortAsc)
	contInd := m.sortIndicator(sortCol == projectSortContainers, sortAsc)

	// Calculate column widths (with sparkline column)
	sparkW := sparklineMaxHistory + 2
	nameW := max(m.width-40-sparkW, 15)
	cpuW := 10
	memW := 10
	contW := 10

	// Table header
	tableHeader := lipgloss.JoinHorizontal(lipgloss.Top,
		tableHeaderStyle.Width(nameW).Render("NAME"+nameInd),
		tableHeaderStyle.Width(cpuW).Align(lipgloss.Right).Render("CPU"+cpuInd),
		tableHeaderStyle.Width(memW).Align(lipgloss.Right).Render("MEM"+memInd),
		tableHeaderStyle.Width(contW).Align(lipgloss.Right).Render("CONT"+contInd),
		tableHeaderStyle.Width(sparkW).Render("TREND"),
	)

	// Table rows
	var rows []string
	for i, project := range projects {
		style := tableCellStyle
		if i == m.projectCursor {
			style = tableSelectedStyle
		}

		name := project.Name
		if project.CPUPercentage > resourceWarningThreshold || project.MemoryPercentage > resourceWarningThreshold {
			name = warningStyle.Render("⚠") + " " + name
		}

		// Sparkline for this project
		var spark string
		if h, ok := m.sparklines[string(project.ID)]; ok {
			spark = renderSparkline(h.cpu, sparklineCPUStyle) + " " + renderSparkline(h.mem, sparklineMEMStyle)
		}

		row := lipgloss.JoinHorizontal(lipgloss.Top,
			style.Width(nameW).Render(name),
			style.Width(cpuW).Align(lipgloss.Right).Render(fmt.Sprintf("%.1f%%", max(0, project.CPUPercentage))),
			style.Width(memW).Align(lipgloss.Right).Render(fmt.Sprintf("%.1f%%", max(0, project.MemoryPercentage))),
			style.Width(contW).Align(lipgloss.Right).Render(fmt.Sprintf("%d/%d", project.ContainersRunning, len(project.ContainersState))),
			style.Width(sparkW).Render(spark),
		)
		rows = append(rows, row)
	}

	table := tableBorderStyle.Width(m.width - 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, append([]string{tableHeader}, rows...)...),
	)

	// Search input
	var searchLine string
	if m.searching {
		searchLine = "\n" + m.searchInput.View()
	}

	// Status bar with refresh timer
	statusLine := m.renderStatusBar()

	return tabs + "\n" + header + "\n" + table + searchLine + statusLine
}

// viewContainerList renders the container list.
func (m *Model) viewContainerList() string {
	containers := m.filteredContainers()

	tabs := m.renderTabs()
	header := m.renderHeader()

	sortCol := m.containerSort.column
	sortAsc := m.containerSort.asc

	nameInd := m.sortIndicator(sortCol == containerSortName, sortAsc)
	statusInd := m.sortIndicator(sortCol == containerSortStatus, sortAsc)
	cpuInd := m.sortIndicator(sortCol == containerSortCPU, sortAsc)
	memInd := m.sortIndicator(sortCol == containerSortMemory, sortAsc)

	sparkW := sparklineMaxHistory + 2
	nameW := max(m.width-44-sparkW, 15)
	statusW := 12
	cpuW := 10
	memW := 10

	tableHeader := lipgloss.JoinHorizontal(lipgloss.Top,
		tableHeaderStyle.Width(nameW).Render("NAME"+nameInd),
		tableHeaderStyle.Width(statusW).Render("STATUS"+statusInd),
		tableHeaderStyle.Width(cpuW).Align(lipgloss.Right).Render("CPU"+cpuInd),
		tableHeaderStyle.Width(memW).Align(lipgloss.Right).Render("MEM"+memInd),
		tableHeaderStyle.Width(sparkW).Render("TREND"),
	)

	var rows []string
	for i, container := range containers {
		style := tableCellStyle
		if i == m.containerCursor {
			style = tableSelectedStyle
		}

		// Container name with optional hyperlink
		name := container.Service
		if container.CPUPercentage > resourceWarningThreshold || container.MemoryPercentage > resourceWarningThreshold {
			name = warningStyle.Render("⚠") + " " + name
		}

		statusText := container.Status
		if statusText == "" {
			statusText = "unknown"
		}
		sStyle := statusStyle(statusText)
		displayStatus := statusText
		if container.PendingAction != "" {
			displayStatus = m.spinner.View() + " " + container.PendingAction
			sStyle = statusRestartingStyle
		}
		if len(displayStatus) > statusW-2 {
			if statusW > 5 {
				displayStatus = displayStatus[:statusW-3] + "…"
			} else {
				displayStatus = "…"
			}
		}

		// Sparkline for this container
		var spark string
		if h, ok := m.sparklines[string(container.ID)]; ok {
			spark = renderSparkline(h.cpu, sparklineCPUStyle) + " " + renderSparkline(h.mem, sparklineMEMStyle)
		}

		row := lipgloss.JoinHorizontal(lipgloss.Top,
			style.Width(nameW).Render(name),
			style.Width(statusW).Render(sStyle.Render(displayStatus)),
			style.Width(cpuW).Align(lipgloss.Right).Render(fmt.Sprintf("%.1f%%", container.CPUPercentage)),
			style.Width(memW).Align(lipgloss.Right).Render(fmt.Sprintf("%.1f%%", container.MemoryPercentage)),
			style.Width(sparkW).Render(spark),
		)
		rows = append(rows, row)
	}

	table := tableBorderStyle.Width(m.width - 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, append([]string{tableHeader}, rows...)...),
	)

	var searchLine string
	if m.searching {
		searchLine = "\n" + m.searchInput.View()
	}

	statusLine := m.renderStatusBar()

	return tabs + "\n" + header + "\n" + table + searchLine + statusLine
}

// viewLogView renders the log view with line numbers and search highlights.
func (m *Model) viewLogView() string {
	tabs := m.renderTabs()
	header := m.renderHeader()

	m.updateLogViewport()

	logContent := tableBorderStyle.Width(m.width - 2).Render(m.logViewport.View())

	var filterLine string
	if m.logFilterActive {
		filterLine = "\n" + m.logFilterInput.View()
	}

	return tabs + "\n" + header + "\n" + logContent + filterLine
}

// viewHelp renders the help modal using the bubbles/help component.
func (m *Model) viewHelp() string {
	var km help.KeyMap
	switch m.view {
	case viewProjectList:
		km = projectKeyMap{}
	case viewContainerList:
		km = containerKeyMap{}
	case viewContainerLog:
		km = logKeyMap{}
	}

	helpContent := helpBorderStyle.Render(
		helpTitleStyle.Render("⌨ Keyboard Shortcuts") + "\n\n" +
			m.helpModel.View(km) + "\n\n" +
			helpDescStyle.Render("Press ESC or ? to close"),
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, helpContent)
}

// viewDisappeared renders the disappeared container modal.
func (m *Model) viewDisappeared() string {
	displayID := m.containerID
	if len(displayID) > 12 {
		displayID = displayID[:12]
	}

	modal := helpBorderStyle.Render(
		m.spinner.View() + " Container " + m.containerName + " (" + displayID + ") no longer exists.\n\n" +
			"Waiting for it to reappear or press Enter/Esc to return.",
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

// renderTabs renders breadcrumb-style navigation tabs.
func (m *Model) renderTabs() string {
	sep := tabSeparatorStyle.Render(" › ")

	switch m.view {
	case viewProjectList:
		return activeTabStyle.Render("Projects")
	case viewContainerList:
		return inactiveTabStyle.Render("Projects") + sep + activeTabStyle.Render(m.projectName)
	case viewContainerLog:
		return inactiveTabStyle.Render("Projects") + sep +
			inactiveTabStyle.Render(m.projectName) + sep +
			activeTabStyle.Render(m.containerService)
	}
	return ""
}

// renderHeader renders the info bar below tabs.
func (m *Model) renderHeader() string {
	var text string
	switch m.view {
	case viewProjectList:
		count := len(m.filteredProjects())
		text = headerTitleStyle.Render("c8s") + " │ " +
			headerCountStyle.Render(fmt.Sprintf("%d", count)) + " projects"
		if m.searchQuery != "" {
			text += " " + headerFilterStyle.Render("(filter: "+m.searchQuery+")")
		}
		if m.projectPaused {
			text += " " + headerPausedStyle.Render("⏸ PAUSED")
		}

	case viewContainerList:
		count := len(m.filteredContainers())
		text = headerTitleStyle.Render("c8s") + " │ " +
			headerCountStyle.Render(fmt.Sprintf("%d", count)) + " containers"
		if m.searchQuery != "" {
			text += " " + headerFilterStyle.Render("(filter: "+m.searchQuery+")")
		}
		if m.containerPaused {
			text += " " + headerPausedStyle.Render("⏸ PAUSED")
		}

	case viewContainerLog:
		text = headerTitleStyle.Render("c8s") + " │ Logs " + m.containerService
		if m.logPaused {
			text += " " + headerPausedStyle.Render("⏸ PAUSED")
		}
		if m.logFilter != "" {
			text += " " + headerFilterStyle.Render("(filter: "+m.logFilter+")")
		}
		if m.showTimestamp {
			text += " " + headerFilterStyle.Render("(timestamps)")
		}
	}

	// Add refresh timer
	if !m.lastRefresh.IsZero() {
		elapsed := time.Since(m.lastRefresh).Truncate(time.Second)
		text += "  " + refreshTimerStyle.Render(fmt.Sprintf("⟳ %s ago", elapsed))
	}

	return headerStyle.Width(m.width).Render(text)
}

// renderStatusBar renders the bottom status bar with messages and spinner.
func (m *Model) renderStatusBar() string {
	if m.statusMsg != "" {
		return "\n" + statusBarStyle.Width(m.width).Render(m.statusMsg)
	}
	return ""
}

// sortIndicator returns a sort direction arrow.
func (m *Model) sortIndicator(active bool, asc bool) string {
	if !active {
		return ""
	}
	if asc {
		return sortIndicatorStyle.Render(" ↑")
	}
	return sortIndicatorStyle.Render(" ↓")
}
