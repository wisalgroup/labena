package integration

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/platform/notification"
)

const subscriptionMaxAge = 90 * 24 * time.Hour

func registerTasks(mux *asynq.ServeMux, notifier *notification.Notifier) {
	mux.HandleFunc(notification.PruneSubscriptionsTask, func(ctx context.Context, t *asynq.Task) error {
		if t.Type() != notification.PruneSubscriptionsTask || len(t.Payload()) != 0 {
			return errors.Join(errors.New("invalid subscription pruning task"), asynq.SkipRetry)
		}
		_, err := notifier.PruneSubscriptions(ctx, time.Now().Add(-subscriptionMaxAge))
		return err
	})
	mux.HandleFunc(notification.DispatchTask, func(ctx context.Context, _ *asynq.Task) error { _, err := notifier.Dispatch(ctx); return err })
	mux.HandleFunc(notification.DeliveryTask, func(ctx context.Context, t *asynq.Task) error {
		var id uuid.UUID
		if err := json.Unmarshal(t.Payload(), &id); err != nil || id == uuid.Nil {
			return errors.Join(errors.New("invalid delivery ID"), asynq.SkipRetry)
		}
		return notifier.Deliver(ctx, id)
	})
}
