// Package asynqx defines application integration contracts for Asynq.
package asynqx

import (
	"context"

	"github.com/hibiken/asynq"
)

// Client accepts native Asynq tasks and options. App injects a client with routing
// and telemetry middleware; a native *asynq.Client also satisfies this interface.
// Clients are borrowed by services. Their lifecycle belongs to app.
type Client interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

var _ Client = (*asynq.Client)(nil)
