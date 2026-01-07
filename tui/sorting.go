package tui

import (
	"strings"

	"github.com/syrm/c8s/dto"
)

// warningPrefix is the tview formatting prefix added to items with high CPU/memory usage.
const warningPrefix = "[yellow]⚠[-] "

// stripWarningPrefix removes the warning prefix from cell text if present.
func stripWarningPrefix(text string) string {
	if strings.HasPrefix(text, warningPrefix) {
		return text[len(warningPrefix):]
	}
	return text
}

// fuzzyMatch checks if all characters in query appear in order in text.
// Example: "cr" matches "container" because 'c' and 'r' appear in order.
func fuzzyMatch(text, query string) bool {
	if query == "" {
		return true
	}

	textLower := strings.ToLower(text)
	queryLower := strings.ToLower(query)

	// Convert to runes for proper Unicode handling
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
func filterProjects(projects []dto.Project, query string) []dto.Project {
	if query == "" {
		return projects
	}

	filtered := make([]dto.Project, 0, len(projects))
	for _, project := range projects {
		if fuzzyMatch(project.Name, query) {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

// compareProjects returns comparison result for sorting projects.
func compareProjects(a, b dto.Project, sortColumn projectSortColumn, ascending bool) int {
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
	}

	if !ascending {
		cmp = -cmp
	}

	// Secondary sort by name if primary comparison is equal
	if cmp == 0 {
		cmp = strings.Compare(a.Name, b.Name)
	}

	return cmp
}

// compareContainers returns comparison result for sorting containers.
func compareContainers(a, b dto.Container, sortColumn containerSortColumn, ascending bool) int {
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
	}

	if !ascending {
		cmp = -cmp
	}

	// Secondary sort by name if primary comparison is equal
	if cmp == 0 {
		cmp = strings.Compare(a.Service, b.Service)
	}

	return cmp
}
