package asynqx

import (
	"context"
	"errors"
	"fmt"
	"github.com/hibiken/asynq"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
	"testing"
	"testing/synctest"
)

func TestWorkerCallsJoinBeforeDependencyCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		calls := &workerCalls{next: asynq.HandlerFunc(func(context.Context, *asynq.Task) error { close(entered); <-release; return nil })}
		go func() { _ = calls.ProcessTask(t.Context(), asynq.NewTask("test", nil)) }()
		<-entered
		go func() { calls.closeAndWait(); close(done) }()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("returned while handler still using dependencies")
		default:
		}
		if err := calls.ProcessTask(t.Context(), asynq.NewTask("late", nil)); !errors.Is(err, context.Canceled) {
			t.Fatal("admitted late handler", err)
		}
		close(release)
		<-done
	})
}

type schedulerStub struct {
	registered    []string
	removed       []string
	failAt        int
	rollbackError error
	destinations  []string
}

func (s *schedulerStub) Register(spec string, task *asynq.Task, options ...asynq.Option) (string, error) {
	s.registered = append(s.registered, task.Type())
	if len(s.registered) == s.failAt {
		return "", errors.New("registration failed")
	}
	for _, o := range options {
		if o.Type() == asynq.QueueOpt {
			s.destinations = append(s.destinations, o.Value().(string))
		}
	}
	return fmt.Sprint(len(s.registered)), nil
}

func (s *schedulerStub) Unregister(id string) error {
	s.removed = append(s.removed, id)
	return s.rollbackError
}

type commandStub struct{}

func (commandStub) Task(name string, args ...string) (*asynq.Task, error) {
	if name != "allowed" {
		return nil, errors.New("unregistered command")
	}
	return asynq.NewTask("app.command", []byte(`{}`)), nil
}

func scheduledTask() (*asynq.Task, error) { return asynq.NewTask("task", nil), nil }

func TestSchedulePreflightAndRollback(t *testing.T) {
	routing := asynqmiddleware.Routing{Default: "default", Queues: map[string]int{"default": 1, "high": 3}, Tasks: map[string]string{"task": "high"}}
	tasks := []TaskSchedule{{Spec: "@hourly", Task: scheduledTask}}
	commands := []CommandSchedule{{Spec: "@daily", Command: "allowed"}}
	for _, bad := range []CommandSchedule{{Spec: "invalid", Command: "allowed"}, {Spec: "@daily", Command: "unknown"}} {
		scheduler := &schedulerStub{}
		if err := RegisterSchedules(scheduler, routing, commandStub{}, tasks, []CommandSchedule{bad}); err == nil || len(scheduler.registered) != 0 {
			t.Fatal("preflight changed scheduler", err)
		}
	}
	scheduler := &schedulerStub{}
	if err := RegisterSchedules(scheduler, routing, commandStub{}, tasks, commands); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(scheduler.destinations) != "[high default]" {
		t.Fatal("schedule routing inconsistent", scheduler.destinations)
	}
	rollbackError := errors.New("rollback failure")
	scheduler = &schedulerStub{failAt: 2, rollbackError: rollbackError}
	if err := RegisterSchedules(scheduler, routing, commandStub{}, tasks, commands); !errors.Is(err, rollbackError) || fmt.Sprint(scheduler.removed) != "[1]" {
		t.Fatal("rollback or error preservation failed", err, scheduler.removed)
	}
	if err := RegisterSchedules(&schedulerStub{}, routing, nil, tasks, commands); err == nil {
		t.Fatal("disabled commands accepted")
	}
}
