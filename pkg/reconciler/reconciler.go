package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/omarismael/dockenciler/pkg/config"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/notifier"
	"github.com/omarismael/dockenciler/pkg/registry"
)

// Reconciler struct holds dependencies for reconciliation
// In a real implementation, these would be concrete types
// For this interface design spike, we use interface types directly
// to demonstrate dependency injection
type Reconciler struct {
	DockerClient interface {
		ListContainers(ctx context.Context, labelFilter string) ([]docker.Container, error)
		InspectContainer(ctx context.Context, id string) (docker.ContainerSpec, error)
		PullImage(ctx context.Context, imageRef string) error
		RecreateContainer(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error
		UpdateService(ctx context.Context, serviceID string, spec docker.ServiceSpec) error
		GetImageDigest(ctx context.Context, imageRef string) (string, error)
		IsSwarmMode(ctx context.Context) (bool, error)
		GetServiceID(ctx context.Context, containerID string) (string, error)
		Authenticate(ctx context.Context, username, password, registryHost string) error
	}
	Registry interface {
		GetLatestDigest(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error)
		GetImageVersion(ctx context.Context, imageRef string) (string, error)
		GetAuth(ctx context.Context) (registry.Auth, error)
		InvalidateCache()
	}
	Notifier interface {
		Notify(ctx context.Context, n notifier.Notification) error
	}
	Config   *config.Config
	Location *time.Location
}

func convertConfigCriteriaToRegistryCriteria(configCriteria config.Criteria) registry.Criteria {
	return registry.Criteria{
		Version: configCriteria.Version,
		Regex:   configCriteria.Regex,
		Digest:  configCriteria.Digest,
	}
}

func (r *Reconciler) Reconcile(ctx context.Context) error {
	// List containers using the configured label filter
	containers, err := r.DockerClient.ListContainers(ctx, r.Config.Docker.LabelFilter)
	if err != nil {
		slog.Error("Failed to list containers", "error", err)
		return fmt.Errorf("failed to list containers: %w", err)
	}

	slog.Info("Starting reconciliation", "container_count", len(containers))

	var (
		checked   int
		updated   int
		upToDate  int
		skipped   int
		failed    int
		skipLog   []string
	)

	for _, container := range containers {
		// Skip self-update: containers with dockenciler.instance=true label
		if container.Labels != nil && container.Labels["dockenciler.instance"] == "true" {
			slog.Info("Skipping self-update container", "container_id", container.ID)
			skipped++
			skipLog = append(skipLog, fmt.Sprintf("%s (self-update)", shortID(container.ID)))
			continue
		}

		// Skip containers in exclusions list
		isExcluded := false
		if r.Config.Exclusions != nil {
			for _, exclusion := range r.Config.Exclusions {
				if exclusion == container.ID {
					slog.Info("Skipping excluded container", "container_id", container.ID, "exclusion", exclusion)
					isExcluded = true
					break
				}
			}
		}
		if isExcluded {
			skipped++
			skipLog = append(skipLog, fmt.Sprintf("%s (excluded)", shortID(container.ID)))
			continue
		}

		slog.Info("Checking container", "container_id", container.ID, "image", container.Image)

		// Get the current image digest
		currentDigest, err := r.DockerClient.GetImageDigest(ctx, container.Image)
		if err != nil {
			slog.Error("Failed to get current image digest", "container_id", container.ID, "image", container.Image, "error", err)
			failed++
			continue // Continue to next container
		}

		// Debug log for current digest
		slog.Debug("Got current image digest", "container_id", container.ID, "image", container.Image, "digest", currentDigest)

		// Get the latest digest from registry using configured criteria
		criteria := convertConfigCriteriaToRegistryCriteria(r.Config.Criteria)
		latestDigest, err := r.Registry.GetLatestDigest(ctx, container.Image, criteria)
		if err != nil {
			slog.Error("Failed to get latest digest", "container_id", container.ID, "image", container.Image, "error", err)
			failed++
			continue // Continue to next container
		}

		// Debug log for latest digest
		slog.Debug("Got latest registry digest", "container_id", container.ID, "image", container.Image, "digest", latestDigest)

		checked++

		// Compare digests
		if currentDigest == latestDigest {
			slog.Info("Container is up to date", "container_id", container.ID, "image", container.Image, "digest", currentDigest)
			upToDate++
			continue
		}

		// Digests differ, update required
		slog.Info("Update required for container", "container_id", container.ID, "image", container.Image, "current_digest", currentDigest, "latest_digest", latestDigest)

		// Check if dry-run mode is enabled
		if r.Config.DryRun {
			slog.Info("Dry-run: would update container", "container_id", container.ID, "from_digest", currentDigest, "to_digest", latestDigest)
			continue
		}

		// Get auth credentials from registry
		auth, err := r.Registry.GetAuth(ctx)
		if err != nil {
			slog.Error("Failed to get auth from registry", "container_id", container.ID, "image", container.Image, "error", err)
			failed++
			continue // Continue to next container
		}

		// Authenticate with Docker daemon
		if err := r.DockerClient.Authenticate(ctx, auth.Username, auth.Password, auth.RegistryHost); err != nil {
			slog.Error("Failed to authenticate with Docker daemon", "container_id", container.ID, "image", container.Image, "error", err)
			failed++
			continue // Continue to next container
		}

		// Strip digest pin from image reference to ensure we pull the latest tag
		imageRef := container.Image
		if idx := strings.Index(imageRef, "@"); idx != -1 {
			imageRef = imageRef[:idx]
		}

		// Pull the new image
		if err := r.DockerClient.PullImage(ctx, imageRef); err != nil {
			// Check if error is related to authorization or credentials
			if strings.Contains(err.Error(), "authorization failed") || strings.Contains(err.Error(), "no basic auth credentials") {
				slog.Warn("Pull failed with auth error, invalidating cache and retrying", "container_id", container.ID, "image", container.Image, "error", err)
				
				// Invalidate cache
				r.Registry.InvalidateCache()
				
				// Re-fetch auth
				auth2, err2 := r.Registry.GetAuth(ctx)
				if err2 != nil {
					slog.Error("Failed to re-fetch auth from registry", "container_id", container.ID, "image", container.Image, "error", err2)
					failed++
					continue // Continue to next container
				}
				
				// Re-authenticate
				if err := r.DockerClient.Authenticate(ctx, auth2.Username, auth2.Password, auth2.RegistryHost); err != nil {
					slog.Error("Failed to re-authenticate with Docker daemon", "container_id", container.ID, "image", container.Image, "error", err)
					failed++
					continue // Continue to next container
				}
				
				// Retry pull once
				if err := r.DockerClient.PullImage(ctx, imageRef); err != nil {
					slog.Error("Failed to pull image after retry", "container_id", container.ID, "image", container.Image, "error", err)
					failed++
					continue // Continue to next container
				}
				
				slog.Info("Pull succeeded after retry", "container_id", container.ID, "image", container.Image)
			} else {
				slog.Error("Failed to pull image", "container_id", container.ID, "image", container.Image, "error", err)
				failed++
				continue // Continue to next container
			}
		}

		// Inspect container to get full spec
		spec, err := r.DockerClient.InspectContainer(ctx, container.ID)
		if err != nil {
			slog.Error("Failed to inspect container", "container_id", container.ID, "image", container.Image, "error", err)
			failed++
			continue // Continue to next container
		}

		// Check if we're in Swarm mode and if this container is managed by a service
		isSwarm, err := r.DockerClient.IsSwarmMode(ctx)
		if err != nil {
			slog.Error("Failed to check swarm mode", "container_id", container.ID, "image", container.Image, "error", err)
			// Continue with recreation if we can't determine swarm status
			isSwarm = false
		}

		var containerUpdated bool
		if isSwarm {
			// Try to get the service ID for this container
			serviceID, err := r.DockerClient.GetServiceID(ctx, container.ID)
			if err == nil && serviceID != "" {
				// Use service update for rolling update
				serviceSpec := docker.ServiceSpec{}
				serviceSpec.TaskTemplate.ContainerSpec.Image = container.Image
				slog.Info("Updating service for rolling update", "container_id", container.ID, "service_id", serviceID)
				if err := r.DockerClient.UpdateService(ctx, serviceID, serviceSpec); err != nil {
					slog.Error("Failed to update service", "container_id", container.ID, "service_id", serviceID, "image", container.Image, "error", err)
					// Fall back to container recreation on error
				} else {
					containerUpdated = true
				}
			}
		}

		// If not in swarm mode, no service ID found, or service update failed, recreate container
		if !containerUpdated {
			if err := r.DockerClient.RecreateContainer(ctx, container.ID, spec, container.Image); err != nil {
				// Check if the error is because the container is managed by swarm
				if err == docker.ErrContainerManagedBySwarm {
					slog.Info("Container is managed by swarm, attempting service update", "container_id", container.ID)
					// Try to get service ID and update service as fallback
					serviceID, err := r.DockerClient.GetServiceID(ctx, container.ID)
					if err == nil && serviceID != "" {
						slog.Info("Updating service for swarm-managed container", "container_id", container.ID, "service_id", serviceID)
						serviceSpec := docker.ServiceSpec{}
						serviceSpec.TaskTemplate.ContainerSpec.Image = container.Image
						if err := r.DockerClient.UpdateService(ctx, serviceID, serviceSpec); err != nil {
							slog.Error("Failed to update service", "container_id", container.ID, "service_id", serviceID, "image", container.Image, "error", err)
							failed++
							continue // Continue to next container
						}
					} else {
						slog.Error("Failed to get service ID for swarm-managed container", "container_id", container.ID, "image", container.Image, "error", err)
						failed++
						continue // Continue to next container
					}
				} else {
					slog.Error("Failed to recreate container", "container_id", container.ID, "image", container.Image, "error", err)
					failed++
					continue // Continue to next container
				}
			}
		}

		// Notify about the update
		notification := notifier.Notification{
			Subject:     fmt.Sprintf("Container %s updated", container.ID),
			Body:        fmt.Sprintf("Container %s was updated from digest %s to %s", container.ID, currentDigest, latestDigest),
			Level:       "info",
			ContainerID: container.ID,
			Image:       container.Image,
			OldDigest:   currentDigest,
			NewDigest:   latestDigest,
			Timestamp:   time.Now(),
			Location:    r.Location,
		}
		if err := r.Notifier.Notify(ctx, notification); err != nil {
			slog.Error("Failed to send notification", "container_id", container.ID, "image", container.Image, "error", err)
			// Continue even if notification fails
		}

		slog.Info("Container updated successfully", "container_id", container.ID, "image", container.Image)
		updated++
	}

	// Print reconciliation summary
	slog.Info("Reconciliation completed",
		"total", len(containers),
		"checked", checked,
		"up_to_date", upToDate,
		"updated", updated,
		"skipped", skipped,
		"failed", failed,
	)
	if len(skipLog) > 0 {
		slog.Info("Skipped containers", "containers", fmt.Sprintf("[%s]", joinStrings(skipLog, ", ")))
	}
	return nil
}

// shortID returns the first 12 characters of a container ID for compact display.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
