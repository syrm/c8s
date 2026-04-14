package docker

import (
	"context"
	"testing"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/syrm/c8s/internal/model"
)

// TestStatusFromAction_TableDriven tests status conversion for all Docker events.
func TestStatusFromAction_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		action   events.Action
		expected string
	}{
		{
			name:     "start event",
			action:   events.ActionStart,
			expected: model.StatusRunning,
		},
		{
			name:     "stop event",
			action:   events.ActionStop,
			expected: model.StatusExited,
		},
		{
			name:     "die event",
			action:   events.ActionDie,
			expected: model.StatusExited,
		},
		{
			name:     "pause event",
			action:   events.ActionPause,
			expected: model.StatusPaused,
		},
		{
			name:     "unpause event",
			action:   events.ActionUnPause,
			expected: model.StatusRunning,
		},
		{
			name:     "restart event",
			action:   events.ActionRestart,
			expected: model.StatusRestarting,
		},
		{
			name:     "remove event",
			action:   events.ActionRemove,
			expected: model.StatusRemoving,
		},
		{
			name:     "create event",
			action:   events.ActionCreate,
			expected: model.StatusCreated,
		},
		{
			name:     "unknown event",
			action:   events.ActionExecCreate,
			expected: "", // Unknown events return empty string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := statusFromAction(tt.action)
			if result != tt.expected {
				t.Errorf("statusFromAction(%v) = %v, want %v", tt.action, result, tt.expected)
			}
		})
	}
}

// TestContainerSetStatusFromAction tests the SetStatusFromAction method.
func TestContainerSetStatusFromAction(t *testing.T) {
	tests := []struct {
		name     string
		action   events.Action
		expected string
	}{
		{
			name:     "container starts",
			action:   events.ActionStart,
			expected: model.StatusRunning,
		},
		{
			name:     "container stops",
			action:   events.ActionStop,
			expected: model.StatusExited,
		},
		{
			name:     "container pauses",
			action:   events.ActionPause,
			expected: model.StatusPaused,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{}
			c.SetStatusFromAction(tt.action)
			if c.Status != tt.expected {
				t.Errorf("SetStatusFromAction(%v) set status to %v, want %v", tt.action, c.Status, tt.expected)
			}
		})
	}
}

// TestContainerSetPendingAction tests the SetPendingAction method.
func TestContainerSetPendingAction(t *testing.T) {
	tests := []struct {
		name   string
		action string
		valid  bool
	}{
		{
			name:   "valid start action",
			action: "starting",
			valid:  true,
		},
		{
			name:   "valid stop action",
			action: "stopping",
			valid:  true,
		},
		{
			name:   "valid restart action",
			action: "restarting",
			valid:  true,
		},
		{
			name:   "valid remove action",
			action: "removing",
			valid:  true,
		},
		{
			name:   "empty action",
			action: "",
			valid:  true,
		},
		{
			name:   "invalid action",
			action: "invalid",
			valid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{}
			c.SetPendingAction(tt.action)

			if tt.valid {
				if c.PendingAction != tt.action {
					t.Errorf("SetPendingAction(%v) set PendingAction to %v, want %v", tt.action, c.PendingAction, tt.action)
				}
			} else {
				// Invalid actions should not change the pending action
				if c.PendingAction == tt.action {
					t.Errorf("SetPendingAction(%v) should not accept invalid actions", tt.action)
				}
			}
		})
	}
}

