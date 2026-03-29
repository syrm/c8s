# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

c8s is a Docker Compose monitoring and management TUI (Terminal User Interface) application written in Go. It provides real-time container monitoring with CPU/memory metrics, log streaming, and container lifecycle operations.

## Build & Development Commands

```bash
# Build the binary
go build -o c8s main.go

# Run the application
./c8s

# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run a single test
go test -v -run TestContainersCommandConcurrency ./docker/

# Install/update dependencies
go mod download
go mod tidy
```

## Architecture

The codebase follows a layered architecture with channel-based communication between layers:

```
main.go
    │
    ├── tui/ (Terminal UI layer)
    │   └── Uses tview/tcell for rendering
    │
    └── docker/ (Docker API layer)
        └── Interfaces with Docker/Podman daemon

internal/
    ├── model/   (Shared data structures)
    └── channel/ (Channel utilities with timeout)
```

### Key Design Patterns

**Interface-based Docker API** (`docker/api.go`): The `DockerAPI` interface allows mocking the Docker client in tests. Tests use `mockDockerAPI` to simulate Docker responses.

**Channel-based concurrency**: The TUI and Docker layers communicate via channels with 5-second timeouts (defined in `internal/model/constants.go`). This prevents deadlocks and allows graceful shutdown.

**Command pattern for container operations**: Each container has a `Command` channel that serializes operations via `handleCommands()`, ensuring thread-safe access to container state.

### Important Files

- `docker/docker.go`: Main Docker client, container state management, goroutine lifecycle
- `docker/container.go`: Container model with command handler
- `tui/tui.go`: Main TUI controller, page management
- `tui/data.go`: Data binding between Docker layer and UI
- `internal/model/request.go`: Request/response types for TUI-Docker communication

### Testing Approach

Tests focus on concurrency safety. The test suite (`docker/docker_test.go`) validates:
- Multiple goroutines safely accessing container commands
- Channel helper functions under concurrent load
- Atomic operations on stats generation counters
- Log collection flag race conditions

## Runtime Notes

- Logs are written to `app.log` in JSON format in the current directory
- Supports both Docker daemon and Podman socket (automatic fallback)
- Container stats refresh continuously; logs stream in real-time
