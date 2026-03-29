package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/syrm/c8s/internal/model"
	itimer "github.com/syrm/c8s/internal/timer"
)

func (t *Tui) GetRequestData() <-chan model.RequestData {
	return t.requestData
}

// sendRequest safely sends a request on the requestData channel.
// Returns false if the TUI is closing or if the send would block.
// Recovers from panic only if the channel was closed during shutdown.
func (t *Tui) sendRequest(req model.RequestData) bool {
	if t.closing.Load() {
		return false
	}

	defer func() {
		if r := recover(); r != nil {
			// Check if this is a "send on closed channel" panic
			// which is expected during shutdown
			errMsg := fmt.Sprintf("%v", r)
			if strings.Contains(errMsg, "send on closed channel") {
				// Expected during shutdown - log at debug level
				t.logger.Debug("sendRequest recovered from closed channel", slog.Any("panic", r))
				return
			}
			// Re-panic for unexpected errors to avoid masking bugs
			t.logger.Error("sendRequest recovered from unexpected panic", slog.Any("panic", r))
			panic(r)
		}
	}()

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case t.requestData <- req:
		return true
	case <-timer.C:
		return false
	}
}

func (t *Tui) getData(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
	defer itimer.StopTicker(ticker)

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			if t.closing.Load() {
				return
			}

			switch t.nav.View() {
			case viewProjectList:
				t.refreshProjectList()
			case viewProject:
				t.refreshContainerList()
			case viewContainerLog:
				t.refreshContainerLog(ctx)
			}
		}
	}
}

func (t *Tui) refreshProjectList() {
	if t.projectRefresh.IsPaused() {
		return
	}

	response := make(chan []model.Project, 1)
	if !t.sendRequest(&model.RequestProjectList{Response: response}) {
		t.logger.Warn("failed to send project list request")
		return
	}

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case projects := <-response:
		t.projectView.UpdateFrom(projects)
		t.app.QueueUpdateDraw(func() {
			t.drawProjects()
		})
	case <-timer.C:
		t.logger.Warn("timeout waiting for project list response")
	}
}

func (t *Tui) refreshContainerList() {
	if t.containerRefresh.IsPaused() {
		return
	}

	currentProjectID := t.nav.ProjectID()
	response := make(chan []model.Container, 1)
	if !t.sendRequest(&model.RequestProject{ProjectID: model.ProjectID(currentProjectID), Response: response}) {
		t.logger.Warn("failed to send container list request")
		return
	}

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case containers := <-response:
		t.containerView.UpdateFrom(containers)
		t.app.QueueUpdateDraw(func() {
			t.drawContainers()
		})
	case <-timer.C:
		t.logger.Warn("timeout waiting for container list response")
	}
}

func (t *Tui) refreshContainerLog(ctx context.Context) {
	if t.logView.Paused.Load() {
		return
	}

	if t.logView.Disappeared.Load() {
		t.handleDisappearedContainer(ctx)
		return
	}

	currentContainerID := t.nav.ContainerID()
	t.logger.DebugContext(ctx, "fetching logs for container", slog.String("container_id", currentContainerID))

	t.updateLogs(ctx)
}

func (t *Tui) handleDisappearedContainer(ctx context.Context) {
	currentContainerID := t.nav.ContainerID()

	response := make(chan model.Container, 1)
	if !t.sendRequest(&model.RequestContainerLog{ContainerID: model.ContainerID(currentContainerID), Response: response}) {
		t.logger.Warn("failed to send container log request in handleDisappearedContainer")
		return
	}

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	var c model.Container
	select {
	case c = <-response:
	case <-timer.C:
		t.logger.Warn("timeout waiting for container log response in handleDisappearedContainer")
		return
	}

	if c.ID == "" {
		t.logger.DebugContext(ctx, "container disappeared again")
		t.tryReconnectContainer(ctx)
		return
	}

	if len(c.Logs) == 0 {
		t.logger.DebugContext(ctx, "still waiting for logs...")
		return
	}

	t.logger.DebugContext(ctx, "got logs, closing modal", slog.Int("log_count", len(c.Logs)))
	t.logView.Disappeared.Store(false)
	t.logView.SetData(c.Logs)

	t.app.QueueUpdateDraw(func() {
		t.drawContainerLog()
		t.pages.HidePage("modal")
	})
}

