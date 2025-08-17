package docker

type project struct {
	ID          string
	Name        string
	RenderedRow int
	Containers  []*container
}

func NewProject(id string, name string) *project {
	return &project{
		ID:         id,
		Name:       name,
		Containers: []*container{},
	}
}
