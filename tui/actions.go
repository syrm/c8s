package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/syrm/c8s/internal/model"
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
func (t *Tui) getSelectedContainer(rowIndex int, filter containerStatusFilter) *model.Container {
	if rowIndex <= 0 {
		return nil
	}

	cell := t.containerView.Table.GetCell(rowIndex, 0)
	if cell == nil {
		return nil
	}
	cellText := stripWarningPrefix(cell.Text)

	containers := t.containerView.Values()
	for _, container := range containers {
		if cellText != container.Service {
			continue
		}

		switch filter {
		case filterRunning:
			if container.Status != model.StatusRunning {
				continue
			}
		case filterNotRunning:
			if container.Status == model.StatusRunning {
				continue
			}
		}

		c := container
		return &c
	}
	return nil
}

// setPendingAction sends a request to set a pending action on a container.
func (t *Tui) setPendingAction(containerID model.ContainerID, action string) {
	response := make(chan bool, 1)
	if !t.sendRequest(&model.RequestSetPendingAction{
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
func (t *Tui) updateLocalCache(containerID model.ContainerID, action string) {
	if c, ok := t.containerView.Get(containerID); ok {
		c.PendingAction = action
		t.containerView.Set(containerID, c)
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

// containerActionParams defines the parameters for a container action.
type containerActionParams struct {
	pendingAction string
	dockerCmd     string
	timeoutMsg    string
	errorMsg      string
}

// runContainerAction runs a docker command on a container asynchronously.
// The container must already be validated before calling this function.
func (t *Tui) runContainerAction(container *model.Container, params containerActionParams) bool {
	if !t.actions.TryAcquire() {
		t.showStatusMessage("Too many pending actions, please wait")
		return false
	}

	t.setPendingAction(container.ID, params.pendingAction)
	t.updateLocalCache(container.ID, params.pendingAction)
	t.drawContainers()

	containerID := string(container.ID)
	t.actions.Add(1)
	go func() {
		defer t.actions.Done()
		defer t.actions.Release()

		ctx, cancel := context.WithTimeout(t.actions.Context(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "docker", params.dockerCmd, containerID)
		if err := cmd.Run(); err != nil {
			t.app.QueueUpdateDraw(func() {
				if ctx.Err() == context.DeadlineExceeded {
					t.showStatusMessage(params.timeoutMsg)
				} else {
					t.showStatusMessage(fmt.Sprintf(params.errorMsg, err))
				}
			})
		}
	}()
	return true
}

// validateAndGetContainer gets and validates a container for an action.
func (t *Tui) validateAndGetContainer(filter containerStatusFilter) *model.Container {
	rowIndex, _ := t.containerView.Table.GetSelection()
	container := t.getSelectedContainer(rowIndex, filter)
	if container == nil {
		return nil
	}

	if !isValidContainerID(string(container.ID)) {
		t.showStatusMessage("Invalid container ID")
		return nil
	}

	return container
}

// handleContainerStop stops the selected running container.
func (t *Tui) handleContainerStop() bool {
	container := t.validateAndGetContainer(filterRunning)
	if container == nil {
		return false
	}
	return t.runContainerAction(container, containerActionParams{
		pendingAction: actionStopping,
		dockerCmd:     "stop",
		timeoutMsg:    "Stop container timed out",
		errorMsg:      "Failed to stop container: %v",
	})
}

// handleContainerRestart restarts a running container or starts a stopped one.
func (t *Tui) handleContainerRestart() bool {
	container := t.validateAndGetContainer(filterAny)
	if container == nil {
		return false
	}

	if container.Status == model.StatusRunning {
		return t.runContainerAction(container, containerActionParams{
			pendingAction: actionRestarting,
			dockerCmd:     "restart",
			timeoutMsg:    "Restart container timed out",
			errorMsg:      "Failed to restart container: %v",
		})
	}
	return t.runContainerAction(container, containerActionParams{
		pendingAction: actionStarting,
		dockerCmd:     "start",
		timeoutMsg:    "Start container timed out",
		errorMsg:      "Failed to start container: %v",
	})
}

// handleContainerRemove removes the selected stopped container.
func (t *Tui) handleContainerRemove() bool {
	container := t.validateAndGetContainer(filterNotRunning)
	if container == nil {
		return false
	}
	return t.runContainerAction(container, containerActionParams{
		pendingAction: actionRemoving,
		dockerCmd:     "rm",
		timeoutMsg:    "Remove container timed out",
		errorMsg:      "Failed to remove container: %v",
	})
}

// ActionController manages container action goroutines.
type ActionController struct {
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	wg            sync.WaitGroup
	sem           chan struct{}
	maxConcurrent int
}

// NewActionController creates a new ActionController with placeholder context.
// Call Start(ctx) to initialize with a proper parent context.
func NewActionController(maxConcurrent int) *ActionController {
	return &ActionController{
		ctx:           context.Background(), // Placeholder until Start() is called
		sem:           make(chan struct{}, maxConcurrent),
		maxConcurrent: maxConcurrent,
	}
}

// Start initializes the ActionController with a parent context.
// This should be called before using the controller.
func (a *ActionController) Start(parent context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ctx, a.cancel = context.WithCancel(parent)
}

func (a *ActionController) Context() context.Context {
	return a.ctx
}

func (a *ActionController) TryAcquire() bool {
	select {
	case a.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *ActionController) Release() {
	<-a.sem
}

func (a *ActionController) Add(delta int) {
	a.wg.Add(delta)
}

func (a *ActionController) Done() {
	a.wg.Done()
}

func (a *ActionController) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

func (a *ActionController) Wait() {
	a.wg.Wait()
}