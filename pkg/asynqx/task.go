package asynqx

import "github.com/hibiken/asynq"

// NewTask creates a native task with headers ready for client trace propagation.
// Native options and payload ownership follow asynq.NewTaskWithHeaders. No trace
// context is captured until enqueue. Construct a fresh task for each enqueue;
// client middleware mutates its headers, so do not enqueue it concurrently.
func NewTask(typename string, payload []byte, opts ...asynq.Option) *asynq.Task {
	return asynq.NewTaskWithHeaders(typename, payload, make(map[string]string), opts...)
}
