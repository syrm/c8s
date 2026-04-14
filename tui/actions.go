package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/syrm/c8s/internal/model"
)

// validShellPattern is pre-compiled for performance.
var validShellPattern = regexp.MustCompile(`^[a-zA-Z0-9/_-]+$`)

// dockerBin is the resolved absolute path to the docker binary.
var dockerBin = resolveDockerBin()

func resolveDockerBin() string {
	path, err := exec.LookPath("docker")
	if err != nil {
		return "docker"
	}
	return path
}

const defaultShell = "/bin/sh"

var preferredShells = []string{"/bin/bash", "/bin/sh", "/bin/ash"}

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

// isValidShell validates that a shell path is safe.
func isValidShell(shell string) bool {
	if shell == "" {
		return false
	}
	validShells := map[string]bool{
		"/bin/sh": true, "/bin/bash": true, "/bin/zsh": true,
		"/bin/ash": true, "/bin/fish": true, "/usr/bin/sh": true,
		"/usr/bin/bash": true, "/usr/bin/zsh": true, "/usr/bin/ash": true,
		"/usr/bin/fish": true, "sh": true, "bash": true,
		"zsh": true, "ash": true, "fish": true,
	}
	if validShells[shell] {
		return true
	}
	return validShellPattern.MatchString(shell)
}

// findAvailableShell checks which shells are available in the container.
func findAvailableShell(containerID string) string {
	for _, shell := range preferredShells {
		if !isValidShell(shell) {
			continue
		}
		cmd := exec.Command(dockerBin, "exec", containerID, "test", "-x", shell)
		if cmd.Run() == nil {
			return shell
		}
	}
	if isValidShell(defaultShell) {
		return defaultShell
	}
	return "sh"
}

// shellCmd opens an interactive shell in a container using tea.ExecProcess.
func shellCmd(containerID string) tea.Cmd {
	shell := findAvailableShell(containerID)
	c := exec.Command(dockerBin, "exec", "-it", containerID, shell)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return shellFinishedMsg{err: err}
	})
}

// containerActionCmd runs a docker command on a container asynchronously.
func containerActionCmd(containerID, dockerCmd, timeoutMsg, errorMsg string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, dockerBin, dockerCmd, containerID)
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return actionResultMsg{err: err, message: timeoutMsg}
			}
			return actionResultMsg{err: err, message: fmt.Sprintf(errorMsg, err)}
		}
		return actionResultMsg{}
	}
}

// getSelectedContainer returns the container at the given cursor index.
func getSelectedContainer(containers []model.Container, cursor int) *model.Container {
	if cursor < 0 || cursor >= len(containers) {
		return nil
	}
	c := containers[cursor]
	return &c
}
