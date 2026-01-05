# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

c8s is a Terminal User Interface (TUI) application for monitoring Docker Compose projects. It displays real-time CPU and memory statistics for Docker containers organized by compose projects, and provides log viewing capabilities with filtering and formatting features.

## Build and Run Commands

```bash
# Build the binary
go build

# Run the application
./c8s

# Build with specific Go version
go build -o c8s

# Run tests (if any exist)
This project doesn't need tests
```

## Architecture

### High-Level Structure

The application uses a concurrent architecture with three main components:

1. **TUI Layer** (`tui/tui.go`): Handles terminal rendering and user interaction using tview
2. **Docker Layer** (`docker/docker.go`, `docker/container.go`): Manages Docker API interactions and container monitoring
3. **DTO Layer** (`dto/`): Data transfer objects for communication between layers

### Communication Pattern

The application uses **channel-based request/response pattern** for communication between TUI and Docker layers:

- TUI sends requests via `requestData` channel with embedded response channels
- Docker processes requests and sends responses back through the embedded channels
- This ensures thread-safe data access without shared mutable state

### Concurrency Model

#### Docker Layer (docker/docker.go)
Runs four concurrent goroutines via errgroup:
- `handleContainersCommand`: Serializes access to the containers map
- `handleEvents`: Listens to Docker events (create, start, stop, destroy)
- `collectContainers`: Initial collection of existing containers
- `handleRequests`: Processes TUI requests (project list, container list, logs)

Each Container also runs:
- `handleCommands`: Serializes access to container state
- `getContainerStatsRealtime`: Streams real-time stats from Docker API

#### TUI Layer (tui/tui.go)
- `getData`: Polls every 2 seconds based on current view (projects/containers/logs)
- View-specific refresh logic that prevents unnecessary updates

### View System

Three views managed by `currentView`:
- `viewProjectList`: Shows all Docker Compose projects with aggregated stats
- `viewProject`: Shows containers within a selected project
- `viewContainerLog`: Shows logs for a selected container

Navigation:
- Enter/Right Arrow: Drill down into selection
- Esc/Left Arrow: Go back to previous view
- View changes are synchronized with `currentViewLock`

### Log Management

Logs are collected on-demand when entering container log view:
- `LogCollectionActive` flag prevents duplicate log collection
- Logs are streamed using Docker's `ContainerLogs` API with `Follow: true`
- Limited to 1000 lines per container (`maxLogLines`) to prevent memory leaks
- Logs are cleared when exiting log view or container is deleted

Features:
- `p`: Pause/unpause log streaming
- `f`: Enable filtering by text
- `t`: Toggle timestamp display
- Automatic JSON log parsing and formatting
- Color-coded log levels (ERROR=red, WARN=yellow, INFO=green, DEBUG=gray)

### Data Refresh Pattern

The `getData` function in tui/tui.go runs on a 2-second ticker and queries data based on the current view. This causes containers to be re-sorted by CPU usage, which can be disruptive during navigation.

**Current behavior**: Every 2 seconds, containers are re-sorted by CPU percentage, causing their positions to change in the list while users are navigating.

### Container Lifecycle

1. **Discovery**: Containers found via initial `ContainerList` or Docker events
2. **Monitoring**: Stats streaming starts immediately via `getContainerStatsRealtime`
3. **Deletion**: When stats stream ends or destroy event received:
   - Container removed from map
   - `Delete()` called to cancel context and clear logs
   - If user is viewing logs, automatically returns to container list with status message

### Memory Management

- Logs limited to 1000 lines per container
- Logs cleared when exiting log view
- Logs cleared when container is deleted
- Containers removed from map when deleted or stats streaming ends

## Key Implementation Details

### Container Identification
Only containers with Docker Compose labels are tracked:
- `com.docker.compose.project.working_dir`: Used as Project ID
- `com.docker.compose.project`: Project name
- `com.docker.compose.service`: Service name

### CPU/Memory Calculations
- Follows Docker CLI's calculation methods (see references in code)
- Supports both cgroup v1 and v2
- Accounts for inactive file cache in memory calculations

### Thread Safety
- All shared state access is serialized through command channels
- No direct map access from multiple goroutines
- RWMutex used only for view state and UI data caches in TUI layer

## Logging

Application logs are written to `app.log` in JSON format with source location enabled. Use this for debugging issues.

## Dependencies

- `github.com/docker/docker`: Docker client API
- `github.com/rivo/tview`: Terminal UI framework
- `golang.org/x/sync/errgroup`: Concurrent goroutine management
