package docker

import (
	"context"
	"fmt"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/rs/zerolog/log"
)

type DockerManager struct {
	client        *client.Client
	containerName string
	containerID   string

	// Protects access to 'containerStatus'
	statusLock      sync.RWMutex
	containerStatus string
}

// NewDockerManager creates and returns a new DockerManager.
// It initializes the Docker client and sets up the container name.
func NewDockerManager(containerName string) (*DockerManager, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}

	return &DockerManager{
		client:          cli,
		containerName:   containerName,
		containerStatus: "unknown",
	}, nil
}

// GetStatus returns the current container status in a thread-safe manner.
func (dm *DockerManager) GetStatus() string {
	dm.statusLock.RLock()
	defer dm.statusLock.RUnlock()
	return dm.containerStatus
}

// setStatus updates the container status in a thread-safe manner.
func (dm *DockerManager) setStatus(status string) {
	dm.statusLock.Lock()
	defer dm.statusLock.Unlock()
	dm.containerStatus = status
}

// UpdateContainerInfo does a one-time lookup of the container's state
// and updates dm.containerStatus accordingly.
func (dm *DockerManager) UpdateContainerInfo(ctx context.Context) (bool, error) {
	mcFilter := filters.NewArgs()
	mcFilter.Add("name", dm.containerName)
	containers, err := dm.client.ContainerList(ctx, container.ListOptions{All: true, Filters: mcFilter})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

        for _, c := range containers {
		for _, name := range c.Names { // should succeed
			if name == "/"+dm.containerName {
				dm.containerID = c.ID
				dm.setStatus(c.State)
				return true, nil
			}
		}
	}

	dm.setStatus("notCreated")
	return false, nil
}

// Start starts the container if it is not already running.
func (dm *DockerManager) Start(ctx context.Context) error {
	//dm.UpdateContainerInfo(ctx)
	status := dm.GetStatus()
	if status != "running" {
		if err := dm.client.ContainerStart(ctx, dm.containerID, container.StartOptions{}); err != nil {
			return fmt.Errorf("failed to start container %s: %w", dm.containerName, err)
		}

		log.Info().Msg("Container started.")
		dm.setStatus("running")
	}
	return nil
}

// WatchContainerState listens for Docker events related to our container
// and updates the internal status when it changes.
// does not detect exit events but they are detected on demand.
/*func (dm *DockerManager) WatchContainerState(ctx context.Context) error {
	// Create a filter to only receive events for our container name
	eventFilter := filters.NewArgs()
	eventFilter.Add("container", dm.containerName)

	options := events.ListOptions{
		Filters: eventFilter,
	}

	eventCh, errCh := dm.client.Events(ctx, options)

	for {
		select {
		case <-ctx.Done():
			log.Warn().Msg("Context canceled; stopping WatchContainerState.")
			return ctx.Err()

		case err := <-errCh:
			// The error channel signals if the Events stream fails
			if err != nil {
				return fmt.Errorf("error while watching Docker events: %w", err)
			}
			return nil // if err is nil, the event stream ended gracefully

		case event := <-eventCh:
			log.Debug().Str("event", string(event.Action))
			if event.Action == "start" || event.Action == "stop" {
				log.Debug().Str("status", string(event.Action)).Msgf("Container status updated.")
				dm.setStatus(string(event.Action))
			}
		}
	}
}
*/
