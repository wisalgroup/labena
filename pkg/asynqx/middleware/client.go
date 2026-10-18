// Package middleware composes application policies around native Asynq enqueue calls.
package middleware

import (
	"context"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/pkg/database"
)

type EnqueueFunc func(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)

func (f EnqueueFunc) EnqueueContext(ctx context.Context, t *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return f(ctx, t, opts...)
}

type Middleware func(EnqueueFunc) EnqueueFunc

// Chain applies middleware in listed order, outermost first. Configure once at startup.
func Chain(next EnqueueFunc, middleware ...Middleware) EnqueueFunc {
	for i := len(middleware) - 1; i >= 0; i-- {
		next = middleware[i](next)
	}
	return next
}

// RequireNoTransaction rejects framework SQL transaction scopes and canceled
// contexts before enqueueing. Install outermost so no client middleware performs
// effects first. Native clients bypass this policy; it does not provide an outbox.
func RequireNoTransaction() Middleware {
	return func(next EnqueueFunc) EnqueueFunc {
		return func(ctx context.Context, task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
			if err := database.RequireNoTransaction(ctx); err != nil {
				return nil, err
			}
			return next(ctx, task, options...)
		}
	}
}
