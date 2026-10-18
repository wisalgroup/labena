package middleware

import (
	"github.com/hibiken/asynq"
	"github.com/wisalgroup/labena/pkg/logging"
	"log/slog"
)

// LogScheduledTask observes native scheduler publications, which bypass the
// application's enqueue chain. No producer context is attached at registration.
func LogScheduledTask(log *slog.Logger) func(*asynq.TaskInfo, error) {
	return func(info *asynq.TaskInfo, err error) {
		if err != nil {
			log.Error("scheduled task enqueue failed", "error_category", logging.ErrorCategory(err))
			return
		}
		log.Info("scheduled task enqueued", "task_id", info.ID, "task_name", info.Type, "queue", info.Queue)
	}
}
