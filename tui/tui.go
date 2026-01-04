package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/syrm/c8s/dto"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type currentView int

const (
	viewProjectList currentView = iota
	viewProject
	viewContainerLog
)

type RequestData interface {
	isRequestData()
}

type RequestProjectList struct {
	Response chan []dto.Project
}

func (p *RequestProjectList) isRequestData() {}

type RequestContainerLog struct {
	ContainerID dto.ContainerID
	Response    chan dto.Container
}

func (p *RequestContainerLog) isRequestData() {}

type RequestProject struct {
	ProjectID dto.ProjectID
	Response  chan []dto.Container
}

func (p *RequestProject) isRequestData() {}

type Tui struct {
	app                    *tview.Application
	tableProject           *tview.Table
	tableProjectData       map[dto.ProjectID]dto.Project
	tableProjectDataLock   sync.RWMutex
	tableContainer         *tview.Table
	tableContainerData     map[dto.ContainerID]dto.Container
	tableContainerDataLock sync.RWMutex
	containerLayout        *tview.Flex
	statusBar              *tview.TextView
	tableContainerLog      *tview.TextView
	tableContainerLogData  []string
	logPaused              bool
	logPausedLock          sync.RWMutex
	logShowTimestamp       bool
	logShowTimestampLock   sync.RWMutex
	logFilter              string
	logFilterLock          sync.RWMutex
	statusTimer            *time.Timer
	logFilterInput         *tview.InputField
	logLayout              *tview.Flex
	currentView            currentView
	currentViewLock        sync.RWMutex
	currentProjectID       string
	currentContainerID     string
	currentContainerName   string
	requestData            chan RequestData
	logger                 *slog.Logger
}

func NewTui(logger *slog.Logger) *Tui {
	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	app := tview.NewApplication()

	tableProject := tview.NewTable().SetSelectable(true, false)
	tableProject.SetBorder(true)

	tableContainer := tview.NewTable().SetSelectable(true, false)
	tableContainer.SetBorder(true)

	statusBar := tview.NewTextView()
	statusBar.SetDynamicColors(true)
	statusBar.SetTextAlign(tview.AlignCenter)

	containerLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tableContainer, 0, 1, true)

	tableContainerLog := tview.NewTextView()
	tableContainerLog.SetScrollable(true)
	tableContainerLog.SetWordWrap(true)
	tableContainerLog.SetBorder(true)
	tableContainerLog.SetDynamicColors(true)

	logFilterInput := tview.NewInputField()
	logFilterInput.SetLabel("Filter: ")
	logFilterInput.SetFieldWidth(0)

	logLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tableContainerLog, 0, 1, true)

	tui := &Tui{
		app:                app,
		logger:             logger,
		tableProject:       tableProject,
		tableProjectData:   make(map[dto.ProjectID]dto.Project),
		tableContainer:     tableContainer,
		tableContainerData: make(map[dto.ContainerID]dto.Container),
		containerLayout:    containerLayout,
		statusBar:          statusBar,
		tableContainerLog:  tableContainerLog,
		logFilterInput:     logFilterInput,
		logLayout:          logLayout,
		logShowTimestamp:   false,
		requestData:        make(chan RequestData),
		currentView:        viewProjectList,
	}

	tableProject.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			rowIndex, _ := tableProject.GetSelection()
			tui.tableProjectDataLock.RLock()
			for _, project := range tui.tableProjectData {
				if project.Name == tableProject.GetCell(rowIndex, 0).Text {
					tui.currentProjectID = string(project.ID)
					tui.currentViewLock.Lock()
					tui.currentView = viewProject
					tui.currentViewLock.Unlock()
					break
				}
			}
			tui.tableProjectDataLock.RUnlock()
			tui.tableContainer.Clear()
			tui.drawContainers()
			tui.app.SetRoot(tui.containerLayout, true)
		}

		return event
	})

	tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			tui.app.SetRoot(tui.tableProject, true)
			tui.currentViewLock.Lock()
			tui.currentView = viewProjectList
			tui.currentViewLock.Unlock()
			tui.currentContainerID = ""
		}

		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			rowIndex, _ := tableContainer.GetSelection()
			tui.tableContainerDataLock.RLock()
			for _, container := range tui.tableContainerData {
				if container.Service == tableContainer.GetCell(rowIndex, 0).Text {
					tui.currentContainerID = string(container.ID)
					tui.currentContainerName = container.Name
					break
				}
			}
			tui.tableContainerDataLock.RUnlock()
			tui.drawContainerLog()
			tui.app.SetRoot(tui.logLayout, true)
			tui.currentViewLock.Lock()
			tui.currentView = viewContainerLog
			tui.currentViewLock.Unlock()
		}

		return event
	})

	tableContainerLog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			tui.tableContainer.Clear()
			tui.drawContainers()
			tui.app.SetRoot(tui.containerLayout, true)
			tui.currentViewLock.Lock()
			tui.currentView = viewProject
			tui.currentViewLock.Unlock()
			tui.currentContainerID = ""
			tui.logPausedLock.Lock()
			tui.logPaused = false
			tui.logPausedLock.Unlock()
			tui.logFilterLock.Lock()
			tui.logFilter = ""
			tui.logFilterLock.Unlock()
			tui.logFilterInput.SetText("")
			tui.logLayout.RemoveItem(tui.logFilterInput)
			tui.tableContainerLogData = nil // Clear logs to free memory
		}

		if event.Rune() == 'p' {
			tui.logPausedLock.Lock()
			tui.logPaused = !tui.logPaused
			tui.logPausedLock.Unlock()
			tui.drawContainerLog()
		}

		if event.Rune() == 'f' {
			tui.logLayout.AddItem(tui.logFilterInput, 1, 0, true)
			tui.app.SetFocus(tui.logFilterInput)
		}

		if event.Rune() == 't' {
			tui.logShowTimestampLock.Lock()
			tui.logShowTimestamp = !tui.logShowTimestamp
			tui.logShowTimestampLock.Unlock()
			tui.drawContainerLog()
		}

		return event
	})

	logFilterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			tui.logFilterLock.Lock()
			tui.logFilter = logFilterInput.GetText()
			tui.logFilterLock.Unlock()
			tui.logLayout.RemoveItem(tui.logFilterInput)
			tui.app.SetFocus(tui.tableContainerLog)
			tui.drawContainerLog()
		}
		if key == tcell.KeyEsc {
			tui.logFilterLock.Lock()
			tui.logFilter = ""
			tui.logFilterLock.Unlock()
			logFilterInput.SetText("")
			tui.logLayout.RemoveItem(tui.logFilterInput)
			tui.app.SetFocus(tui.tableContainerLog)
			tui.drawContainerLog()
		}
	})

	return tui
}

