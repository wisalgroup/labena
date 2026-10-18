// Package integration connects media to the application's transports and migrations.
package integration

import (
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/media"
	"github.com/wisalgroup/labena/internal/platform/media/migrations"
)

// Register borrows a constructed capability and registers selected transports once
// during startup. It performs no I/O and starts no goroutines.
func Register(capability *media.Library, targets dependencies.Targets) error {
	if capability == nil {
		return media.ErrLibraryRequired
	}
	if targets.Migrations != nil {
		if err := targets.Migrations.Register("internal/platform/media/migrations", migrations.Files); err != nil {
			return err
		}
	}
	if targets.RootCommand != nil {
		targets.RootCommand.AddCommand(newCommands(capability))
	}
	if targets.HTTP != nil {
		RegisterContentHTTP(targets.HTTP, capability)
	}
	if targets.TaskRouter != nil {
		registerTasks(targets.TaskRouter, capability)
	}
	return nil
}
