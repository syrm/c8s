package model

import (
	"testing"
)

func TestProjectCopy(t *testing.T) {
	t.Parallel()

	original := Project{
		ID:                "project-1",
		Name:              "test-project",
		CPUPercentage:     75.5,
		MemoryPercentage:  60.2,
		ContainersRunning: 3,
		ContainersCPU: map[ContainerID]float64{
			"c1": 25.0,
			"c2": 50.5,
		},
		ContainersMemory: map[ContainerID]float64{
			"c1": 30.0,
			"c2": 30.2,
		},
		ContainersState: map[ContainerID]string{
			"c1": StatusRunning,
			"c2": StatusRunning,
		},
	}

	copied := original.Copy()

	// Verify values match
	if copied.ID != original.ID {
		t.Errorf("Copy().ID = %q, want %q", copied.ID, original.ID)
	}
	if copied.Name != original.Name {
		t.Errorf("Copy().Name = %q, want %q", copied.Name, original.Name)
	}
	if copied.CPUPercentage != original.CPUPercentage {
		t.Errorf("Copy().CPUPercentage = %f, want %f", copied.CPUPercentage, original.CPUPercentage)
	}
	if copied.MemoryPercentage != original.MemoryPercentage {
		t.Errorf("Copy().MemoryPercentage = %f, want %f", copied.MemoryPercentage, original.MemoryPercentage)
	}
	if copied.ContainersRunning != original.ContainersRunning {
		t.Errorf("Copy().ContainersRunning = %d, want %d", copied.ContainersRunning, original.ContainersRunning)
	}

	// Verify deep copy — modifying copy should not affect original
	copied.ContainersCPU["c3"] = 99.0
	if _, exists := original.ContainersCPU["c3"]; exists {
		t.Error("Modifying copy's ContainersCPU affected original")
	}

	copied.ContainersMemory["c3"] = 99.0
	if _, exists := original.ContainersMemory["c3"]; exists {
		t.Error("Modifying copy's ContainersMemory affected original")
	}

	copied.ContainersState["c3"] = StatusExited
	if _, exists := original.ContainersState["c3"]; exists {
		t.Error("Modifying copy's ContainersState affected original")
	}
}

func TestProjectCopy_NilMaps(t *testing.T) {
	t.Parallel()

	original := Project{
		ID:   "project-nil",
		Name: "nil-maps",
	}

	copied := original.Copy()

	if copied.ID != original.ID {
		t.Errorf("Copy().ID = %q, want %q", copied.ID, original.ID)
	}
	// Nil maps should remain nil or empty after copy
	if len(copied.ContainersCPU) > 0 {
		t.Error("Copy() should not create non-empty ContainersCPU from nil")
	}
}

func TestContainerStatusConstants(t *testing.T) {
	t.Parallel()

	// Verify status constants are non-empty and distinct
	statuses := []string{
		StatusRunning, StatusExited, StatusPaused,
		StatusRestarting, StatusCreated, StatusRemoving,
	}

	seen := make(map[string]bool)
	for _, s := range statuses {
		if s == "" {
			t.Error("Status constant should not be empty")
		}
		if seen[s] {
			t.Errorf("Duplicate status constant: %q", s)
		}
		seen[s] = true
	}
}

func TestChannelTimeout(t *testing.T) {
	t.Parallel()

	if ChannelTimeout <= 0 {
		t.Errorf("ChannelTimeout = %v, should be positive", ChannelTimeout)
	}
}

func TestRequestDataInterface(t *testing.T) {
	t.Parallel()

	// Verify all request types implement RequestData
	var _ RequestData = &RequestProjectList{}
	var _ RequestData = &RequestContainerLog{}
	var _ RequestData = &RequestProject{}
	var _ RequestData = &RequestSetPendingAction{}
	var _ RequestData = &RequestStopLogCollection{}
}

func TestRequestProjectList(t *testing.T) {
	t.Parallel()

	req := &RequestProjectList{
		Response: make(chan []Project, 1),
	}

	// Send and receive
	go func() {
		req.Response <- []Project{{ID: "p1", Name: "test"}}
	}()

	result := <-req.Response
	if len(result) != 1 {
		t.Fatalf("Expected 1 project, got %d", len(result))
	}
	if result[0].ID != "p1" {
		t.Errorf("Project ID = %q, want %q", result[0].ID, "p1")
	}
}

func TestRequestContainerLog(t *testing.T) {
	t.Parallel()

	req := &RequestContainerLog{
		ContainerID: ContainerID("test-container"),
		Response:    make(chan Container, 1),
	}

	if req.ContainerID != "test-container" {
		t.Errorf("ContainerID = %q, want %q", req.ContainerID, "test-container")
	}

	go func() {
		req.Response <- Container{ID: "test-container", Status: StatusRunning}
	}()

	result := <-req.Response
	if result.ID != "test-container" {
		t.Errorf("Container ID = %q, want %q", result.ID, "test-container")
	}
}
