package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rivo/tview"

	"github.com/syrm/c8s/internal/model"
)

func (t *Tui) getSortIndicator(isActive bool, isAsc bool) string {
	if !isActive {
		return ""
	}
	if isAsc {
		return "[fuchsia]↑[-]"
	}
	return "[fuchsia]↓[-]"
}

func (t *Tui) RenderProjectHeader() {
	sortCol, sortAsc := t.projectView.Sort.Get()
	nameIndicator := t.getSortIndicator(sortCol == projectSortName, sortAsc)
	cpuIndicator := t.getSortIndicator(sortCol == projectSortCPU, sortAsc)
	memIndicator := t.getSortIndicator(sortCol == projectSortMemory, sortAsc)
	contIndicator := t.getSortIndicator(sortCol == projectSortContainers, sortAsc)

	t.projectView.Table.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf("[cyan::b]NAME%s[-::-]", nameIndicator)).SetAlign(tview.AlignLeft).SetExpansion(3).SetSelectable(false))
	t.projectView.Table.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("[cyan::b]CPU%s[-::-]", cpuIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.projectView.Table.SetCell(0, 2, tview.NewTableCell(fmt.Sprintf("[cyan::b]MEM%s[-::-]", memIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.projectView.Table.SetCell(0, 3, tview.NewTableCell(fmt.Sprintf("[cyan::b]CONT%s[-::-]", contIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetSelectable(false))
	t.projectView.Table.SetFixed(1, 0)
}

func (t *Tui) RenderContainerHeader() {
	sortCol, sortAsc := t.containerView.Sort.Get()
	nameIndicator := t.getSortIndicator(sortCol == containerSortName, sortAsc)
	cpuIndicator := t.getSortIndicator(sortCol == containerSortCPU, sortAsc)
	memIndicator := t.getSortIndicator(sortCol == containerSortMemory, sortAsc)
	statusIndicator := t.getSortIndicator(sortCol == containerSortStatus, sortAsc)

	w := int(t.tableWidth.Load())
	statusMaxWidth := t.calculateStatusWidth(w)

	t.containerView.Table.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf("[cyan::b]NAME%s[-::-]", nameIndicator)).SetAlign(tview.AlignLeft).SetExpansion(3).SetSelectable(false))
	t.containerView.Table.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("[cyan::b]STATUS%s[-::-]", statusIndicator)).SetAlign(tview.AlignLeft).SetExpansion(0).SetMaxWidth(statusMaxWidth).SetSelectable(false))
	t.containerView.Table.SetCell(0, 2, tview.NewTableCell(fmt.Sprintf("[cyan::b]CPU%s[-::-]", cpuIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.containerView.Table.SetCell(0, 3, tview.NewTableCell(fmt.Sprintf("[cyan::b]MEM%s[-::-]", memIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.containerView.Table.SetFixed(1, 0)
}

func (t *Tui) calculateStatusWidth(tableWidth int) int {
	minOtherWidth := 44
	availableForStatus := tableWidth - minOtherWidth

	if availableForStatus < 4 {
		return 4
	}
	if availableForStatus < 12 {
		return availableForStatus
	}
	return 12
}

func (t *Tui) drawProjects() {
	projects := t.projectView.Values()
	projects = filterProjects(projects, t.projectView.Search.Query())

	sortCol, sortAsc := t.projectView.Sort.Get()
	slices.SortStableFunc(projects, func(a, b model.Project) int {
		return compareProjects(a, b, sortCol, sortAsc)
	})

	t.projectView.Table.Clear()
	t.RenderProjectHeader()

	for index, project := range projects {
		rowIndex := index + 1

		projectName := project.Name
		if project.CPUPercentage > resourceWarningThreshold || project.MemoryPercentage > resourceWarningThreshold {
			projectName = "[yellow]⚠[-] " + projectName
		}
		t.projectView.Table.SetCell(rowIndex, 0, tview.NewTableCell(projectName))

		t.projectView.Table.SetCell(rowIndex, 1,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", max(0, project.CPUPercentage))).SetAlign(tview.AlignRight))

		t.projectView.Table.SetCell(rowIndex, 2,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", max(0, project.MemoryPercentage))).SetAlign(tview.AlignRight))

		t.projectView.Table.SetCell(rowIndex, 3,
			tview.NewTableCell(fmt.Sprintf("%d/%d", project.ContainersRunning, len(project.ContainersState))).SetAlign(tview.AlignRight))
	}

	t.updateHeader()
}

func (t *Tui) drawContainers() {
	containers := t.containerView.Values()
	currentProjectID := t.nav.ProjectID()

	sortCol, sortAsc := t.containerView.Sort.Get()
	slices.SortStableFunc(containers, func(a, b model.Container) int {
		return compareContainers(a, b, sortCol, sortAsc)
	})

	t.containerView.Table.Clear()
	t.RenderContainerHeader()

	index := 0
	for _, container := range containers {
		if string(container.Project.ID) != currentProjectID {
			continue
		}

		if !fuzzyMatch(container.Service, t.containerView.Search.Query()) {
			continue
		}

		index++

		serviceName := container.Service
		if container.CPUPercentage > resourceWarningThreshold || container.MemoryPercentage > resourceWarningThreshold {
			serviceName = "[yellow]⚠[-] " + serviceName
		}
		t.containerView.Table.SetCell(index, 0, tview.NewTableCell(serviceName))

		statusText := container.Status
		if statusText == "" {
			statusText = "unknown"
		}

		displayText := statusText
		var statusColor string
		switch strings.ToLower(statusText) {
		case model.StatusRunning:
			statusColor = "green"
		case model.StatusExited, "dead", model.StatusRemoving:
			statusColor = "red"
		case model.StatusPaused:
			statusColor = "yellow"
		case model.StatusRestarting:
			statusColor = "fuchsia"
		case model.StatusCreated:
			statusColor = "cyan"
		default:
			statusColor = "gray"
		}

		if container.PendingAction != "" {
			displayText = container.PendingAction + "…"
			statusColor = "fuchsia"
		}

		maxWidth := t.calculateStatusWidth(int(t.tableWidth.Load()))
		if len(displayText) > maxWidth {
			if maxWidth > 3 {
				displayText = displayText[:maxWidth-1] + "…"
			} else {
				displayText = "…"
			}
		}

		t.containerView.Table.SetCell(index, 1,
			tview.NewTableCell(fmt.Sprintf("[%s]%s[-]", statusColor, displayText)).SetAlign(tview.AlignLeft))

		t.containerView.Table.SetCell(index, 2,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", container.CPUPercentage)).SetAlign(tview.AlignRight))

		t.containerView.Table.SetCell(index, 3,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", container.MemoryPercentage)).SetAlign(tview.AlignRight))
	}

	t.updateHeader()
}

func (t *Tui) drawContainerLog() {
	t.logView.View.Clear()

	paused := t.logView.Paused.Load()
	filter := t.logView.GetFilter()
	showTimestamp := t.logView.ShowTimestamp.Load()
	logData := t.logView.GetData()

	var logs []string
	for _, line := range logData {
		if filter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(filter)) {
			continue
		}
		logs = append(logs, colorizeLogLine(line, showTimestamp))
	}

	t.logView.View.SetText(strings.Join(logs, ""))

	if !paused {
		t.logView.View.ScrollToEnd()
	}

	t.updateHeader()
}

func (t *Tui) showStatusMessage(message string) {
	t.status.Show(message, statusMessageDuration, t.app, t.closing.Load)
}

func (t *Tui) setProjectSort(col projectSortColumn) {
	t.projectView.Sort.Toggle(col, col == projectSortName)
	t.drawProjects()
}

func (t *Tui) setContainerSort(col containerSortColumn) {
	t.containerView.Sort.Toggle(col, col == containerSortName || col == containerSortStatus)
	t.drawContainers()
}
