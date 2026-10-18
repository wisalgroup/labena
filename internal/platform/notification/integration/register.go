// Package integration connects notifications to the application's transports and migrations.
package integration

import (
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/internal/platform/notification/migrations"
)

// Register borrows a constructed capability and registers selected transports once
// during startup. It performs no I/O and starts no goroutines.
func Register(capability *notification.Notifier, targets dependencies.Targets) error {
	if capability == nil {
		return notification.ErrNotifierRequired
	}
	if targets.Migrations != nil {
		if err := targets.Migrations.Register("internal/platform/notification/migrations", migrations.Files); err != nil {
			return err
		}
	}
	if targets.RootCommand != nil {
		targets.RootCommand.AddCommand(newCommands(capability))
	}
	if targets.TaskRouter != nil {
		registerTasks(targets.TaskRouter, capability)
	}
	return nil
}
