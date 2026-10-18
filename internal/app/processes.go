package app

import (
	"context"
	"errors"

	"github.com/wisalgroup/labena/internal/platform/lifecycle"
	"github.com/wisalgroup/labena/internal/platform/notification"
)

const (
	stopAdmission = iota
	stopWorkers
	stopSubscribers
	stopMetrics
)

// configureProcesses resolves execution dependencies without starting any runners.
// Consumers start before their producers and remain alive while producers drain.
func (a *application) configureProcesses(selected Processes) error {
	if selected == (Processes{}) {
		return nil
	}
	if a.metrics != nil {
		server, err := a.newMetricsServer()
		if err != nil {
			return err
		}
		a.processes = append(a.processes, lifecycle.Process{Name: "metrics", Runner: server, Phase: stopMetrics})
	}
	if selected.HTTP || selected.Workers {
		a.processes = append(a.processes, lifecycle.Process{Name: "events", Runner: a.eventRunner, Phase: stopSubscribers})
	}
	if selected.HTTP {
		server, err := a.newHTTPServer()
		if err != nil {
			return err
		}
		dependencies := []<-chan struct{}{a.eventRunner.Ready()}
		if selected.Workers {
			dependencies = append(dependencies, a.queueWorker.Ready())
		}
		a.processes = append(a.processes, lifecycle.Process{Name: "http", Runner: lifecycle.AfterReady(server, dependencies...), Phase: stopAdmission})
	}
	if selected.Workers {
		dispatcher, err := notification.NewDispatcher(a.resources.Notifier, a.cfg.Notification.DispatchInterval)
		if err != nil {
			return err
		}
		a.processes = append(a.processes,
			lifecycle.Process{Name: "notification dispatch", Runner: dispatcher, Phase: stopAdmission},
			lifecycle.Process{Name: "queue", Runner: lifecycle.AfterReady(a.queueWorker, a.eventRunner.Ready()), Phase: stopWorkers},
		)
	}
	if selected.Scheduler {
		a.processes = append(a.processes, lifecycle.Process{Name: "scheduler", Runner: a.schedulerRunner, Phase: stopAdmission})
	}
	return nil
}

func (a *application) run(ctx context.Context) error {
	if len(a.processes) == 0 {
		return errors.New("app: select at least one process")
	}
	// CLI inspection supports an unconfigured database; serving does not.
	if a.resources.DB == nil || a.resources.Transactions == nil {
		return errors.New("app: DATABASE_URL is required by the installed business modules")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.resources.Logger.InfoContext(ctx, "application starting", "version", version, "revision", buildRevision())
	return a.lifetime.Run(ctx, a.processes...)
}
