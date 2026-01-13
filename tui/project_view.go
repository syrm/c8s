package tui

import (
	"sync"

	"github.com/rivo/tview"

	"github.com/syrm/c8s/internal/model"
)

// ProjectView holds all project view state.
type ProjectView struct {
	Table  *tview.Table
	Layout *tview.Flex
	Search *SearchState
	Sort   SortState[projectSortColumn]
	
	data map[model.ProjectID]model.Project
	mu   sync.RWMutex
}

func (p *ProjectView) Get(id model.ProjectID) (model.Project, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.data[id]
	return v, ok
}

func (p *ProjectView) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.data)
}

func (p *ProjectView) Values() []model.Project {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]model.Project, 0, len(p.data))
	for _, v := range p.data {
		result = append(result, v)
	}
	return result
}

func (p *ProjectView) UpdateFrom(items []model.Project) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.data == nil {
		p.data = make(map[model.ProjectID]model.Project)
	}

	activeKeys := make(map[model.ProjectID]struct{}, len(items))
	for _, item := range items {
		p.data[item.ID] = item
		activeKeys[item.ID] = struct{}{}
	}

	for k := range p.data {
		if _, exists := activeKeys[k]; !exists {
			delete(p.data, k)
		}
	}
}