func (t *Tui) RenderProjectHeader() {
	t.tableProject.SetCell(0, 0, tview.NewTableCell("[::b]Project").SetAlign(tview.AlignCenter).SetExpansion(3).SetSelectable(false))
	t.tableProject.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 3, tview.NewTableCell("[::b]Cont.").SetAlign(tview.AlignRight).SetExpansion(2).SetSelectable(false))
	t.tableProject.SetFixed(1, 0)
}

func (t *Tui) RenderContainerHeader(project string) {
	t.tableContainer.SetCell(0, 0, tview.NewTableCell("[::b]"+project+" container").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false))
	t.tableContainer.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetFixed(1, 0)
}

func (t *Tui) drawProjects() {
	projects := slices.Collect(maps.Values(t.tableProjectData))

	slices.SortStableFunc(projects, func(a, b dto.Project) int {
		if a.CPUPercentage < b.CPUPercentage {
			return 1
		}

		if a.CPUPercentage > b.CPUPercentage {
			return -1
		}

		return strings.Compare(a.Name, b.Name)
	})

	t.tableProject.Clear()
	t.RenderProjectHeader()
	offset := 0
	for index, project := range projects {
		t.tableProject.SetCell(index+1+offset, 0, tview.NewTableCell(project.Name))
		t.tableProject.SetCell(
			index+1+offset,
			1,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", max(0, project.CPUPercentage)),
			).
				SetAlign(tview.AlignRight),
		)
		t.tableProject.SetCell(
			index+1+offset,
			2,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", max(0, project.MemoryPercentage)),
			).
				SetAlign(tview.AlignRight),
		)
		t.tableProject.SetCell(
			index+1+offset,
			3,
			tview.NewTableCell(
				fmt.Sprintf("%d/%d", project.ContainersRunning, len(project.ContainersState)),
			).
				SetAlign(tview.AlignRight),
		)
	}
}

