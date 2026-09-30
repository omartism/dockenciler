package docker

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/stretchr/testify/require"
)

// guardService is a minimal inspectable service carrying the ID the guard
// keys its backoff on.
func guardService(updateStatus *swarm.UpdateStatus) swarm.Service {
	return swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 42}},
		Spec: swarm.ServiceSpec{
			Mode:         swarm.ServiceMode{Global: &swarm.GlobalService{}},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "repo:master"}},
		},
		UpdateStatus: updateStatus,
	}
}

func forceImageSpec() ServiceSpec {
	spec := ServiceSpec{Force: true}
	spec.TaskTemplate.ContainerSpec.Image = "repo:master"
	return spec
}

// A ForceUpdate bump restarts the rollout. When the reconcile interval is
// shorter than the rollout's own delay+monitor window, bumping on every tick
// means no task ever reaches the new image: the service churns tasks forever
// and a task sits on a stale digest. A rollout already in flight must be left
// to finish instead of being superseded.
func TestUpdateService_InFlightRolloutBlocksSecondBump(t *testing.T) {
	started := time.Now().Add(-10 * time.Second)
	inspectCount := 0
	mockClient := &mockDockerClient{
		ServiceInspectWithRawFunc: func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error) {
			inspectCount++
			return guardService(&swarm.UpdateStatus{
				State:     swarm.UpdateStateUpdating,
				StartedAt: &started,
			}), nil, nil
		},
	}
	updateCalled := false
	mockClient.ServiceUpdateFunc = func(context.Context, string, swarm.Version, swarm.ServiceSpec, types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
		updateCalled = true
		return swarm.ServiceUpdateResponse{}, nil
	}

	client := &DockerClientImpl{client: mockClient}
	spec := forceImageSpec()
	spec.MaxRolloutWait = 30 * time.Minute

	err := client.UpdateService(context.Background(), "svc1", spec)
	require.ErrorIs(t, err, ErrServiceUpdateInFlight)
	require.False(t, updateCalled, "a bump during an in-flight rollout restarts it and must not be sent")
}

// The guard must not wedge a service whose rollout is genuinely stuck: once
// the wait exceeds MaxRolloutWait the bump is allowed through.
func TestUpdateService_StuckRolloutIsSupersededAfterMaxWait(t *testing.T) {
	started := time.Now().Add(-45 * time.Minute)
	mockClient := &mockDockerClient{
		ServiceInspectWithRawFunc: func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error) {
			return guardService(&swarm.UpdateStatus{
				State:     swarm.UpdateStateUpdating,
				StartedAt: &started,
			}), nil, nil
		},
	}
	updateCalled := false
	mockClient.ServiceUpdateFunc = func(context.Context, string, swarm.Version, swarm.ServiceSpec, types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
		updateCalled = true
		return swarm.ServiceUpdateResponse{}, nil
	}

	client := &DockerClientImpl{client: mockClient}
	spec := forceImageSpec()
	spec.MaxRolloutWait = 30 * time.Minute

	require.NoError(t, client.UpdateService(context.Background(), "svc1", spec))
	require.True(t, updateCalled, "a rollout past MaxRolloutWait must be superseded, not waited on forever")
}

// The backoff is the coarse floor that applies even when the update status is
// not yet visible: a second bump inside the interval is refused.
func TestUpdateService_BackoffBlocksRapidRebump(t *testing.T) {
	mockClient := &mockDockerClient{
		ServiceInspectWithRawFunc: func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error) {
			return guardService(nil), nil, nil
		},
	}
	bumpCount := 0
	mockClient.ServiceUpdateFunc = func(context.Context, string, swarm.Version, swarm.ServiceSpec, types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
		bumpCount++
		return swarm.ServiceUpdateResponse{}, nil
	}

	client := &DockerClientImpl{client: mockClient}
	spec := forceImageSpec()
	spec.MinForceUpdateInterval = 10 * time.Minute

	require.NoError(t, client.UpdateService(context.Background(), "svc1", spec))
	require.Equal(t, 1, bumpCount)

	err := client.UpdateService(context.Background(), "svc1", spec)
	require.ErrorIs(t, err, ErrServiceUpdateBackoff)
	require.Equal(t, 1, bumpCount, "the second bump inside the backoff window must not be sent")
}

// The backoff is per service: one service being bumped must not delay another.
func TestUpdateService_BackoffIsPerService(t *testing.T) {
	mockClient := &mockDockerClient{
		ServiceInspectWithRawFunc: func(_ context.Context, id string, _ types.ServiceInspectOptions) (swarm.Service, []byte, error) {
			s := guardService(nil)
			s.ID = id
			return s, nil, nil
		},
	}
	bumped := map[string]int{}
	mockClient.ServiceUpdateFunc = func(_ context.Context, id string, _ swarm.Version, _ swarm.ServiceSpec, _ types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
		bumped[id]++
		return swarm.ServiceUpdateResponse{}, nil
	}

	client := &DockerClientImpl{client: mockClient}
	spec := forceImageSpec()
	spec.MinForceUpdateInterval = 10 * time.Minute

	require.NoError(t, client.UpdateService(context.Background(), "svc1", spec))
	require.NoError(t, client.UpdateService(context.Background(), "svc2", spec))
	require.Equal(t, map[string]int{"svc1": 1, "svc2": 1}, bumped)
}

