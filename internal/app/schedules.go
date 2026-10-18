package app

import (
	"time"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/modules/identity"
	"github.com/wisalgroup/labena/internal/platform/media"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/asynqx"
)

// registerSchedules is the central recurring-work policy. Modules own handlers
// and command factories; app owns timing, enablement and scheduler execution.
// Install after module registration. Calendar expressions use UTC.
func (a *application) registerSchedules() error {
	var tasks []asynqx.TaskSchedule
	if len(a.resources.Disks) > 0 {
		tasks = append(tasks,
			asynqx.TaskSchedule{
				Spec:    "*/5 * * * *",
				Task:    func() (*asynq.Task, error) { return asynqx.NewTask(media.RecoverTask, nil), nil },
				Options: []asynq.Option{asynq.Unique(5 * time.Minute), asynq.Timeout(2 * time.Minute)},
			},
		)
	}
	if a.cfg.Push.Enabled {
		tasks = append(tasks, asynqx.TaskSchedule{
			Spec:    "@every 1h",
			Task:    func() (*asynq.Task, error) { return asynqx.NewTask(notification.PruneSubscriptionsTask, nil), nil },
			Options: []asynq.Option{asynq.Unique(time.Hour), asynq.Timeout(time.Minute)},
		})
	}
	var commands []asynqx.CommandSchedule
	if a.cfg.Auth.Enabled {
		commands = append(commands, asynqx.CommandSchedule{Spec: "*/5 * * * *", Command: identity.PruneSessionsCommand})
		commands = append(commands, asynqx.CommandSchedule{Spec: "*/5 * * * *", Command: identity.PruneVerificationCommand})
	}
	return asynqx.RegisterSchedules(a.scheduler, a.cfg.Queue.Routing(), a.targets.ScheduledCommands, tasks, commands)
}
