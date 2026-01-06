package tui

import "fmt"

// getProjectName returns the name of the current project, or "unknown" if not found.
func (t *Tui) getProjectName() string {
	if t.currentProjectID == "" {
		return "unknown"
	}

	t.tableProjectDataLock.RLock()
	defer t.tableProjectDataLock.RUnlock()

	for _, project := range t.tableProjectData {
		if string(project.ID) == t.currentProjectID {
			return project.Name
		}
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
	}

	t.headerView.SetText(text)
}

// buildProjectListHeader builds the header for the project list view.
func (t *Tui) buildProjectListHeader() string {
	t.tableProjectDataLock.RLock()
	count := len(t.tableProjectData)
	t.tableProjectDataLock.RUnlock()

	filter := ""
	if t.projectSearchQuery != "" {
		filter = fmt.Sprintf(" [white](filter: %s)[-]", t.projectSearchQuery)
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
	if t.containerSearchQuery != "" {
		filter = fmt.Sprintf(" [white](filter: %s)[-]", t.containerSearchQuery)
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

	return fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Logs[-] [white]%s[-] [white](%s)[-]%s",
		t.currentContainerService, projectName, status)
}
