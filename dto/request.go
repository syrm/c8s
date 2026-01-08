package dto

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
type RequestContainerLog struct {
	ContainerID ContainerID
	Response    chan Container
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

// NewRequestProjectList creates a new RequestProjectList with a buffered response channel.
func NewRequestProjectList() *RequestProjectList {
	return &RequestProjectList{
		Response: make(chan []Project, 1),
	}
}

// NewRequestContainerLog creates a new RequestContainerLog with a buffered response channel.
func NewRequestContainerLog(id ContainerID) *RequestContainerLog {
	return &RequestContainerLog{
		ContainerID: id,
		Response:    make(chan Container, 1),
	}
}

// NewRequestProject creates a new RequestProject with a buffered response channel.
func NewRequestProject(id ProjectID) *RequestProject {
	return &RequestProject{
		ProjectID: id,
		Response:  make(chan []Container, 1),
	}
}

// NewRequestSetPendingAction creates a new RequestSetPendingAction with a buffered response channel.
func NewRequestSetPendingAction(id ContainerID, action string) *RequestSetPendingAction {
	return &RequestSetPendingAction{
		ContainerID:   id,
		PendingAction: action,
		Response:      make(chan bool, 1),
	}
}
