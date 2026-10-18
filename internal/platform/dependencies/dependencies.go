// Package dependencies defines the resources and registration targets shared by
// application assembly and business modules. It imports no app or business module
// assembly packages.
package dependencies

import (
	"log/slog"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/danielgtaylor/huma/v2"
	"github.com/hibiken/asynq"
	"github.com/spf13/cobra"
	"github.com/uptrace/bun"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/blob"

	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/internal/platform/media"
	"github.com/wisalgroup/labena/internal/platform/messaging"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/internal/platform/seeders"
	"github.com/wisalgroup/labena/pkg/asynqx"
	"github.com/wisalgroup/labena/pkg/cache"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/database/migrate"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/push"
	"github.com/wisalgroup/labena/pkg/ratelimit"
)

// Resources contains app-owned infrastructure borrowed by module registration and app assembly.
// App fills it before construction and closes resources after all users finish.
// Copying this bundle shares the same clients; modules must never close them.
// Disabled capabilities may be nil. DB and Transactions must use the same pool
// and be supplied together. Business services belong in explicit arguments.
// Platform capabilities are built once before business modules. Maps are read-only
// after assembly.
type Resources struct {
	DB             *bun.DB
	Transactions   *database.TxManager
	Cache          cache.Store
	RateLimiter    ratelimit.Limiter
	Mailer         mail.Mailer
	MailRenderer   *mailview.Renderer
	Disks          map[string]*blob.Bucket
	DefaultDisk    string
	Notifier       *notification.Notifier
	Pusher         push.Sender
	Media          *media.Library
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Queue          asynqx.Client
	EventPublisher messaging.Publisher
}

// Targets contains borrowed startup registries. Register sequentially before
// execution, then leave them unchanged. Nil fields omit unused transports.
type Targets struct {
	Migrations        *migrate.Registry
	RootCommand       *cobra.Command
	ScheduledCommands *cli.ScheduledCommands
	TaskRouter        *asynq.ServeMux
	EventRouter       *message.Router
	EventSubscriber   message.Subscriber
	Seeders           *seeders.Registry

	// HTTP is the versioned typed business API. Register before services run.
	HTTP huma.API
	// AuthenticatedHTTP requires a validated bearer token and active session.
	AuthenticatedHTTP huma.API
}
