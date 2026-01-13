package tui

import (
	"sync"

	"github.com/rivo/tview"
)

// SearchState manages search input and query.
type SearchState struct {
	Input *tview.InputField
	
	query string
	mu    sync.RWMutex
}

func NewSearchState() *SearchState {
	return &SearchState{
		Input: createSearchInput(),
	}
}

func (s *SearchState) Query() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.query
}

func (s *SearchState) SetQuery(q string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.query = q
}
