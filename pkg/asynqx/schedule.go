package asynqx

import (
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"

	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
)

// TaskSchedule builds one native task before any schedules are registered.
// Task factories must not enqueue work or perform external side effects.
type TaskSchedule struct {
	Spec    string
	Task    func() (*asynq.Task, error)
	Options []asynq.Option
}

// CommandSchedule refers to a command allowed by the supplied command task builder.
type CommandSchedule struct {
	Spec    string
	Command string
	Args    []string
	Options []asynq.Option
}

// ScheduleRegistrar is the native registration/rollback capability of asynq.Scheduler.
type ScheduleRegistrar interface {
	Register(string, *asynq.Task, ...asynq.Option) (string, error)
	Unregister(string) error
}

// CommandTasks builds a native task for an explicitly registered command.
type CommandTasks interface {
	Task(string, ...string) (*asynq.Task, error)
}

type schedule struct {
	spec    string
	task    *asynq.Task
	options []asynq.Option
}

// RegisterSchedules constructs and validates every definition before registering
// any. If native registration fails, earlier entries from this call are removed;
// rollback errors are preserved. Call only before starting the scheduler.
func RegisterSchedules(s ScheduleRegistrar, routing asynqmiddleware.Routing, commands CommandTasks, tasks []TaskSchedule, commandSchedules []CommandSchedule) error {
	if s == nil {
		return errors.New("tasks: scheduler required")
	}
	entries := make([]schedule, 0, len(tasks)+len(commandSchedules))
	prepare := func(spec string, task *asynq.Task, options []asynq.Option) error {
		if _, err := cron.ParseStandard(spec); err != nil {
			return fmt.Errorf("tasks: schedule %q: %w", spec, err)
		}
		if task == nil || task.Type() == "" {
			return errors.New("tasks: invalid scheduled task")
		}
		resolved, err := routing.EnqueueOptions(task.Type(), options)
		if err != nil {
			return err
		}
		entries = append(entries, schedule{spec, task, resolved})
		return nil
	}
	for i, definition := range tasks {
		task, err := buildScheduledTask(definition.Task)
		if err == nil {
			err = prepare(definition.Spec, task, definition.Options)
		}
		if err != nil {
			return fmt.Errorf("tasks: task schedule %d: %w", i+1, err)
		}
	}
	if len(commandSchedules) > 0 && commands == nil {
		return errors.New("tasks: scheduled commands are disabled")
	}
	for i, definition := range commandSchedules {
		task, err := commands.Task(definition.Command, definition.Args...)
		if err == nil {
			err = prepare(definition.Spec, task, definition.Options)
		}
		if err != nil {
			return fmt.Errorf("tasks: command schedule %d (%s): %w", i+1, definition.Command, err)
		}
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		id, err := s.Register(entry.spec, entry.task, entry.options...)
		if err != nil {
			failure := fmt.Errorf("tasks: register %s (%s): %w", entry.task.Type(), entry.spec, err)
			for i := len(ids) - 1; i >= 0; i-- {
				if err := s.Unregister(ids[i]); err != nil {
					failure = errors.Join(failure, fmt.Errorf("tasks: rollback schedule %s: %w", ids[i], err))
				}
			}
			return failure
		}
		ids = append(ids, id)
	}
	return nil
}

func buildScheduledTask(factory func() (*asynq.Task, error)) (*asynq.Task, error) {
	if factory == nil {
		return nil, errors.New("schedule: nil task factory")
	}
	return factory()
}
