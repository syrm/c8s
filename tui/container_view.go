package tui

import (
	"sync"

	"github.com/rivo/tview"

	"github.com/syrm/c8s/internal/model"
)

// ContainerView holds all container view state.
type ContainerView struct {
	Table  *tview.Table
	Layout *tview.Flex
	Search *SearchState
	Sort   SortState[containerSortColumn]
	
	data map[model.ContainerID]model.Container
	mu   sync.RWMutex
}

func (c *ContainerView) Get(id model.ContainerID) (model.Container, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[id]
	return v, ok
}

func (c *ContainerView) Set(id model.ContainerID, v model.Container) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[model.ContainerID]model.Container)
	}
	c.data[id] = v
}

func (c *ContainerView) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

func (c *ContainerView) Values() []model.Container {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]model.Container, 0, len(c.data))
	for _, v := range c.data {
		result = append(result, v)
	}
	return result
}

func (c *ContainerView) UpdateFrom(items []model.Container) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.data == nil {
		c.data = make(map[model.ContainerID]model.Container)
	}

	activeKeys := make(map[model.ContainerID]struct{}, len(items))
	for _, item := range items {
		c.data[item.ID] = item
		activeKeys[item.ID] = struct{}{}
	}

	for k := range c.data {
		if _, exists := activeKeys[k]; !exists {
			delete(c.data, k)
		}
	}
}
