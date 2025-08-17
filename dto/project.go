package dto

type ProjectID string

type Project struct {
	ID                    ProjectID
	Name                  string
	CPUPercentage         float64
	MemoryUsagePercentage float64
	ContainersRunning     int
	ContainersTotal       int
}
