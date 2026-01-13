package tui

import (
	"sync"
)

// NavigationState manages the current navigation state.
type NavigationState struct {
	view             currentView
	projectID        string
	projectName      string
	containerID      string
	containerName    string
	containerService string
	mu               sync.RWMutex
}

func (n *NavigationState) View() currentView {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.view
}

func (n *NavigationState) SetView(v currentView) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.view = v
}

func (n *NavigationState) ProjectID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.projectID
}

func (n *NavigationState) SetProjectID(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.projectID = id
}

func (n *NavigationState) ProjectName() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.projectName
}

func (n *NavigationState) SetProjectName(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.projectName = name
}

func (n *NavigationState) ContainerID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.containerID
}

func (n *NavigationState) SetContainerID(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.containerID = id
}

func (n *NavigationState) ContainerName() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.containerName
}

func (n *NavigationState) ContainerService() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.containerService
}

func (n *NavigationState) SetContainerInfo(id, name, service string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.containerID = id
	n.containerName = name
	n.containerService = service
}

func (n *NavigationState) ClearContainerInfo() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.containerID = ""
	n.containerName = ""
	n.containerService = ""
}