func (t *Tui) drawContainers() {
	t.tableContainerDataLock.RLock()
	containers := slices.SortedStableFunc(maps.Values(t.tableContainerData), func(a, b dto.Container) int {
		if a.CPUPercentage < b.CPUPercentage {
			return 1
		}

		if a.CPUPercentage > b.CPUPercentage {
			return -1
		}

		return strings.Compare(a.Name, b.Name)
	})
	t.tableContainerDataLock.RUnlock()

	t.tableContainer.Clear()
	t.tableProjectDataLock.RLock()
	t.RenderContainerHeader(t.tableProjectData[dto.ProjectID(t.currentProjectID)].Name)
	t.tableProjectDataLock.RUnlock()
	index := 0
	for _, container := range containers {
		if string(container.Project.ID) != t.currentProjectID {
			continue
		}
		index += 1

		t.tableContainer.SetCell(index, 0, tview.NewTableCell(container.Service))
		t.tableContainer.SetCell(
			index,
			1,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", container.CPUPercentage),
			).
				SetAlign(tview.AlignRight),
		)
		t.tableContainer.SetCell(
			index,
			2,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", container.MemoryPercentage),
			).
				SetAlign(tview.AlignRight),
		)
	}
}

func (t *Tui) drawContainerLog() {
	t.tableContainerLog.Clear()

	t.tableContainerDataLock.RLock()
	container, ok := t.tableContainerData[dto.ContainerID(t.currentContainerID)]
	t.tableContainerDataLock.RUnlock()

	t.logPausedLock.RLock()
	paused := t.logPaused
	t.logPausedLock.RUnlock()

	t.logFilterLock.RLock()
	filter := t.logFilter
	t.logFilterLock.RUnlock()

	t.logShowTimestampLock.RLock()
	showTimestamp := t.logShowTimestamp
	t.logShowTimestampLock.RUnlock()

	if ok {
		statusIndicators := ""
		if paused {
			statusIndicators += " [yellow](PAUSED)[-]"
		}
		if filter != "" {
			statusIndicators += fmt.Sprintf(" [green](filter: %s)[-]", filter)
		}
		if showTimestamp {
			statusIndicators += " [gray](time)[-]"
		}
		t.tableContainerLog.SetTitle(fmt.Sprintf(" [::b]%s logs (%s)%s ", container.Service, container.Name, statusIndicators))
	}

	// Apply filter and colorize
	var logs []string
	for _, line := range t.tableContainerLogData {
		if filter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(filter)) {
			continue
		}
		logs = append(logs, colorizeLogLine(line, showTimestamp))
	}

	t.tableContainerLog.SetText(strings.Join(logs, ""))

	// Auto-scroll to bottom
	if !paused {
		t.tableContainerLog.ScrollToEnd()
	}
}

func formatJSONLog(line string, showTimestamp bool) (string, bool) {
	// Find the JSON part (after Docker timestamp if present)
	jsonStart := strings.Index(line, "{")
	if jsonStart == -1 {
		return "", false
	}

	dockerTimestamp := strings.TrimSpace(line[:jsonStart])
	jsonPart := line[jsonStart:]

	var logData map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &logData); err != nil {
		return "", false
	}

	// Extract common fields
	var logTimestamp, level, msg string
	var extras []string

	// Timestamp fields (from the log itself)
	for _, key := range []string{"time", "timestamp", "ts", "@timestamp", "t"} {
		if v, ok := logData[key]; ok {
			logTimestamp = fmt.Sprintf("%v", v)
			delete(logData, key)
			break
		}
	}

	// Level fields
	for _, key := range []string{"level", "lvl", "severity", "loglevel"} {
		if v, ok := logData[key]; ok {
			level = strings.ToUpper(fmt.Sprintf("%v", v))
			delete(logData, key)
			break
		}
	}

	// Message fields
	for _, key := range []string{"msg", "message", "text"} {
		if v, ok := logData[key]; ok {
			msg = fmt.Sprintf("%v", v)
			delete(logData, key)
			break
		}
	}

	// Collect remaining fields sorted by key
	var keys []string
	for k := range logData {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := logData[k]
		vStr := fmt.Sprintf("%v", v)
		// Escape brackets for tview
		vStr = strings.ReplaceAll(vStr, "[", "[[]")
		extras = append(extras, fmt.Sprintf("[blue]%s[-]=%s", k, vStr))
	}

	// Build formatted line
	var parts []string

	// Show exactly one timestamp if enabled
	// Priority: log's own timestamp (formatted like Docker) > Docker's timestamp
	if showTimestamp {
		if logTimestamp != "" {
			// Try to parse and format like Docker (RFC3339Nano)
			formattedTs := formatTimestamp(logTimestamp)
			parts = append(parts, fmt.Sprintf("[gray]%s[-]", formattedTs))
		} else if dockerTimestamp != "" {
			parts = append(parts, fmt.Sprintf("[gray]%s[-]", dockerTimestamp))
		}
	}

	if level != "" {
		levelColor := getLevelColor(level)
		parts = append(parts, fmt.Sprintf("[%s]%-5s[-]", levelColor, level))
	}
	if msg != "" {
		msg = strings.ReplaceAll(msg, "[", "[[]")
		parts = append(parts, msg)
	}
	if len(extras) > 0 {
		parts = append(parts, strings.Join(extras, " "))
	}

	return strings.Join(parts, " ") + "\n", true
}

