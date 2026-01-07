package tui

import (
	"time"

	"github.com/syrm/c8s/dto"
)

// Timing constants
const (
	refreshInterval       = 2 * time.Second
	refreshPauseDuration  = 5 * time.Second
	statusMessageDuration = 5 * time.Second
)

// resourceWarningThreshold is the percentage above which a warning is shown.
const resourceWarningThreshold = 80.0

// defaultShell is the default shell to use when opening a container shell.
const defaultShell = "/bin/sh"

// channelTimeout is imported from dto for consistent timeout across layers.
const channelTimeout = dto.ChannelTimeout

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
