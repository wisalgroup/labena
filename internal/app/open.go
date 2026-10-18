package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/lifecycle"
	"github.com/wisalgroup/labena/internal/platform/logviewer"
	"github.com/wisalgroup/labena/internal/platform/seeders"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/logging"
)

// open acquires resources and completes registration before exposing the app.
// Failure releases acquired resources. Serving starts only when run is called.
func open(ctx context.Context, cfg config.Config, selected Processes) (_ *application, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Database.DSN != "" && cfg.Database.Driver != database.DriverPostgres {
		return nil, errors.New("app: installed modules require PostgreSQL")
	}
	a := &application{cfg: cfg}
	logConfig := cfg.Logging.Channels(cfg.App.AppName, cfg.App.Environment)
	if cfg.Logging.Viewer.Enabled {
		a.logViewer, err = logviewer.New(cfg.Logging.Viewer.ViewerConfig(), logConfig.Channels)
		if err != nil {
			return nil, err
		}
		logConfig.WrapHandler = a.logViewer.WrapHandler
	}
	logs, err := logging.Open(logConfig)
	if err != nil {
		return nil, err
	}
	a.resources.Logger = logs.Default()
	a.lifetime, err = lifecycle.New(lifecycle.Config{StartupTimeout: cfg.App.StartupTimeout, ShutdownTimeout: cfg.App.ShutdownTimeout, CleanupTimeout: cfg.App.CleanupTimeout}, a.resources.Logger)
	if err != nil {
		return nil, errors.Join(err, logs.Close())
	}
	a.lifetime.Own("logging", logs.Shutdown)
	defer func() {
		// Native registries can panic on conflicting definitions. Discard partial
		// initialization and release every acquired resource on either kind of failure.
		if recovered := recover(); recovered != nil {
			if cause, ok := recovered.(error); ok {
				err = fmt.Errorf("app: initialization: %w", cause)
			} else {
				err = fmt.Errorf("app: initialization: %v", recovered)
			}
		}
		if err != nil {
			err = errors.Join(err, a.lifetime.Close(context.WithoutCancel(ctx)))
		}
	}()
	ctx, cancel := context.WithTimeout(logging.WithLogger(ctx, a.resources.Logger), cfg.App.StartupTimeout)
	defer cancel()
	if err := a.openInfrastructure(ctx); err != nil {
		return nil, err
	}
	authentication, err := a.newAuthentication()
	if err != nil {
		return nil, fmt.Errorf("construct authentication: %w", err)
	}

	// Security wiring belongs to this composition. Disabled authentication still
	// creates a protected group that fails closed. Push registration goes through
	// Identity's transaction and session lock, including during logout.
	public := huma.NewGroup(a.httpRouter, "/api/v1")
	if cfg.RateLimit.Enabled {
		public.UseMiddleware(api.RateLimit(a.resources.RateLimiter, cfg.RateLimit.Limit(), api.ClientIPKey))
	}
	a.targets.HTTP = public
	a.targets.AuthenticatedHTTP = api.Authenticated(public, authentication)
	a.targets.Migrations = &migrate.Registry{}
	a.targets.Seeders = &seeders.Registry{}
	if err := a.registerConsole(); err != nil {
		return nil, err
	}
	if err := a.registerModules(authentication); err != nil {
		return nil, err
	}
	if err := a.registerCapabilities(); err != nil {
		return nil, err
	}
	if _, err := a.targets.Migrations.FS(cfg.Database.Driver); err != nil {
		return nil, fmt.Errorf("app: migrations: %w", err)
	}
	if err := a.registerSchedules(); err != nil {
		return nil, fmt.Errorf("app: schedules: %w", err)
	}
	if err := a.configureProcesses(selected); err != nil {
		return nil, err
	}
	if err := cli.Validate(a.targets.RootCommand); err != nil {
		return nil, fmt.Errorf("app: commands: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a, nil
}
