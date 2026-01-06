package docker

// DockerCommand represents a type-safe command to the Docker service.
// This replaces the functor-based ContainersCommand pattern.
type DockerCommand interface {
	isDockerCommand()
}

// GetContainerCmd requests a container by ID.
type GetContainerCmd struct {
	ID       ContainerID
	Response chan *Container
}

func (GetContainerCmd) isDockerCommand() {}

// SetContainerCmd sets a container in the map.
type SetContainerCmd struct {
	Container *Container
}

func (SetContainerCmd) isDockerCommand() {}

// DeleteContainerCmd deletes a container from the map.
type DeleteContainerCmd struct {
	ID ContainerID
}

func (DeleteContainerCmd) isDockerCommand() {}

// ListContainersForProjectCmd lists all containers for a project.
type ListContainersForProjectCmd struct {
	ProjectID string
	Response  chan []*Container
}

func (ListContainersForProjectCmd) isDockerCommand() {}

// ListAllContainersCmd lists all containers.
type ListAllContainersCmd struct {
	Response chan []*Container
}

func (ListAllContainersCmd) isDockerCommand() {}
