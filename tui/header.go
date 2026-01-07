package tui

import (
	"fmt"

	"github.com/syrm/c8s/dto"
)

// getProjectName returns the name of the current project, or "unknown" if not found.
func (t *Tui) getProjectName() string {
	currentProjectID := t.getCurrentProjectID()
	if currentProjectID == "" {
		return "unknown"
	}

	t.tableProjectDataLock.RLock()
	defer t.tableProjectDataLock.RUnlock()

	if project, ok := t.tableProjectData[dto.ProjectID(currentProjectID)]; ok {
		return project.Name
	}
	return "unknown"
}

// updateHeader updates the header text based on the current view.
func (t *Tui) updateHeader() {
	t.currentViewLock.RLock()
	cv := t.currentView
	t.currentViewLock.RUnlock()

	var text string
	switch cv {
	case viewProjectList:
		text = t.buildProjectListHeader()
	case viewProject:
		text = t.buildContainerListHeader()
	case viewContainerLog:
		text = t.buildLogViewHeader()
	default:
		text = " [white::b]c8s[-::]"
	}

	t.headerView.SetText(text)
}

// buildProjectListHeader builds the header for the project list view.
func (t *Tui) buildProjectListHeader() string {
	t.tableProjectDataLock.RLock()
	count := len(t.tableProjectData)
	t.tableProjectDataLock.RUnlock()

	filter := ""
	projectSearchQuery := t.getProjectSearchQuery()
	if projectSearchQuery != "" {
		filter = fmt.Sprintf(" [white](filter: %s)[-]", projectSearchQuery)
	}

	t.projectRefreshPausedLock.RLock()
	paused := ""
	if t.projectRefreshPaused {
		paused = " [fuchsia]PAUSED[-]"
	}
	t.projectRefreshPausedLock.RUnlock()

	return fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Projects([fuchsia]%d[-])[-]%s%s", count, filter, paused)
}

// buildContainerListHeader builds the header for the container list view.
func (t *Tui) buildContainerListHeader() string {
	t.tableContainerDataLock.RLock()
	count := len(t.tableContainerData)
	t.tableContainerDataLock.RUnlock()

	projectName := t.getProjectName()

	filter := ""
	containerSearchQuery := t.getContainerSearchQuery()
	if containerSearchQuery != "" {
		filter = fmt.Sprintf(" [white](filter: %s)[-]", containerSearchQuery)
	}

	t.containerRefreshPausedLock.RLock()
	paused := ""
	if t.containerRefreshPaused {
		paused = " [fuchsia]PAUSED[-]"
	}
	t.containerRefreshPausedLock.RUnlock()

	return fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Containers([fuchsia]%d[-])[-] [white](%s)[-]%s%s",
		count, projectName, filter, paused)
}

// buildLogViewHeader builds the header for the log view.
func (t *Tui) buildLogViewHeader() string {
	status := ""

	t.logPausedLock.RLock()
	if t.logPaused {
		status += " [fuchsia]PAUSED[-]"
	}
	t.logPausedLock.RUnlock()

	t.logFilterLock.RLock()
	if t.logFilter != "" {
		status += fmt.Sprintf(" [white](filter: %s)[-]", t.logFilter)
	}
	t.logFilterLock.RUnlock()

	t.logShowTimestampLock.RLock()
	if t.logShowTimestamp {
		status += " [white](time)[-]"
	}
	t.logShowTimestampLock.RUnlock()

	projectName := t.getProjectName()
	containerService := t.getCurrentContainerService()

	return fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Logs[-] [white]%s[-] [white](%s)[-]%s",
		containerService, projectName, status)
}
