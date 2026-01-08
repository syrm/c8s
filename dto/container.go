package dto

// Container status constants.
const (
	StatusRunning    = "running"
	StatusExited     = "exited"
	StatusPaused     = "paused"
	StatusRestarting = "restarting"
	StatusCreated    = "created"
	StatusRemoving   = "removing"
)

// ContainerID is a unique identifier for a Docker container.
type ContainerID string

// Container represents a Docker container state as transferred between layers.
type Container struct {
	ID               ContainerID
	Project          ContainerProject
	Service          string
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	Logs             []string
	Status           string
	PendingAction    string // "starting", "stopping", "restarting", "removing" or ""
}

// ContainerProject contains the project information for a container.
type ContainerProject struct {
	ID   ProjectID
	Name string
}
