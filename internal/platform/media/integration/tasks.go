package integration

import (
	"context"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/platform/media"
)

func registerTasks(mux *asynq.ServeMux, svc *media.Library) {
	mux.HandleFunc(media.RecoverTask, func(ctx context.Context, _ *asynq.Task) error { _, err := svc.Recover(ctx); return err })
}
