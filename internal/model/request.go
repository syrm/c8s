package model

// RequestData is the interface for all request types between TUI and Docker layers.
type RequestData interface {
	isRequestData()
}

// RequestProjectList requests the list of all projects.
type RequestProjectList struct {
	Response chan []Project
}

func (p *RequestProjectList) isRequestData() {}

// RequestContainerLog requests logs for a specific container.
// LogChan receives batches of new log lines streamed from Docker.
type RequestContainerLog struct {
	ContainerID ContainerID
	Response    chan Container
	LogChan     chan []string
}

func (p *RequestContainerLog) isRequestData() {}

// RequestProject requests containers for a specific project.
type RequestProject struct {
	ProjectID ProjectID
	Response  chan []Container
}

func (p *RequestProject) isRequestData() {}

// RequestSetPendingAction sets a pending action on a container.
type RequestSetPendingAction struct {
	ContainerID   ContainerID
	PendingAction string
	Response      chan bool
}

func (p *RequestSetPendingAction) isRequestData() {}

// RequestStopLogCollection stops the log collection for a specific container.
type RequestStopLogCollection struct {
	ContainerID ContainerID
}

func (p *RequestStopLogCollection) isRequestData() {}
