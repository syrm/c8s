package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/syrm/c8s/dto"
	itimer "github.com/syrm/c8s/internal/timer"
)

// containerStatusFilter defines which container statuses are allowed for an action.
type containerStatusFilter int

const (
	filterRunning    containerStatusFilter = iota // Only running containers
	filterNotRunning                              // Only non-running containers
	filterAny                                     // Any status
)

// isValidContainerID validates that a container ID has the expected Docker format.
func isValidContainerID(id string) bool {
	if len(id) < 12 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// getSelectedContainer returns the container at the selected row, filtered by status.
func (t *Tui) getSelectedContainer(rowIndex int, filter containerStatusFilter) *dto.Container {
	if rowIndex <= 0 {
		return nil
	}

	cell := t.containerView.Table.GetCell(rowIndex, 0)
	if cell == nil {
		return nil
	}
	cellText := stripWarningPrefix(cell.Text)

	containers := t.containerView.Data.Values()
	for _, container := range containers {
		if cellText != container.Service {
			continue
		}

		switch filter {
		case filterRunning:
			if container.Status != dto.StatusRunning {
				continue
			}
		case filterNotRunning:
			if container.Status == dto.StatusRunning {
				continue
			}
		}

		c := container
		return &c
	}
	return nil
}

// setPendingAction sends a request to set a pending action on a container.
func (t *Tui) setPendingAction(containerID dto.ContainerID, action string) {
	response := make(chan bool, 1)
	if !t.sendRequest(&dto.RequestSetPendingAction{
		ContainerID:   containerID,
		PendingAction: action,
		Response:      response,
	}) {
		return
	}

	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case <-response:
	case <-timer.C:
	}
}

// updateLocalCache updates the local container cache with a pending action.
func (t *Tui) updateLocalCache(containerID dto.ContainerID, action string) {
	if c, ok := t.containerView.Data.Get(containerID); ok {
		c.PendingAction = action
		t.containerView.Data.Set(containerID, c)
	}
}

// handleContainerShell opens an interactive shell in the selected container.
func (t *Tui) handleContainerShell() bool {
	rowIndex, _ := t.containerView.Table.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterRunning)
	if container == nil {
		return false
	}

	if !isValidContainerID(string(container.ID)) {
		t.showStatusMessage("Invalid container ID")
		return false
	}

	shell := findAvailableShell(string(container.ID))

	var shellErr error
	t.app.Suspend(func() {
		cmd := exec.Command("docker", "exec", "-it", string(container.ID), shell)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		shellErr = cmd.Run()
	})

	if shellErr != nil {
		t.app.QueueUpdateDraw(func() {
			t.showStatusMessage(fmt.Sprintf("Shell exited with error: %v", shellErr))
		})
	}
	return true
}

// findAvailableShell checks which shells are available in the container.
func findAvailableShell(containerID string) string {
	for _, shell := range preferredShells {
		cmd := exec.Command("docker", "exec", containerID, "test", "-x", shell)
		if cmd.Run() == nil {
			return shell
		}
	}
	return defaultShell
}

// handleContainerStop stops the selected running container.
func (t *Tui) handleContainerStop() bool {
	rowIndex, _ := t.containerView.Table.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterRunning)
	if container == nil {
		return false
	}

	if !isValidContainerID(string(container.ID)) {
		t.showStatusMessage("Invalid container ID")
		return false
	}

	if !t.actions.TryAcquire() {
		t.showStatusMessage("Too many pending actions, please wait")
		return false
	}

	t.setPendingAction(container.ID, actionStopping)
	t.updateLocalCache(container.ID, actionStopping)
	t.drawContainers()

	t.actions.Add(1)
	go func() {
		defer t.actions.Done()
		defer t.actions.Release()

		ctx, cancel := context.WithTimeout(t.actions.Context(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "docker", "stop", string(container.ID))
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				t.app.QueueUpdateDraw(func() {
					t.showStatusMessage("Stop container timed out")
				})
			} else {
				t.app.QueueUpdateDraw(func() {
					t.showStatusMessage(fmt.Sprintf("Failed to stop container: %v", err))
				})
			}
		}
	}()
	return true
}

// handleContainerRestart restarts a running container or starts a stopped one.
func (t *Tui) handleContainerRestart() bool {
	rowIndex, _ := t.containerView.Table.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterAny)
	if container == nil {
		return false
	}

	if !isValidContainerID(string(container.ID)) {
		t.showStatusMessage("Invalid container ID")
		return false
	}

	if !t.actions.TryAcquire() {
		t.showStatusMessage("Too many pending actions, please wait")
		return false
	}

	var action string
	if container.Status == dto.StatusRunning {
		action = actionRestarting
	} else {
		action = actionStarting
	}

	t.setPendingAction(container.ID, action)
	t.updateLocalCache(container.ID, action)
	t.drawContainers()

	t.actions.Add(1)
	go func() {
		defer t.actions.Done()
		defer t.actions.Release()

		ctx, cancel := context.WithTimeout(t.actions.Context(), 30*time.Second)
		defer cancel()

		var cmd *exec.Cmd
		if container.Status == dto.StatusRunning {
			cmd = exec.CommandContext(ctx, "docker", "restart", string(container.ID))
		} else {
			cmd = exec.CommandContext(ctx, "docker", "start", string(container.ID))
		}
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				t.app.QueueUpdateDraw(func() {
					t.showStatusMessage("Restart container timed out")
				})
			} else {
				verb := strings.TrimSuffix(action, "ing")
				t.app.QueueUpdateDraw(func() {
					t.showStatusMessage(fmt.Sprintf("Failed to %s container: %v", verb, err))
				})
			}
		}
	}()
	return true
}

// handleContainerRemove removes the selected stopped container.
func (t *Tui) handleContainerRemove() bool {
	rowIndex, _ := t.containerView.Table.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterNotRunning)
	if container == nil {
		return false
	}

	if !isValidContainerID(string(container.ID)) {
		t.showStatusMessage("Invalid container ID")
		return false
	}

	if !t.actions.TryAcquire() {
		t.showStatusMessage("Too many pending actions, please wait")
		return false
	}

	t.setPendingAction(container.ID, actionRemoving)
	t.updateLocalCache(container.ID, actionRemoving)
	t.drawContainers()

	t.actions.Add(1)
	go func() {
		defer t.actions.Done()
		defer t.actions.Release()

		ctx, cancel := context.WithTimeout(t.actions.Context(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "docker", "rm", string(container.ID))
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				t.app.QueueUpdateDraw(func() {
					t.showStatusMessage("Remove container timed out")
				})
			} else {
				t.app.QueueUpdateDraw(func() {
					t.showStatusMessage(fmt.Sprintf("Failed to remove container: %v", err))
				})
			}
		}
	}()
	return true
}
