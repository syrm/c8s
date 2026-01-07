package tui

import (
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

	t.tableContainerDataLock.RLock()
	defer t.tableContainerDataLock.RUnlock()

	cell := t.tableContainer.GetCell(rowIndex, 0)
	if cell == nil {
		return nil
	}
	cellText := cell.Text
	for _, container := range t.tableContainerData {
		if !strings.Contains(cellText, container.Service) {
			continue
		}

		// Apply status filter
		switch filter {
		case filterRunning:
			if container.Status != "running" {
				continue
			}
		case filterNotRunning:
			if container.Status == "running" {
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
	select {
	case t.requestData <- &dto.RequestSetPendingAction{
		ContainerID:   containerID,
		PendingAction: action,
		Response:      response,
	}:
	case <-time.After(channelTimeout):
		return
	}

	select {
	case <-response:
	case <-time.After(channelTimeout):
	}
}

// updateLocalCache updates the local container cache with a pending action.
func (t *Tui) updateLocalCache(containerID dto.ContainerID, action string) {
	t.tableContainerDataLock.Lock()
	if c, ok := t.tableContainerData[containerID]; ok {
		c.PendingAction = action
		t.tableContainerData[containerID] = c
	}
	t.tableContainerDataLock.Unlock()
}

// handleContainerShell opens an interactive shell in the selected container.
func (t *Tui) handleContainerShell() bool {
	rowIndex, _ := t.tableContainer.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterRunning)
	if container == nil {
		return false
	}

	t.app.Suspend(func() {
		cmd := exec.Command("docker", "exec", "-it", string(container.ID), "/bin/sh")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	})
	return true
}

// handleContainerStop stops the selected running container.
func (t *Tui) handleContainerStop() bool {
	rowIndex, _ := t.tableContainer.GetSelection()
	container := t.getSelectedContainer(rowIndex, filterRunning)
	if container == nil {
		return false
	}

	t.setPendingAction(container.ID, "stopping")
	t.updateLocalCache(container.ID, "stopping")
	t.drawContainers()

	go func() {
		cmd := exec.Command("docker", "stop", string(container.ID))
		if err := cmd.Run(); err != nil {
			t.app.QueueUpdateDraw(func() {
				t.showStatusMessage(fmt.Sprintf("Failed to stop container: %v", err))
			})
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
	if container.Status == "running" {
		action = "restarting"
	} else {
		action = "starting"
	}

	t.setPendingAction(container.ID, action)
	t.updateLocalCache(container.ID, action)
	t.drawContainers()

	go func() {
		var cmd *exec.Cmd
		if container.Status == "running" {
			cmd = exec.Command("docker", "restart", string(container.ID))
		} else {
			cmd = exec.Command("docker", "start", string(container.ID))
		}
		if err := cmd.Run(); err != nil {
			t.app.QueueUpdateDraw(func() {
				t.showStatusMessage(fmt.Sprintf("Failed to %s container: %v", action[:len(action)-3], err))
			})
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

	t.setPendingAction(container.ID, "removing")
	t.updateLocalCache(container.ID, "removing")
	t.drawContainers()

	go func() {
		cmd := exec.Command("docker", "rm", string(container.ID))
		if err := cmd.Run(); err != nil {
			t.app.QueueUpdateDraw(func() {
				t.showStatusMessage(fmt.Sprintf("Failed to remove container: %v", err))
			})
		}
	}()
	return true
}
