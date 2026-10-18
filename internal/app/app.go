// Package app assembles business modules and platform capabilities and owns their
// execution lifetime. Callers supply configuration and select processes explicitly.
package app

import (
	"context"
	"errors"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/lifecycle"
	"github.com/wisalgroup/labena/internal/platform/logviewer"
	"github.com/wisalgroup/labena/internal/platform/messaging"
	"github.com/wisalgroup/labena/pkg/asynqx"
	"github.com/wisalgroup/labena/pkg/metrics"
)

var (
	ErrStarted         = lifecycle.ErrStarted
	ErrRunning         = lifecycle.ErrRunning
	ErrShutdownTimeout = lifecycle.ErrShutdownTimeout
)

// Processes selects execution, independently of module registration. HTTP and
// Workers include their process-local event consumers. Workers also dispatch
// pending notifications. Scheduler enqueues recurring work for available workers.
type Processes struct{ HTTP, Workers, Scheduler bool }

// application owns the state for one invocation. Startup populates it before
// execution; modules borrow only resources and targets, never the application.
type application struct {
	cfg      config.Config
	lifetime *lifecycle.Runtime

	resources dependencies.Resources
	targets   dependencies.Targets
	processes []lifecycle.Process

	readinessChecks []func(context.Context) error
	httpRouter      *api.Server
	queueInspector  *asynq.Inspector
	queueWorker     *asynqx.Worker
	scheduler       *asynq.Scheduler
	schedulerRunner *asynqx.SchedulerRunner
	eventRunner     *messaging.Runner
	metrics         *metrics.Provider
	logViewer       *logviewer.Viewer
}

// Serve constructs the application, runs the selected processes, and releases
// resources after their users finish. A zero selection is an error. The caller
// owns configuration loading, process signals and exit handling.
func Serve(ctx context.Context, cfg config.Config, processes Processes) (err error) {
	if processes == (Processes{}) {
		return errors.New("app: select at least one process")
	}
	a, err := open(ctx, cfg, processes)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.lifetime.Close(context.WithoutCancel(ctx))) }()
	return a.run(ctx)
}

// Execute constructs the same application as Serve and executes one command.
// Only the serve command runs the selected processes; help, completion and other
// commands start no application runners. A zero selection is valid except for serve.
func Execute(ctx context.Context, cfg config.Config, processes Processes, args []string) (err error) {
	a, err := open(ctx, cfg, processes)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.lifetime.Close(context.WithoutCancel(ctx))) }()
	return a.execute(ctx, args)
}
