package tui

import (
	"github.com/syrm/c8s/internal/model"
)

// projectsMsg is sent when the project list is refreshed.
type projectsMsg []model.Project

// containersMsg is sent when the container list is refreshed.
type containersMsg []model.Container

// logLinesMsg carries a batch of new log lines from the Docker stream.
type logLinesMsg []string

// logStreamDoneMsg signals the log stream has ended.
type logStreamDoneMsg struct{}

// logStartedMsg carries the container metadata after log streaming starts.
type logStartedMsg struct {
	container model.Container
	found     bool
}

// tickMsg triggers periodic data refresh.
type tickMsg struct{}

// statusMsg shows a temporary status message.
type statusMsg string

// clearStatusMsg clears the status message.
type clearStatusMsg struct{}

// shellFinishedMsg is sent after a shell session ends.
type shellFinishedMsg struct {
	err error
}

// actionResultMsg is sent when a container action completes.
type actionResultMsg struct {
	err     error
	message string
}

// containerDisappearedMsg signals a container no longer exists.
type containerDisappearedMsg struct {
	name string
	id   string
}
