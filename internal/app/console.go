package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/seeders"
	cachemigrations "github.com/wisalgroup/labena/pkg/cache/migrations"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/logging"
)

func (a *application) registerConsole() error {
	root, err := cli.New(cli.Config{Use: "cli", Short: "Application and module commands", Version: version + " (" + buildRevision() + ")"},
		newMigrateCommand(a.cfg, a.targets.Migrations), newSeedCommand(a.targets.Seeders),
		newRouteCommand(a.cfg, a.httpRouter), newCacheCommand(a.cfg), cli.NewQueueCommand(a.queueInspector),
		a.newServeCommand(),
	)
	if err != nil {
		return err
	}
	a.targets.RootCommand = root
	return nil
}

func (a *application) execute(ctx context.Context, args []string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := a.targets.RootCommand
	root.SetArgs(append([]string{}, args...))
	if a.cfg.Tracing.Enabled {
		tracer := a.resources.TracerProvider.Tracer("github.com/wisalgroup/labena/console")
		traced, operation := tracer.Start(ctx, "console.execute")
		operation.SetAttributes(attribute.String("command", commandName(root, args)))
		ctx = traced
		defer func() {
			defer operation.End()
			if p := recover(); p != nil {
				operation.SetStatus(codes.Error, "command panicked")
				panic(p)
			}
			if err != nil {
				operation.SetStatus(codes.Error, "command failed")
			}
		}()
	}
	return root.ExecuteContext(logging.WithLogger(ctx, a.resources.Logger))
}

func commandName(root *cobra.Command, args []string) string {
	if command, _, err := root.Find(args); err == nil {
		return command.CommandPath()
	}
	return "console"
}

func newRouteCommand(cfg config.Config, router *api.Server) *cobra.Command {
	group := &cobra.Command{Use: "route", Short: "Inspect application routes"}
	group.AddCommand(&cobra.Command{
		Use: "list", Short: "List registered HTTP routes", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Read at execution time, after every module has registered its routes.
			type route struct{ Method, Path, Handler string }
			routes := []route{{"GET", "/healthz", "liveness"}, {"GET", "/readyz", "readiness"}}
			if cfg.HTTP.DocsEnabled {
				routes = append(routes, route{"GET", "/docs", "API reference"}, route{"GET", "/openapi.json", "OpenAPI"})
			}
			if cfg.Queue.DashboardEnabled {
				routes = append(routes, route{"ANY", "/admin/queues/", "queue dashboard"})
			}
			if cfg.Logging.Viewer.Enabled {
				routes = append(routes, route{"ANY", "/admin/logs/", "log viewer"})
			}
			for path, item := range router.OpenAPI().Paths {
				for method, operation := range map[string]*huma.Operation{"GET": item.Get, "POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete, "HEAD": item.Head, "OPTIONS": item.Options, "TRACE": item.Trace} {
					if operation != nil {
						routes = append(routes, route{method, path, operation.OperationID})
					}
				}
			}
			slices.SortFunc(routes, func(a, b route) int {
				if order := strings.Compare(a.Path, b.Path); order != 0 {
					return order
				}
				return strings.Compare(a.Method, b.Method)
			})
			output := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(output, "METHOD\tPATH\tHANDLER"); err != nil {
				return err
			}
			for _, route := range routes {
				if _, err := fmt.Fprintf(output, "%s\t%s\t%s\n", route.Method, route.Path, route.Handler); err != nil {
					return err
				}
			}
			return output.Flush()
		},
	})
	return group
}

func (a *application) newServeCommand() *cobra.Command {
	return &cobra.Command{Use: "serve", Short: "Run the processes selected by this executable", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.run(cmd.Context()) },
	}
}

