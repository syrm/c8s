package tui

import "time"

// Timing constants
const (
	refreshInterval       = 2 * time.Second
	refreshPauseDuration  = 5 * time.Second
	statusMessageDuration = 5 * time.Second
	channelTimeout        = 5 * time.Second  // Timeout for channel operations to prevent deadlock
	dockerAPITimeout      = 30 * time.Second // Timeout for Docker API calls
)

// View type constants
type currentView int

const (
	viewProjectList currentView = iota
	viewProject
	viewContainerLog
)

// Project sort column constants
type projectSortColumn int

const (
	projectSortName projectSortColumn = iota
	projectSortCPU
	projectSortMemory
	projectSortContainers
)

// Container sort column constants
type containerSortColumn int

const (
	containerSortName containerSortColumn = iota
	containerSortCPU
	containerSortMemory
	containerSortStatus
)