func formatTimestamp(ts string) string {
	// Try common timestamp formats and convert to Docker format (RFC3339Nano)
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t.Format(time.RFC3339Nano)
		}
	}

	// If parsing fails, return as-is
	return ts
}

func getLevelColor(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL":
		return "red"
	case "WARN", "WARNING":
		return "yellow"
	case "INFO":
		return "green"
	case "DEBUG", "TRACE":
		return "gray"
	default:
		return "white"
	}
}

// extractTimestampPrefix tries to extract a timestamp from the beginning of a string
// Returns the timestamp, the remaining string, and whether a timestamp was found
func extractTimestampPrefix(s string) (timestamp string, rest string, found bool) {
	// Common timestamp patterns at the start of log lines
	// Docker format: "2024-01-15T10:30:00.123456789Z " or "2024-01-15T10:30:00+01:00 "
	// Log formats: "2024-01-15T10:30:00Z ", "2024-01-15 10:30:00 "

	if len(s) < 20 {
		return "", s, false
	}

	// Try to find space after timestamp
	spaceIdx := -1
	for i := 19; i < len(s) && i < 40; i++ {
		if s[i] == ' ' {
			spaceIdx = i
			break
		}
	}

	if spaceIdx == -1 {
		return "", s, false
	}

	potentialTs := s[:spaceIdx]

	// Try to parse it as a timestamp
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if _, err := time.Parse(format, potentialTs); err == nil {
			return potentialTs, s[spaceIdx+1:], true
		}
	}

	return "", s, false
}

func colorizeLogLine(line string, showTimestamp bool) string {
	// Try to parse as JSON first
	if formatted, ok := formatJSONLog(line, showTimestamp); ok {
		return formatted
	}

	// For non-JSON logs, handle timestamps properly
	// Docker adds timestamp at the beginning, and the log itself might have its own timestamp

	// First, extract Docker's timestamp
	dockerTs, afterDockerTs, hasDockerTs := extractTimestampPrefix(line)

	// Then check if the remaining content has its own timestamp
	logTs, content, hasLogTs := extractTimestampPrefix(afterDockerTs)

	var displayLine string
	if showTimestamp {
		// Priority: log's own timestamp (formatted) > Docker's timestamp
		if hasLogTs {
			formattedTs := formatTimestamp(logTs)
			displayLine = formattedTs + " " + content
		} else if hasDockerTs {
			displayLine = dockerTs + " " + afterDockerTs
		} else {
			displayLine = line
		}
	} else {
		// Don't show any timestamp
		if hasLogTs {
			displayLine = content
		} else if hasDockerTs {
			displayLine = afterDockerTs
		} else {
			displayLine = line
		}
	}

	lower := strings.ToLower(displayLine)

	// Escape tview color tags in the original line
	displayLine = strings.ReplaceAll(displayLine, "[", "[[]")

	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic"):
		return "[red]" + displayLine + "[-]"
	case strings.Contains(lower, "warn"):
		return "[yellow]" + displayLine + "[-]"
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		return "[gray]" + displayLine + "[-]"
	case strings.Contains(lower, "info"):
		return "[green]" + displayLine + "[-]"
	default:
		return displayLine
	}
}

func (t *Tui) showStatusMessage(message string) {
	t.statusBar.SetText("[red]" + message + "[-]")
	t.containerLayout.AddItem(t.statusBar, 1, 0, false)

	// Cancel previous timer if exists
	if t.statusTimer != nil {
		t.statusTimer.Stop()
	}

	// Clear message after 5 seconds
	t.statusTimer = time.AfterFunc(5*time.Second, func() {
		t.app.QueueUpdateDraw(func() {
			t.containerLayout.RemoveItem(t.statusBar)
			t.statusBar.SetText("")
		})
	})
}

func (t *Tui) GetRequestData() <-chan RequestData {
	return t.requestData
}

