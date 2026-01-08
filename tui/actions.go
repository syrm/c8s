package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/syrm/c8s/dto"
)

// containerStatusFilter defines which container statuses are allowed for an action.
type containerStatusFilter int

const (
	filterRunning    containerStatusFilter = iota // Only running containers
	filterNotRunning                              // Only non-running containers
	filterAny                                     // Any status
)

// getSelectedContainer returns the container at the selected row, filtered by status.
func (t *Tui) getSelectedContainer(rowIndex int, filter containerStatusFilter) *dto.Container {
	if rowIndex <= 0 {
		return nil
	}

	cell := t.tableContainer.GetCell(rowIndex, 0)
	if cell == nil {
		return nil
	}
	cellText := stripWarningPrefix(cell.Text)

	t.tableContainerDataLock.RLock()
	defer t.tableContainerDataLock.RUnlock()

	for _, container := range t.tableContainerData {
		if cellText != container.Service {
			continue
		}

		// Apply status filter
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
	timer := time.NewTimer(channelTimeout)
	defer timer.Stop()

	select {
	case t.requestData <- &dto.RequestSetPendingAction{
		ContainerID:   containerID,
		PendingAction: action,
		Response:      response,
	}:
	case <-timer.C:
		return
	}

	select {
	case <-response:
	case <-timer.C:
	}
}

// updateLocalCache updates the local container cache with a pending action.
// This is an optimization to avoid waiting for the next refresh cycle.
func (t *Tui) updateLocalCache(containerID dto.ContainerID, action string) {
	t.tableContainerDataLock.Lock()
	defer t.tableContainerDataLock.Unlock()

	if c, ok := t.tableContainerData[containerID]; ok {
		// Create a copy with the updated pending action
		// This is necessary because dto.Container is a value type in the map
		c.PendingAction = action
		t.tableContainerData[containerID] = c
	}
}

// handleContainerShell opens an interactive shell in the selected container.
func (t *Tui) handleContainerShell() bool {
	rowIndex, _ := t.tableContainer.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterRunning)
	if container == nil {
		return false
	}

	var shellErr error
	t.app.Suspend(func() {
		cmd := exec.Command("docker", "exec", "-it", string(container.ID), defaultShell)
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

// handleContainerStop stops the selected running container.
func (t *Tui) handleContainerStop() bool {
	rowIndex, _ := t.tableContainer.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterRunning)
	if container == nil {
		return false
	}

	t.setPendingAction(container.ID, actionStopping)
	t.updateLocalCache(container.ID, actionStopping)
	t.drawContainers()

	// Track the action goroutine for proper cleanup
	t.actionsWG.Add(1)
	go func() {
		defer t.actionsWG.Done()

		// Use the shared actions context with timeout to prevent hanging
		ctx, cancel := context.WithTimeout(t.actionsCtx, 30*time.Second)
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
	rowIndex, _ := t.tableContainer.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterAny)
	if container == nil {
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

	// Track the action goroutine for proper cleanup
	t.actionsWG.Add(1)
	go func() {
		defer t.actionsWG.Done()

		// Use the shared actions context with timeout to prevent hanging
		ctx, cancel := context.WithTimeout(t.actionsCtx, 30*time.Second)
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
	rowIndex, _ := t.tableContainer.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterNotRunning)
	if container == nil {
		return false
	}

	t.setPendingAction(container.ID, actionRemoving)
	t.updateLocalCache(container.ID, actionRemoving)
	t.drawContainers()

	// Track the action goroutine for proper cleanup
	t.actionsWG.Add(1)
	go func() {
		defer t.actionsWG.Done()

		// Use the shared actions context with timeout to prevent hanging
		ctx, cancel := context.WithTimeout(t.actionsCtx, 30*time.Second)
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