// TestContainerSnapshot tests the Snapshot method with various container states.
func TestContainerSnapshot_TableDriven(t *testing.T) {
	tests := []struct {
		name      string
		container *Container
		expected  model.Container
	}{
		{
			name: "running container",
			container: &Container{
				ID:               model.ContainerID("test-id"),
				Service:          "test-service",
				Name:             "test-name",
				Project:          model.ContainerProject{ID: model.ProjectID("test-project"), Name: "Test Project"},
				CPUPercentage:    50.5,
				MemoryPercentage: 75.2,
				Status:           model.StatusRunning,
				PendingAction:    "",
			},
			expected: model.Container{
				ID:               model.ContainerID("test-id"),
				Service:          "test-service",
				Name:             "test-name",
				Project:          model.ContainerProject{ID: model.ProjectID("test-project"), Name: "Test Project"},
				CPUPercentage:    50.5,
				MemoryPercentage: 75.2,
				Status:           model.StatusRunning,
				PendingAction:    "",
			},
		},
		{
			name: "container with pending action",
			container: &Container{
				ID:               model.ContainerID("test-id"),
				Service:          "test-service",
				Name:             "test-name",
				Project:          model.ContainerProject{ID: model.ProjectID("test-project"), Name: "Test Project"},
				CPUPercentage:    0,
				MemoryPercentage: 0,
				Status:           model.StatusExited,
				PendingAction:    "stopping",
			},
			expected: model.Container{
				ID:               model.ContainerID("test-id"),
				Service:          "test-service",
				Name:             "test-name",
				Project:          model.ContainerProject{ID: model.ProjectID("test-project"), Name: "Test Project"},
				CPUPercentage:    0,
				MemoryPercentage: 0,
				Status:           model.StatusExited,
				PendingAction:    "stopping",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := tt.container.Snapshot()

			if snapshot.ID != tt.expected.ID {
				t.Errorf("Snapshot ID = %v, want %v", snapshot.ID, tt.expected.ID)
			}
			if snapshot.Service != tt.expected.Service {
				t.Errorf("Snapshot Service = %v, want %v", snapshot.Service, tt.expected.Service)
			}
			if snapshot.Name != tt.expected.Name {
				t.Errorf("Snapshot Name = %v, want %v", snapshot.Name, tt.expected.Name)
			}
			if snapshot.Status != tt.expected.Status {
				t.Errorf("Snapshot Status = %v, want %v", snapshot.Status, tt.expected.Status)
			}
			if snapshot.PendingAction != tt.expected.PendingAction {
				t.Errorf("Snapshot PendingAction = %v, want %v", snapshot.PendingAction, tt.expected.PendingAction)
			}
		})
	}
}

// TestNewContainer_TableDriven tests container creation with different Docker API responses.
func TestNewContainer_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		summary apiContainer.Summary
		action  events.Action
		project model.ContainerProject
		wantErr bool
	}{
		{
			name: "valid running container",
			summary: apiContainer.Summary{
				ID:     "container-id",
				Names:  []string{"/test-container"},
				State:  "running",
				Status: "Up 2 hours",
				Labels: map[string]string{
					"com.docker.compose.project":             "test-project",
					"com.docker.compose.project.working_dir": "/test/dir",
					"com.docker.compose.service":             "test-service",
				},
			},
			action: events.ActionStart,
			project: model.ContainerProject{
				ID:   model.ProjectID("/test/dir"),
				Name: "test-project",
			},
			wantErr: false,
		},
		{
			name: "container without compose labels",
			summary: apiContainer.Summary{
				ID:     "container-id",
				Names:  []string{"/test-container"},
				State:  "running",
				Status: "Up 2 hours",
				Labels: map[string]string{},
			},
			action:  events.ActionStart,
			project: model.ContainerProject{},
			wantErr: true,
		},
		{
			name: "container with invalid project ID",
			summary: apiContainer.Summary{
				ID:     "container-id",
				Names:  []string{"/test-container"},
				State:  "running",
				Status: "Up 2 hours",
				Labels: map[string]string{
					"com.docker.compose.project":             "test-project",
					"com.docker.compose.project.working_dir": "",
					"com.docker.compose.service":             "test-service",
				},
			},
			action:  events.ActionStart,
			project: model.ContainerProject{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c, err := NewContainer(ctx, tt.summary, tt.action, tt.project)

			if err != nil {
				t.Errorf("NewContainer() unexpected error: %v", err)
			}
			if c == nil {
				t.Errorf("NewContainer() returned nil container")
			}
		})
	}
}