func (t *Tui) getData(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var ctxCancel context.CancelFunc

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			t.currentViewLock.RLock()
			cv := t.currentView
			t.currentViewLock.RUnlock()
			switch cv {
			case viewProjectList:
				if ctxCancel != nil {
					ctxCancel()
					ctxCancel = nil
				}

				response := make(chan []dto.Project)
				t.requestData <- &RequestProjectList{
					Response: response,
				}

				projects := <-response
				t.tableProjectDataLock.Lock()
				t.tableProjectData = make(map[dto.ProjectID]dto.Project)
				for _, p := range projects {
					t.tableProjectData[p.ID] = p
				}
				t.tableProjectDataLock.Unlock()

				t.app.QueueUpdateDraw(func() {
					t.drawProjects()
				})
			case viewProject:
				if ctxCancel != nil {
					ctxCancel()
					ctxCancel = nil
				}

				response := make(chan []dto.Container)
				t.requestData <- &RequestProject{
					ProjectID: dto.ProjectID(t.currentProjectID),
					Response:  response,
				}

				containers := <-response
				t.tableContainerDataLock.Lock()
				t.tableContainerData = make(map[dto.ContainerID]dto.Container)
				for _, c := range containers {
					t.tableContainerData[c.ID] = c
				}
				t.tableContainerDataLock.Unlock()

				t.app.QueueUpdateDraw(func() {
					t.drawContainers()
				})

			case viewContainerLog:
				t.logPausedLock.RLock()
				paused := t.logPaused
				t.logPausedLock.RUnlock()

				if paused {
					continue
				}

				t.logger.DebugContext(ctx, "fetching logs for container", slog.String("container_id", t.currentContainerID))

				// First time we enter this view, start the log collection
				if ctxCancel == nil {
					response := make(chan dto.Container)
					t.requestData <- &RequestContainerLog{
						ContainerID: dto.ContainerID(t.currentContainerID),
						Response:    response,
					}
					c := <-response
					if c.ID == "" {
						// Container no longer exists, go back to container list
						containerName := t.currentContainerName
						containerID := t.currentContainerID
						t.currentViewLock.Lock()
						t.currentView = viewProject
						t.currentViewLock.Unlock()
						t.currentContainerID = ""
						t.currentContainerName = ""
						t.logFilterLock.Lock()
						t.logFilter = ""
						t.logFilterLock.Unlock()
						t.logFilterInput.SetText("")
						t.logLayout.RemoveItem(t.logFilterInput)
						t.tableContainerLogData = nil // Clear logs to free memory
						t.app.QueueUpdateDraw(func() {
							t.tableContainer.Clear()
							t.drawContainers()
							t.app.SetRoot(t.containerLayout, true)
							t.showStatusMessage(fmt.Sprintf("Container %s (%s) no longer exists", containerName, containerID[:12]))
						})
						continue
					}
					ctxCancel = c.LogCancel
				}

				// Always get the latest logs
				response := make(chan dto.Container)
				t.requestData <- &RequestContainerLog{
					ContainerID: dto.ContainerID(t.currentContainerID),
					Response:    response,
				}

				c := <-response

				if c.ID == "" {
					// Container no longer exists, go back to container list
					if ctxCancel != nil {
						ctxCancel()
						ctxCancel = nil
					}
					containerName := t.currentContainerName
					containerID := t.currentContainerID
					t.currentViewLock.Lock()
					t.currentView = viewProject
					t.currentViewLock.Unlock()
					t.currentContainerID = ""
					t.currentContainerName = ""
					t.logFilterLock.Lock()
					t.logFilter = ""
					t.logFilterLock.Unlock()
					t.logFilterInput.SetText("")
					t.logLayout.RemoveItem(t.logFilterInput)
					t.tableContainerLogData = nil // Clear logs to free memory
					t.app.QueueUpdateDraw(func() {
						t.tableContainer.Clear()
						t.drawContainers()
						t.app.SetRoot(t.containerLayout, true)
						t.showStatusMessage(fmt.Sprintf("Container %s (%s) no longer exists", containerName, containerID[:12]))
					})
					continue
				}

				t.tableContainerLogData = c.Logs

				t.app.QueueUpdateDraw(func() {
					t.drawContainerLog()
				})
			}
		}
	}
}

func (t *Tui) Render(ctx context.Context) {
	go t.getData(ctx)

	if err := t.app.SetRoot(t.tableProject, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		os.Exit(1)
	}
}
