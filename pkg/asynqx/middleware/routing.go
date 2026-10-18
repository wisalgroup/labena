package middleware

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/hibiken/asynq"
)

// Routing lists available queues and optional exact task-name destinations.
// Precedence: explicit enqueue option, task route, default queue. Routing overrides
// constructor Queue options; all other native constructor options are preserved.
type Routing struct {
	Default string
	Queues  map[string]int
	Tasks   map[string]string
}

func (r Routing) Validate() error {
	if r.Queues[r.Default] <= 0 {
		return errors.New("tasks: default queue is not configured")
	}
	for task, destination := range r.Tasks {
		if strings.TrimSpace(task) == "" || r.Queues[destination] <= 0 {
			return fmt.Errorf("tasks: invalid route for %q", task)
		}
	}
	return nil
}

// EnqueueOptions prepends configured routing while preserving explicit overrides.
// The returned slice is independent of the caller's options slice.
func (r Routing) EnqueueOptions(name string, options []asynq.Option) ([]asynq.Option, error) {
	destination := r.Default
	if routed, ok := r.Tasks[name]; ok {
		destination = routed
	}
	result := make([]asynq.Option, 0, len(options)+1)
	result = append(result, asynq.Queue(destination))
	for _, option := range options {
		if option == nil {
			return nil, errors.New("tasks: nil enqueue option")
		}
		if option.Type() == asynq.QueueOpt {
			destination = option.Value().(string)
		}
		result = append(result, option)
	}
	if r.Queues[destination] <= 0 {
		return nil, fmt.Errorf("tasks: queue %q is not configured", destination)
	}
	return result, nil
}

// Route snapshots configuration and applies queue policy to every enqueue.
func Route(r Routing) Middleware {
	r.Queues = maps.Clone(r.Queues)
	r.Tasks = maps.Clone(r.Tasks)
	return func(next EnqueueFunc) EnqueueFunc {
		return func(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
			if task == nil {
				return nil, errors.New("queue: nil task")
			}
			opts, err := r.EnqueueOptions(task.Type(), opts)
			if err != nil {
				return nil, err
			}
			return next(ctx, task, opts...)
		}
	}
}
