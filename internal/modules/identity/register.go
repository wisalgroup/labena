// Package identity constructs account operations and registers their transports.
// Authentication is supplied by application composition.
package identity

import (
	"errors"

	"github.com/spf13/cobra"

	modulecli "github.com/wisalgroup/labena/internal/modules/identity/delivery/cli"
	eventhandlers "github.com/wisalgroup/labena/internal/modules/identity/delivery/events"
	modulehttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	jobhandlers "github.com/wisalgroup/labena/internal/modules/identity/delivery/jobs"
	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/jobs"
	"github.com/wisalgroup/labena/internal/modules/identity/migrations"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/events"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
)

const PruneSessionsCommand = "identity.prune_sessions"
const PruneVerificationCommand = "identity.prune_verification"

// Register constructs account collaborators and installs selected transports.
// Call once during startup. Resources and targets are borrowed; this function
// performs no I/O and starts no goroutines. Nil authentication disables sign-in.
// AuthenticatedHTTP must already enforce authentication, including when disabled.
// Discard partially populated targets if registration fails; do not retry.
func Register(resources dependencies.Resources, targets dependencies.Targets, authentication service.AuthService, googleEnabled bool) error {
	if googleEnabled && authentication == nil {
		return errors.New("identity: Google sign-in requires authentication")
	}
	users, err := service.NewUserService(repository.NewUserRepository(resources.DB))
	if err != nil {
		return err
	}
	var welcome *jobhandlers.Welcome
	if resources.Notifier != nil {
		welcome = &jobhandlers.Welcome{
			Users:        users,
			Notifier:     resources.Notifier,
			EmailEnabled: resources.Mailer != nil,
		}
	}
	if welcome != nil && targets.EventRouter != nil {
		if targets.EventSubscriber == nil {
			return domain.ErrSubscriberRequired
		}
		if resources.Queue == nil {
			return domain.ErrQueueRequired
		}
	}

	if targets.Migrations != nil {
		if err := targets.Migrations.Register("internal/modules/identity/migrations", migrations.Files); err != nil {
			return err
		}
	}
	if targets.RootCommand != nil {
		group := modulecli.New(users)
		if welcome != nil {
			group.AddCommand(modulecli.NewWelcome(resources.Queue))
		}
		if authentication != nil {
			group.AddCommand(modulecli.NewPruneVerification(authentication))
			group.AddCommand(modulecli.NewPruneSessions(authentication))
		}
		targets.RootCommand.AddCommand(group)
	}
	if welcome != nil {
		if targets.TaskRouter != nil {
			targets.TaskRouter.HandleFunc(jobs.WelcomeType, welcome.Handle)
		}
		if targets.EventRouter != nil {
			targets.EventRouter.AddConsumerHandler(
				"identity.welcome",
				events.UserRegisteredType,
				targets.EventSubscriber,
				eventhandlers.UserRegistered{Queue: resources.Queue}.Handle,
			)
		}
	}
	routes := modulehttp.Routes{
		Users:  users,
		Auth:   authentication,
		SignIn: authentication != nil,
		Social: googleEnabled,
	}
	// Populate optional interfaces only for constructed capabilities; assigning a
	// nil concrete pointer would otherwise enable routes with a typed-nil service.
	if resources.Media != nil {
		routes.Media = resources.Media
	}
	if resources.Notifier != nil {
		routes.Notifications = resources.Notifier
	}
	modulehttp.RegisterRoutes(targets.HTTP, targets.AuthenticatedHTTP, routes)
	if authentication != nil && targets.ScheduledCommands != nil {
		if err := targets.ScheduledCommands.Register(PruneSessionsCommand, func() *cobra.Command {
			return modulecli.NewPruneSessions(authentication)
		}); err != nil {
			return err
		}
		if err := targets.ScheduledCommands.Register(PruneVerificationCommand, func() *cobra.Command {
			return modulecli.NewPruneVerification(authentication)
		}); err != nil {
			return err
		}
	}
	return nil
}