func newSeedCommand(registry *seeders.Registry) *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{
		Use: "seed [name...]", Short: "List seeders or run the selected seeders",
		Long: "Without names, list registered seeders. With names, run only those seeders in the given order. Seeders are separate from migrations and must be safe to rerun.",
		RunE: func(cmd *cobra.Command, names []string) error {
			if timeout <= 0 {
				return errors.New("seeders: timeout must be positive")
			}
			if len(names) == 0 {
				for _, name := range registry.Names() {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), name); err != nil {
						return err
					}
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			if err := registry.Run(ctx, names...); err != nil {
				return err
			}
			logging.FromContext(ctx).InfoContext(ctx, "seeders completed", "seeders", names)
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Selected seeders completed.")
			return err
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Total seeding deadline")
	return command
}

// migrationError preserves the cause for callers without printing SQL or credentials.
type migrationError struct{ cause error }

func (e migrationError) Error() string {
	return "migration failed; check database access and migration state"
}

func (e migrationError) Unwrap() error { return e.cause }

func newMigrateCommand(cfg config.Config, registry *migrate.Registry) *cobra.Command {
	var timeout time.Duration
	group := &cobra.Command{Use: "migrate", Short: "Manage the centralized Goose migrations"}
	group.PersistentFlags().DurationVar(&timeout, "timeout", 0, "Override MIGRATION_TIMEOUT (total migration and advisory-lock deadline)")
	for _, action := range []string{"up", "down", "status", "version", "refresh"} {
		var force bool
		command := &cobra.Command{
			Use: action, Short: map[string]string{"up": "Apply pending migrations", "refresh": "Delete all objects and data in the current schema/database, then migrate up", "down": "Roll back the latest migration", "status": "List migration states", "version": "Show the current schema version"}[action],
			Long: map[string]string{"refresh": "Destructively recreate the current PostgreSQL schema or selected MySQL database, including all data and Goose history, then apply every registered migration. Stop other application processes first. This is not a rollback and cannot be undone; seeders are not run."}[action],
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if cmd.Flags().Changed("timeout") && timeout <= 0 {
					return errors.New("migrations: timeout must be positive")
				}
				runTimeout := cfg.MigrationTimeout
				if cmd.Flags().Changed("timeout") {
					runTimeout = timeout
				}
				if action == "refresh" {
					if cfg.App.Environment != "development" && cfg.App.Environment != "test" {
						return errors.New("migrations: refresh is restricted to development and test")
					}
					if !force {
						return errors.New("migrations: refresh deletes all data; pass --force to confirm")
					}
				}
				if cfg.Database.DSN == "" {
					return errors.New("migrations: DATABASE_URL is required")
				}
				files, err := registry.FS(cfg.Database.Driver)
				if err != nil {
					return err
				}
				ctx := cmd.Context()
				err = migrate.Run(ctx, cfg.Database.ConnectionConfig(), files, action, runTimeout)
				if err != nil {
					if errors.Is(err, goose.ErrNoMigrations) {
						return err
					}
					logging.FromContext(ctx).ErrorContext(ctx, "migration failed", "action", action, "error_type", fmt.Sprintf("%T", err))
					return migrationError{cause: err}
				}
				return nil
			},
		}
		if action == "refresh" {
			command.Flags().BoolVar(&force, "force", false, "Confirm deletion of all objects and data in the configured database schema")
		}
		group.AddCommand(command)
	}
	return group
}

// Cache table publication writes source files; applying SQL is a separate operation.
func newCacheCommand(cfg config.Config) *cobra.Command {
	group := &cobra.Command{Use: "cache", Short: "Manage cache infrastructure"}
	directory := "internal/modules/shared/migrations"
	publish := &cobra.Command{
		Use: "tables", Short: "Publish database cache migrations without applying them",
		Long: "Publish timestamped SQL for DATABASE_DRIVER into the application migrations directory. Existing files are preserved. Apply published migrations separately with migrate up.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			results, err := migrate.Publish(cmd.Context(), directory, cfg.Database.Driver,
				migrate.Table{Source: cachemigrations.Templates, Template: "cache_entries", Name: cfg.Cache.DatabaseTable},
			)
			for _, result := range results {
				status := "Already published"
				if result.Created {
					status = "Published"
				}
				logging.FromContext(cmd.Context()).DebugContext(cmd.Context(), "table migration publication", "path", result.Path, "created", result.Created)
				if _, outputErr := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", status, result.Path); outputErr != nil {
					return errors.Join(err, outputErr)
				}
			}
			return err
		},
	}
	publish.Flags().StringVar(&directory, "dir", directory, "Application migrations directory")
	group.AddCommand(publish)
	return group
}