// A non-forced spec change is a real image change, not a rebuild behind an
// unchanged tag, so the guards must not apply to it.
func TestUpdateService_NonForceUpdateIsNotGuarded(t *testing.T) {
	started := time.Now()
	inspectCount := 0
	mockClient := &mockDockerClient{
		ServiceInspectWithRawFunc: func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error) {
			inspectCount++
			return guardService(&swarm.UpdateStatus{
				State:     swarm.UpdateStateUpdating,
				StartedAt: &started,
			}), nil, nil
		},
	}
	updateCalled := false
	mockClient.ServiceUpdateFunc = func(context.Context, string, swarm.Version, swarm.ServiceSpec, types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
		updateCalled = true
		return swarm.ServiceUpdateResponse{}, nil
	}

	client := &DockerClientImpl{client: mockClient}
	spec := ServiceSpec{}
	spec.TaskTemplate.ContainerSpec.Image = "repo:v2"
	spec.MinForceUpdateInterval = 10 * time.Minute
	spec.MaxRolloutWait = 30 * time.Minute

	require.NoError(t, client.UpdateService(context.Background(), "svc1", spec))
	require.True(t, updateCalled)
	require.Equal(t, 1, inspectCount, "guards must not add an extra inspect for a non-forced update")
}

// An unreadable update status is not a reason to skip the update; the backoff
// still bounds how often that can happen.
func TestUpdateService_FailedStatusReadDoesNotBlockUpdate(t *testing.T) {
	calls := 0
	mockClient := &mockDockerClient{
		ServiceInspectWithRawFunc: func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error) {
			calls++
			if calls > 1 {
				return swarm.Service{}, nil, context.DeadlineExceeded
			}
			return guardService(nil), nil, nil
		},
	}
	updateCalled := false
	mockClient.ServiceUpdateFunc = func(context.Context, string, swarm.Version, swarm.ServiceSpec, types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
		updateCalled = true
		return swarm.ServiceUpdateResponse{}, nil
	}

	client := &DockerClientImpl{client: mockClient}
	spec := forceImageSpec()
	spec.MaxRolloutWait = 30 * time.Minute

	require.NoError(t, client.UpdateService(context.Background(), "svc1", spec))
	require.True(t, updateCalled, "a failed status read must not wedge updates")
}

// A rollout whose start time Swarm did not report cannot be timed out, so it
// must not block forever: the bump goes through and the backoff bounds how
// often that can repeat.
func TestUpdateService_UntimedRolloutDoesNotWedgeUpdates(t *testing.T) {
	mockClient := &mockDockerClient{
		ServiceInspectWithRawFunc: func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error) {
			return guardService(&swarm.UpdateStatus{State: swarm.UpdateStateUpdating}), nil, nil
		},
	}
	updateCalled := false
	mockClient.ServiceUpdateFunc = func(context.Context, string, swarm.Version, swarm.ServiceSpec, types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
		updateCalled = true
		return swarm.ServiceUpdateResponse{}, nil
	}

	client := &DockerClientImpl{client: mockClient}
	spec := forceImageSpec()
	spec.MaxRolloutWait = 30 * time.Minute

	require.NoError(t, client.UpdateService(context.Background(), "svc1", spec))
	require.True(t, updateCalled)
}

func TestGetServiceUpdateState(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	tests := []struct {
		name         string
		updateState  *swarm.UpdateStatus
		wantInFlight bool
	}{
		{name: "no update status", updateState: nil},
		{name: "completed", updateState: &swarm.UpdateStatus{State: swarm.UpdateStateCompleted}},
		{name: "updating", updateState: &swarm.UpdateStatus{State: swarm.UpdateStateUpdating, StartedAt: &started}, wantInFlight: true},
		{name: "rollback started", updateState: &swarm.UpdateStatus{State: swarm.UpdateStateRollbackStarted, StartedAt: &started}, wantInFlight: true},
		{name: "rollback completed", updateState: &swarm.UpdateStatus{State: swarm.UpdateStateRollbackCompleted}},
		{name: "paused", updateState: &swarm.UpdateStatus{State: swarm.UpdateStatePaused}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDockerClient{
				ServiceInspectWithRawFunc: func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error) {
					return guardService(tt.updateState), nil, nil
				},
			}
			state, err := (&DockerClientImpl{client: mockClient}).GetServiceUpdateState(context.Background(), "svc1")
			require.NoError(t, err)
			require.Equal(t, tt.wantInFlight, state.InFlight)
			if tt.updateState != nil && tt.updateState.StartedAt != nil {
				require.Equal(t, *tt.updateState.StartedAt, state.StartedAt)
			}
		})
	}
}
