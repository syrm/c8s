package tui

import (
	"fmt"

	"github.com/syrm/c8s/internal/model"
)

// getProjectName returns the name of the current project, or "unknown" if not found.
func (t *Tui) getProjectName() string {
	currentProjectID := t.nav.ProjectID()
	if currentProjectID == "" {
		return "unknown"
	}

	if project, ok := t.projectView.Get(model.ProjectID(currentProjectID)); ok {
		return project.Name
	}
	return "unknown"
}

// updateHeader updates the header text based on the current view.
func (t *Tui) updateHeader() {
	cv := t.nav.View()

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

	t.header.SetText(text)
}

// buildProjectListHeader builds the header for the project list view.
func (t *Tui) buildProjectListHeader() string {
	count := t.projectView.Len()

	filter := ""
	if q := t.projectView.Search.Query(); q != "" {
		filter = fmt.Sprintf(" [white](filter: %s)[-]", q)
	}

	paused := ""
	if t.projectRefresh.IsPaused() {
		paused = " [fuchsia]PAUSED[-]"
	}

	return fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Projects([fuchsia]%d[-])[-]%s%s", count, filter, paused)
}

// buildContainerListHeader builds the header for the container list view.
func (t *Tui) buildContainerListHeader() string {
	count := t.containerView.Len()
	projectName := t.getProjectName()

	filter := ""
	if q := t.containerView.Search.Query(); q != "" {
		filter = fmt.Sprintf(" [white](filter: %s)[-]", q)
	}

	paused := ""
	if t.containerRefresh.IsPaused() {
		paused = " [fuchsia]PAUSED[-]"
	}

	return fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Containers([fuchsia]%d[-])[-] [white](%s)[-]%s%s",
		count, projectName, filter, paused)
}

// buildLogViewHeader builds the header for the log view.
func (t *Tui) buildLogViewHeader() string {
	status := ""

	if t.logView.Paused.Load() {
		status += " [fuchsia]PAUSED[-]"
	}

	if f := t.logView.GetFilter(); f != "" {
		status += fmt.Sprintf(" [white](filter: %s)[-]", f)
	}

	if t.logView.ShowTimestamp.Load() {
		status += " [white](time)[-]"
	}

	projectName := t.getProjectName()
	containerService := t.nav.ContainerService()

	return fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Logs[-] [white]%s[-] [white](%s)[-]%s",
		containerService, projectName, status)
}