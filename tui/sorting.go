package tui

import (
	"strings"

	"github.com/syrm/c8s/internal/model"
)

// Sort column constants for projects.
type projectSortColumn int

const (
	projectSortName projectSortColumn = iota
	projectSortCPU
	projectSortMemory
	projectSortContainers
)

// Sort column constants for containers.
type containerSortColumn int

const (
	containerSortName containerSortColumn = iota
	containerSortCPU
	containerSortMemory
	containerSortStatus
)

// sortState holds the current sort column and direction.
type sortState[T comparable] struct {
	column T
	asc    bool
}

func (s *sortState[T]) toggle(col T, defaultAsc bool) {
	if s.column == col {
		s.asc = !s.asc
	} else {
		s.column = col
		s.asc = defaultAsc
	}
}

// fuzzyMatch checks if all characters in query appear in order in text.
func fuzzyMatch(text, query string) bool {
	if query == "" {
		return true
	}

	textLower := strings.ToLower(text)
	queryLower := strings.ToLower(query)

	textRunes := []rune(textLower)
	textIdx := 0

	for _, queryChar := range queryLower {
		found := false
		for textIdx < len(textRunes) {
			if textRunes[textIdx] == queryChar {
				found = true
				textIdx++
				break
			}
			textIdx++
		}
		if !found {
			return false
		}
	}
	return true
}

// filterProjects returns projects matching the search query.
func filterProjects(projects []model.Project, query string) []model.Project {
	if query == "" {
		return projects
	}

	filtered := make([]model.Project, 0, len(projects))
	for _, project := range projects {
		if fuzzyMatch(project.Name, query) {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

// compareProjects returns comparison result for sorting projects.
func compareProjects(a, b model.Project, sortColumn projectSortColumn, ascending bool) int {
	var cmp int
	switch sortColumn {
	case projectSortName:
		cmp = strings.Compare(a.Name, b.Name)
	case projectSortCPU:
		if a.CPUPercentage < b.CPUPercentage {
			cmp = -1
		} else if a.CPUPercentage > b.CPUPercentage {
			cmp = 1
		}
	case projectSortMemory:
		if a.MemoryPercentage < b.MemoryPercentage {
			cmp = -1
		} else if a.MemoryPercentage > b.MemoryPercentage {
			cmp = 1
		}
	case projectSortContainers:
		if a.ContainersRunning < b.ContainersRunning {
			cmp = -1
		} else if a.ContainersRunning > b.ContainersRunning {
			cmp = 1
		}
	default:
		cmp = 0
	}

	if !ascending {
		cmp = -cmp
	}

	if cmp == 0 {
		cmp = strings.Compare(a.Name, b.Name)
	}

	return cmp
}

// compareContainers returns comparison result for sorting containers.
func compareContainers(a, b model.Container, sortColumn containerSortColumn, ascending bool) int {
	var cmp int
	switch sortColumn {
	case containerSortName:
		cmp = strings.Compare(a.Service, b.Service)
	case containerSortCPU:
		if a.CPUPercentage < b.CPUPercentage {
			cmp = -1
		} else if a.CPUPercentage > b.CPUPercentage {
			cmp = 1
		}
	case containerSortMemory:
		if a.MemoryPercentage < b.MemoryPercentage {
			cmp = -1
		} else if a.MemoryPercentage > b.MemoryPercentage {
			cmp = 1
		}
	case containerSortStatus:
		cmp = strings.Compare(a.Status, b.Status)
	default:
		cmp = 0
	}

	if !ascending {
		cmp = -cmp
	}

	if cmp == 0 {
		cmp = strings.Compare(a.Service, b.Service)
	}

	return cmp
}
