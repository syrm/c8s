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

// maxConcurrentActions limits the number of concurrent container actions.
const maxConcurrentActions = 10

// resourceWarningThreshold is the percentage above which a warning is shown.
const resourceWarningThreshold = 80.0

// defaultShell is the fallback shell to use when no preferred shell is available.
const defaultShell = "/bin/sh"

// preferredShells is a list of shells to try in order of preference.
// We try bash first as it provides a better user experience.
var preferredShells = []string{"/bin/bash", "/bin/sh", "/bin/ash"}

// Pending action constants for container operations.
const (
	actionStopping   = "stopping"
	actionStarting   = "starting"
	actionRestarting = "restarting"
	actionRemoving   = "removing"
)

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
