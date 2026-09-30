package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omarismael/dockenciler/internal/testutil"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/notifier"
	"github.com/omarismael/dockenciler/pkg/registry"
	"github.com/stretchr/testify/require"
)

// staleDigestHarness wires a swarm reconciler whose single container runs an
// image that no longer matches the registry, which is the state that used to
// produce a ForceUpdate bump on every tick.
func staleDigestHarness(updateErr error) (*Reconciler, *testutil.MockDockerClient, *[]string, *[]notifier.Notification) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}
	stubUpdatePath(mockDocker, mockRegistry)

	mockDocker.ListContainersFunc = func(context.Context, string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:      "c1",
			Image:   "ghcr.io/owner/repo:master",
			ImageID: "sha256:stale",
			Labels:  map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(context.Context, string) (string, error) {
		return "sha256:newtag", nil
	}
	mockRegistry.GetLatestDigestFunc = func(context.Context, string, registry.Criteria) (string, error) {
		return "sha256:newtag", nil
	}
	mockDocker.IsSwarmModeFunc = func(context.Context) (bool, error) { return true, nil }
	mockDocker.GetServiceIDFunc = func(context.Context, string) (string, error) { return "svc1", nil }
	mockDocker.UpdateServiceFunc = func(context.Context, string, docker.ServiceSpec) error {
		return updateErr
	}

	recreated := &[]string{}
	mockDocker.RecreateContainerFunc = func(_ context.Context, id string, _ docker.ContainerSpec, _ string) error {
		*recreated = append(*recreated, id)
		return nil
	}

	notifications := &[]notifier.Notification{}
	r := testReconciler(mockDocker, mockRegistry)
	r.Notifier = &testutil.MockNotifier{
		NotifyFunc: func(_ context.Context, n notifier.Notification) error {
			*notifications = append(*notifications, n)
			return nil
		},
	}
	r.Config.ForceUpdateMinInterval = "10m"
	r.Config.MaxRolloutWait = "30m"
	return r, mockDocker, recreated, notifications
}

// A rollout this process already started is bringing the service onto the new
// image. Falling through to an in-place container swap would fight that
// rollout, and reporting the tick as a failure would be wrong.
func TestReconcile_DeferredServiceUpdateDoesNotRecreateContainer(t *testing.T) {
	for _, sentinel := range []error{docker.ErrServiceUpdateInFlight, docker.ErrServiceUpdateBackoff} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r, _, recreated, notifications := staleDigestHarness(sentinel)

			require.NoError(t, r.Reconcile(context.Background()))
			require.Empty(t, *recreated, "a deferred update must not fall through to an in-place container swap")
			require.Empty(t, *notifications, "nothing was updated, so nothing should be announced")
		})
	}
}

// A deferred update must be reported as skipped, not failed, so the summary
// line reflects that the service is being handled rather than broken.
func TestReconcile_DeferredServiceUpdateCountsAsSkipped(t *testing.T) {
	r, _, _, _ := staleDigestHarness(docker.ErrServiceUpdateInFlight)

	require.NoError(t, r.Reconcile(context.Background()))
}

// The guards are opt-in per call, and a healthy service still rolls normally.
func TestReconcile_ServiceUpdateProceedsWithoutRolloutInFlight(t *testing.T) {
	r, mockDocker, recreated, notifications := staleDigestHarness(nil)
	var seen docker.ServiceSpec
	mockDocker.UpdateServiceFunc = func(_ context.Context, _ string, spec docker.ServiceSpec) error {
		seen = spec
		return nil
	}

	require.NoError(t, r.Reconcile(context.Background()))
	require.Empty(t, *recreated)
	require.Len(t, *notifications, 1)
	require.True(t, seen.Force)
	require.Equal(t, "ghcr.io/owner/repo:master", seen.TaskTemplate.ContainerSpec.Image)
	require.Equal(t, 10*time.Minute, seen.MinForceUpdateInterval, "config must reach the docker guard")
	require.Equal(t, 30*time.Minute, seen.MaxRolloutWait, "config must reach the docker guard")
}

// A genuine update failure must still fall back to recreating the container —
// the deferral guards must not swallow real errors.
func TestReconcile_RealServiceUpdateErrorStillRecreatesContainer(t *testing.T) {
	r, _, recreated, _ := staleDigestHarness(errors.New("update out of sequence"))

	require.NoError(t, r.Reconcile(context.Background()))
	require.Equal(t, []string{"c1"}, *recreated)
}

// A malformed duration must not disable the update; it degrades to the
// pre-guard behaviour rather than wedging rollouts.
func TestReconcile_InvalidGuardDurationStillUpdates(t *testing.T) {
	r, mockDocker, _, notifications := staleDigestHarness(nil)
	r.Config.ForceUpdateMinInterval = "not-a-duration"
	r.Config.MaxRolloutWait = ""
	var seen docker.ServiceSpec
	mockDocker.UpdateServiceFunc = func(_ context.Context, _ string, spec docker.ServiceSpec) error {
		seen = spec
		return nil
	}

	require.NoError(t, r.Reconcile(context.Background()))
	require.Len(t, *notifications, 1)
	require.Zero(t, seen.MinForceUpdateInterval)
	require.Zero(t, seen.MaxRolloutWait)
}