func (t *Tui) tryReconnectContainer(ctx context.Context) {
	currentContainerService := t.nav.ContainerService()
	currentProjectID := t.nav.ProjectID()

	t.logger.DebugContext(ctx, "checking for container reappearance",
		slog.String("service", currentContainerService),
		slog.String("project", currentProjectID))

	responseProject := make(chan []model.Container, 1)
	if !t.sendRequest(&model.RequestProject{ProjectID: model.ProjectID(currentProjectID), Response: responseProject}) {
		t.logger.Warn("failed to send project request in tryReconnectContainer")
		return
	}

	timer1 := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer1)

	var containers []model.Container
	select {
	case containers = <-responseProject:
	case <-timer1.C:
		t.logger.Warn("timeout waiting for project response in tryReconnectContainer")
		return
	}

	var foundContainer *model.Container
	for _, container := range containers {
		t.logger.DebugContext(ctx, "checking container",
			slog.String("container_service", container.Service),
			slog.String("looking_for", currentContainerService))
		if container.Service == currentContainerService && string(container.Project.ID) == currentProjectID {
			foundContainer = &container
			break
		}
	}

	if foundContainer == nil {
		return
	}

	t.logger.InfoContext(ctx, "container reappeared, starting log collection",
		slog.String("service", foundContainer.Service),
		slog.String("new_id", string(foundContainer.ID)))

	t.nav.SetContainerInfo(string(foundContainer.ID), foundContainer.Name, foundContainer.Service)

	response := make(chan model.Container, 1)
	newContainerID := t.nav.ContainerID()
	if !t.sendRequest(&model.RequestContainerLog{ContainerID: model.ContainerID(newContainerID), Response: response}) {
		t.logger.Warn("failed to send container log request in tryReconnectContainer")
		return
	}

	timer2 := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer2)

	var c model.Container
	select {
	case c = <-response:
	case <-timer2.C:
		t.logger.Warn("timeout waiting for container log response in tryReconnectContainer")
		return
	}

	if c.ID == "" {
		t.logger.DebugContext(ctx, "container reappeared but logs not ready yet")
		return
	}
}

func (t *Tui) startLogCollection(ctx context.Context) {
	currentContainerID := t.nav.ContainerID()
	currentContainerName := t.nav.ContainerName()

	response := make(chan model.Container, 1)
	if !t.sendRequest(&model.RequestContainerLog{ContainerID: model.ContainerID(currentContainerID), Response: response}) {
		t.logger.Warn("failed to send container log request in startLogCollection")
		return
	}

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	var c model.Container
	select {
	case c = <-response:
	case <-timer.C:
		t.logger.Warn("timeout waiting for container log response in startLogCollection")
		return
	}

	if c.ID == "" {
		t.logView.Disappeared.Store(true)

		displayID := currentContainerID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		t.app.QueueUpdateDraw(func() {
			t.disappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", currentContainerName, displayID))
			t.pages.ShowPage("modal")
		})
		return
	}
}

func (t *Tui) updateLogs(ctx context.Context) {
	currentContainerID := t.nav.ContainerID()
	currentContainerName := t.nav.ContainerName()

	response := make(chan model.Container, 1)
	if !t.sendRequest(&model.RequestContainerLog{ContainerID: model.ContainerID(currentContainerID), Response: response}) {
		t.logger.Warn("failed to send container log request in updateLogs")
		return
	}

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	var c model.Container
	select {
	case c = <-response:
	case <-timer.C:
		t.logger.Warn("timeout waiting for container log response in updateLogs")
		return
	}

	if c.ID == "" {
		t.logView.Disappeared.Store(true)

		displayID := currentContainerID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		t.app.QueueUpdateDraw(func() {
			t.disappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", currentContainerName, displayID))
			t.pages.ShowPage("modal")
		})
		return
	}

	if len(c.Logs) > 0 {
		t.logView.SetData(c.Logs)
	}

	t.app.QueueUpdateDraw(func() {
		t.drawContainerLog()
	})
}
