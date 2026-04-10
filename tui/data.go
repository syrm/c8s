package tui

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/syrm/c8s/internal/model"
	itimer "github.com/syrm/c8s/internal/timer"
)

const channelTimeout = model.ChannelTimeout

// awaitResponse waits for a value on the channel with a timeout.
func awaitResponse[T any](ch <-chan T) (T, bool) {
	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case result := <-ch:
		return result, true
	case <-timer.C:
		var zero T
		return zero, false
	}
}

// sendRequest safely sends a request on the requestData channel.
func (m *Model) sendRequest(req model.RequestData) bool {
	if m.closing {
		return false
	}

	defer func() {
		if r := recover(); r != nil {
			errMsg := fmt.Sprintf("%v", r)
			if strings.Contains(errMsg, "send on closed channel") {
				m.logger.Debug("sendRequest recovered from closed channel", slog.Any("panic", r))
				return
			}
			m.logger.Error("sendRequest recovered from unexpected panic",
				slog.Any("panic", r),
				slog.String("request_type", fmt.Sprintf("%T", req)))
		}
	}()

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case m.requestData <- req:
		return true
	case <-timer.C:
		return false
	}
}

// tickCmd returns a command that sends a tick after the refresh interval.
func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

// fetchProjectsCmd fetches the project list from Docker.
func (m *Model) fetchProjectsCmd() tea.Cmd {
	return func() tea.Msg {
		response := make(chan []model.Project, 1)
		if !m.sendRequest(&model.RequestProjectList{Response: response}) {
			return nil
		}

		projects, ok := awaitResponse(response)
		if !ok {
			return nil
		}
		return projectsMsg(projects)
	}
}

// fetchContainersCmd fetches containers for the current project.
func (m *Model) fetchContainersCmd() tea.Cmd {
	projectID := m.projectID
	return func() tea.Msg {
		response := make(chan []model.Container, 1)
		if !m.sendRequest(&model.RequestProject{ProjectID: model.ProjectID(projectID), Response: response}) {
			return nil
		}

		containers, ok := awaitResponse(response)
		if !ok {
			return nil
		}
		return containersMsg(containers)
	}
}

// startLogStreamCmd sends a log request to Docker and returns logStartedMsg.
// The logChan is stored on the Model for waitForLogLines to read from.
func (m *Model) startLogStreamCmd() tea.Cmd {
	containerID := m.containerID
	logChan := m.logChan
	return func() tea.Msg {
		response := make(chan model.Container, 1)
		if !m.sendRequest(&model.RequestContainerLog{
			ContainerID: model.ContainerID(containerID),
			Response:    response,
			LogChan:     logChan,
		}) {
			return logStartedMsg{found: false}
		}

		c, ok := awaitResponse(response)
		if !ok {
			return logStartedMsg{found: false}
		}
		return logStartedMsg{container: c, found: c.ID != ""}
	}
}

// waitForLogLines reads the next batch of lines from the log channel.
// Returns logLinesMsg on data, logStreamDoneMsg when the channel closes.
func waitForLogLines(ch <-chan []string) tea.Cmd {
	return func() tea.Msg {
		lines, ok := <-ch
		if !ok {
			return logStreamDoneMsg{}
		}
		return logLinesMsg(lines)
	}
}

// stopLogCollectionCmd stops log collection for a container.
func (m *Model) stopLogCollectionCmd(containerID string) tea.Cmd {
	return func() tea.Msg {
		m.sendRequest(&model.RequestStopLogCollection{ContainerID: model.ContainerID(containerID)})
		return nil
	}
}

// setPendingActionCmd sets a pending action on a container.
func (m *Model) setPendingActionCmd(containerID model.ContainerID, action string) tea.Cmd {
	return func() tea.Msg {
		response := make(chan bool, 1)
		m.sendRequest(&model.RequestSetPendingAction{
			ContainerID:   containerID,
			PendingAction: action,
			Response:      response,
		})
		awaitResponse(response)
		return nil
	}
}

// clearStatusCmd clears the status message after a delay.
func clearStatusCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}
