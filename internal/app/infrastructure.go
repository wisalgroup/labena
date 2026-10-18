package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	wmmetrics "github.com/ThreeDotsLabs/watermill/components/metrics"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/hibiken/asynq"
	asynqmetrics "github.com/hibiken/asynq/x/metrics"
	"github.com/hibiken/asynqmon"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"gocloud.dev/blob"

	sharedmigrations "github.com/wisalgroup/labena/internal/modules/shared/migrations"
	"github.com/wisalgroup/labena/internal/platform/adminhttp"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/cli"
	"github.com/wisalgroup/labena/internal/platform/logviewer"
	"github.com/wisalgroup/labena/internal/platform/mail"
	mailintegration "github.com/wisalgroup/labena/internal/platform/mail/integration"
	"github.com/wisalgroup/labena/internal/platform/media"
	mediaintegration "github.com/wisalgroup/labena/internal/platform/media/integration"
	"github.com/wisalgroup/labena/internal/platform/messaging"
	messagingmiddleware "github.com/wisalgroup/labena/internal/platform/messaging/middleware"
	"github.com/wisalgroup/labena/internal/platform/notification"
	notificationintegration "github.com/wisalgroup/labena/internal/platform/notification/integration"
	"github.com/wisalgroup/labena/pkg/asynqx"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
	"github.com/wisalgroup/labena/pkg/cache"
	cachedb "github.com/wisalgroup/labena/pkg/cache/database"
	cachememory "github.com/wisalgroup/labena/pkg/cache/memory"
	cacheredis "github.com/wisalgroup/labena/pkg/cache/redis"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/httpserver"
	"github.com/wisalgroup/labena/pkg/mailview"
	"github.com/wisalgroup/labena/pkg/metrics"
	"github.com/wisalgroup/labena/pkg/push"
	"github.com/wisalgroup/labena/pkg/ratelimit"
	redisx "github.com/wisalgroup/labena/pkg/redis"
	"github.com/wisalgroup/labena/pkg/storage"
	"github.com/wisalgroup/labena/pkg/tracing"
)

// openInfrastructure completes resources and native registries before module registration.
// Every acquisition immediately records its cleanup with the lifetime owner.
func (a *application) openInfrastructure(ctx context.Context) error {
	if err := a.openTelemetry(ctx); err != nil {
		return fmt.Errorf("app: telemetry: %w", err)
	}
	if a.cfg.Push.Enabled {
		client, err := push.NewExpo(push.ExpoConfig{AccessToken: a.cfg.Push.AccessToken})
		if err != nil {
			return err
		}
		a.resources.Pusher = client
	}
	if err := a.openDatabase(ctx); err != nil {
		return fmt.Errorf("app: database: %w", err)
	}
	if err := a.openCache(ctx); err != nil {
		return fmt.Errorf("app: cache: %w", err)
	}
	if err := a.openFilesystem(ctx); err != nil {
		return fmt.Errorf("app: filesystem: %w", err)
	}
	if err := a.openQueue(); err != nil {
		return fmt.Errorf("app: queue: %w", err)
	}
	if err := a.openMail(); err != nil {
		return fmt.Errorf("app: mail: %w", err)
	}
	if err := a.openRateLimiter(ctx); err != nil {
		return fmt.Errorf("app: rate limiting: %w", err)
	}
	if err := a.openMessaging(); err != nil {
		return fmt.Errorf("app: messaging: %w", err)
	}
	var err error
	a.httpRouter, err = a.newHTTPRouter()
	if err != nil {
		return err
	}
	if err := a.newCapabilities(); err != nil {
		return err
	}
	return nil
}

func (a *application) openDatabase(ctx context.Context) (err error) {
	cfg := a.cfg
	if cfg.Cache.Driver == cache.DriverDatabase || cfg.Database.DSN != "" {
		a.resources.DB, err = database.Open(ctx, cfg.Database.ConnectionConfig())
		if err != nil {
			return err
		}
		a.lifetime.OwnClient("database", a.resources.DB.Close)
		a.readinessChecks = append(a.readinessChecks, a.resources.DB.PingContext)
		if cfg.Tracing.Enabled || cfg.Metrics.Enabled {
			unregister, observeErr := database.Instrument(a.resources.DB, "primary", a.resources.TracerProvider, a.resources.MeterProvider)
			if observeErr != nil {
				return observeErr
			}
			a.lifetime.OwnClient("database metrics", unregister)
		}
		a.resources.Transactions, err = database.NewTxManager(a.resources.DB, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *application) openCache(ctx context.Context) (err error) {
	cfg := a.cfg
	switch cfg.Cache.Driver {
	case cache.DriverMemory:
		store, openErr := cachememory.NewWithConfig(cfg.Cache.MemoryConfig())
		if openErr != nil {
			return openErr
		}
		a.resources.Cache = store
		a.lifetime.Own("cache", store.Close)
	case cache.DriverDatabase:
		a.resources.Cache, err = cachedb.New(a.resources.DB, cfg.Cache.DatabaseConfig())
	case cache.DriverRedis:
		client, openErr := redisx.OpenURL(ctx, cfg.Cache.RedisURL, cfg.App.StartupTimeout)
		if openErr != nil {
			return openErr
		}
		a.lifetime.OwnClient("cache redis", client.Close)
		if cfg.Tracing.Enabled {
			client.AddHook(redisx.NewTracingHook(a.resources.TracerProvider.Tracer("github.com/wisalgroup/labena/redis")))
		}
		a.resources.Cache, err = cacheredis.New(client, cfg.Cache.RedisConfig())
	}
	if err != nil {
		return err
	}
	if a.resources.Cache != nil && (cfg.Tracing.Enabled || cfg.Metrics.Enabled) {
		a.resources.Cache, err = cache.WithTelemetry(a.resources.Cache, a.resources.TracerProvider, a.resources.MeterProvider)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *application) openFilesystem(ctx context.Context) error {
	a.resources.DefaultDisk = a.cfg.Filesystem.Default
	a.resources.Disks = make(map[string]*blob.Bucket)
	names := append([]string{a.cfg.Filesystem.Default}, a.cfg.Filesystem.Additional...)
	for _, name := range names {
		if name == "disabled" || a.resources.Disks[name] != nil {
			continue
		}
		options, err := a.cfg.Filesystem.StorageConfig(name)
		if err != nil {
			return err
		}
		connection, err := storage.Open(ctx, options)
		if err != nil {
			return fmt.Errorf("app: filesystem %q: %w", name, err)
		}
		a.lifetime.OwnClient("filesystem:"+name, connection.Close)
		a.resources.Disks[name] = connection.Bucket
	}
	return nil
}

func (a *application) openTelemetry(ctx context.Context) error {
	a.resources.MeterProvider = noop.NewMeterProvider()
	if a.cfg.Metrics.Enabled {
		provider, err := metrics.New(a.cfg.App.AppName, a.resources.Logger)
		if err != nil {
			return fmt.Errorf("app: metrics: %w", err)
		}
		a.metrics = provider
		a.resources.MeterProvider = provider.MeterProvider
		a.lifetime.Own("metrics", provider.Shutdown)
	}

	provider, err := tracing.Open(ctx, a.cfg.Tracing.ProviderConfig(a.cfg.App.AppName), a.resources.Logger)
	if err != nil {
		return err
	}
	a.resources.TracerProvider = provider.TracerProvider
	a.lifetime.Own("tracing", provider.Shutdown)

	return nil
}

func (a *application) openMail() error {
	views, err := mailview.New(a.cfg.Mail.Brand(a.cfg.App.AppName))
	if err != nil {
		return err
	}
	a.resources.MailRenderer = views
	sender, err := mail.NewSMTP(a.cfg.Mail.SMTPConfig(a.cfg.App.AppName))
	if err != nil {
		return err
	}
	a.resources.Mailer, err = mail.New(
		mail.WithTracing(sender, a.resources.TracerProvider.Tracer("github.com/wisalgroup/labena/mail")), a.resources.Queue,
	)
	// SMTP opens and closes a connection per Send; the facade borrows the queue.
	return err
}

func (a *application) openRateLimiter(ctx context.Context) error {
	if !a.cfg.RateLimit.Enabled && !a.cfg.Auth.Enabled {
		return nil
	}
	client, err := redisx.OpenURL(ctx, a.cfg.RateLimit.RedisURL, 0)
	if err != nil {
		return err
	}
	a.lifetime.OwnClient("rate limit redis", client.Close)
	a.readinessChecks = append(a.readinessChecks, func(ctx context.Context) error { return client.Ping(ctx).Err() })
	a.resources.RateLimiter, err = ratelimit.NewRedis(client, a.cfg.RateLimit.Namespace)
	if err != nil {
		return err
	}
	if a.cfg.Tracing.Enabled || a.cfg.Metrics.Enabled {
		a.resources.RateLimiter, err = ratelimit.WithTelemetry(a.resources.RateLimiter, a.resources.TracerProvider, a.resources.MeterProvider)
	}
	return err
}

// Create the router before module registration, without binding a listener.
func (a *application) newHTTPRouter() (*api.Server, error) {
	proxies, err := a.cfg.HTTP.TrustedProxyPrefixes()
	if err != nil {
		return nil, err
	}
	options := api.Options{Title: a.cfg.App.AppName + " API", Version: version, DisableDocs: !a.cfg.HTTP.DocsEnabled, TrustTraceContext: a.cfg.Tracing.TrustIncoming, TrustedProxies: proxies}
	if a.cfg.Tracing.Enabled {
		options.Tracer = a.resources.TracerProvider.Tracer("github.com/wisalgroup/labena/http")
	}
	if a.cfg.Metrics.Enabled {
		options.Meter = a.resources.MeterProvider.Meter("github.com/wisalgroup/labena/http")
	}
	if a.cfg.HTTP.CORS.Enabled {
		businessCORS := cors.New(a.cfg.HTTP.CORS.Options())
		options.Middleware = append(options.Middleware, func(next http.Handler) http.Handler {
			crossOrigin := businessCORS.Handler(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
					next.ServeHTTP(w, r)
					return
				}
				crossOrigin.ServeHTTP(w, r)
			})
		})
	}
	r, err := api.New(options)
	if err != nil {
		return nil, err
	}
	var check func(context.Context) error
	if a.cfg.HTTP.ReadinessDependencies {
		check = func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, a.cfg.HTTP.ReadinessTimeout)
			defer cancel()
			for _, probe := range a.readinessChecks {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := probe(ctx); err != nil {
					return err
				}
			}
			return ctx.Err()
		}
	}
	api.RegisterHealthRoutes(r.Mux, a.lifetime.Ready(), a.lifetime.Stopping(), check)
	if err := a.registerQueueDashboard(r.Mux); err != nil {
		return nil, err
	}
	if a.logViewer != nil {
		r.Mux.Handle(logviewer.RootPath, a.logViewer)
		r.Mux.Handle(logviewer.RootPath+"/", a.logViewer)
	}
	return r, nil
}

// The HTTP server also drains upgraded WebSocket connections on shutdown.
func (a *application) newHTTPServer() (*httpserver.Server, error) {
	return httpserver.New(a.cfg.HTTP.ServerConfig(), a.httpRouter)
}

func (a *application) newMetricsServer() (*httpserver.Server, error) {
	return httpserver.New(httpserver.Config{
		Address: a.cfg.Metrics.Address, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, ShutdownTimeout: 5 * time.Second, MaxHeaderBytes: 8192,
	}, a.metrics.Handler())
}

func (a *application) openQueue() error {
	cfg := a.cfg.Queue
	opts, err := cfg.RedisOptions()
	if err != nil {
		return errors.New("queue: invalid Redis URL")
	}
	pool := redis.NewClient(opts)
	a.lifetime.OwnClient("queue redis", pool.Close)
	a.readinessChecks = append(a.readinessChecks, func(ctx context.Context) error { return pool.Ping(ctx).Err() })
	a.queueInspector = asynq.NewInspectorFromRedisClient(pool)
	client := asynq.NewClientFromRedisClient(pool)
	a.resources.Queue = asynqmiddleware.Chain(client.EnqueueContext,
		asynqmiddleware.RequireNoTransaction(),
		asynqmiddleware.ObserveClient(a.resources.TracerProvider),
		asynqmiddleware.Route(cfg.Routing()),
	)
	mux := asynq.NewServeMux()
	a.targets.TaskRouter = mux
	a.targets.ScheduledCommands = &cli.ScheduledCommands{}
	mux.Handle(cli.CommandTask, a.targets.ScheduledCommands)
	observe, err := asynqmiddleware.ObserveWorker(a.targets.TaskRouter, a.resources.TracerProvider, a.resources.MeterProvider, a.resources.Logger)
	if err != nil {
		return err
	}
	mux.Use(observe)
	logger := asynqmiddleware.Logger{Log: a.resources.Logger}
	workerConfig := asynq.Config{
		Concurrency: cfg.Concurrency, Queues: cfg.Priorities(), StrictPriority: cfg.StrictPriority,
		ShutdownTimeout: cfg.ShutdownTimeout, Logger: logger,
		HealthCheckFunc: func(err error) {
			if err != nil {
				a.resources.Logger.Error("queue Redis health check failed")
			}
		},
	}
	workerConfig.RetryDelayFunc = cfg.RetryDelay
	workerConfig.IsFailure = cfg.IsFailure
	server := asynq.NewServerFromRedisClient(pool, workerConfig)
	a.queueWorker, err = asynqx.NewWorker(server, mux, pool, a.cfg.App.StartupTimeout)
	if err != nil {
		return err
	}
	a.scheduler = asynq.NewSchedulerFromRedisClient(pool, &asynq.SchedulerOpts{Logger: logger, Location: time.UTC,
		PostEnqueueFunc: asynqmiddleware.LogScheduledTask(a.resources.Logger),
	})
	a.schedulerRunner, err = asynqx.NewSchedulerRunner(a.scheduler, pool, a.cfg.App.StartupTimeout)
	if err != nil {
		return err
	}
	if a.metrics != nil {
		// Upstream Describe queries Redis. An unchecked collector keeps construction
		// free of connectivity requirements; scrapes use native queue metrics.
		collector := asynqmetrics.NewQueueMetricsCollector(a.queueInspector)
		if err := a.metrics.Registerer().Register(queueMetrics{collector}); err != nil {
			return err
		}
	}
	return nil
}

type queueMetrics struct{ prometheus.Collector }

func (queueMetrics) Describe(chan<- *prometheus.Desc) {}

// The dashboard shares the HTTP lifecycle. Its private Redis connections are
// closed with app resources, after HTTP requests have drained.
func (a *application) registerQueueDashboard(router *http.ServeMux) error {
	cfg := a.cfg.Queue
	if !cfg.DashboardEnabled {
		return nil
	}
	opts, err := cfg.RedisOptions()
	if err != nil {
		return errors.New("queue dashboard: invalid Redis URL")
	}
	dashboard := asynqmon.New(asynqmon.Options{
		RootPath: "/admin/queues",
		RedisConnOpt: asynq.RedisClientOpt{
			Network: opts.Network, Addr: opts.Addr, Username: opts.Username,
			Password: opts.Password, DB: opts.DB, TLSConfig: opts.TLSConfig,
			DialTimeout: opts.DialTimeout, ReadTimeout: opts.ReadTimeout, WriteTimeout: opts.WriteTimeout,
		},
	})
	a.lifetime.OwnClient("queue dashboard", dashboard.Close)
	protected, err := adminhttp.Protect(dashboard, adminhttp.Config{Realm: "Queues", Username: cfg.DashboardUsername, Password: cfg.DashboardPassword})
	if err != nil {
		return err
	}
	router.Handle(dashboard.RootPath()+"/", protected)
	return nil
}

const (
	eventsMessagingSystem = "gochannel"
)

func (a *application) openMessaging() error {
	logger := messaging.Logger{
		Log: a.resources.Logger,
	}

	bus := messaging.NewLocalBus(logger)

	a.lifetime.OwnClient("events", bus.Close)

	router, err := message.NewRouter(
		message.RouterConfig{
			CloseTimeout: a.cfg.Events.ShutdownTimeout,
		},
		logger,
	)
	if err != nil {
		return fmt.Errorf("create events router: %w", err)
	}

	a.targets.EventRouter = router
	a.targets.EventSubscriber = bus
	a.eventRunner, err = messaging.RegisterRunner(router)
	if err != nil {
		return err
	}

	router.AddMiddleware(bus.DeliveryMiddleware)
	publisher, err := a.instrumentEvents(router, bus)
	if err != nil {
		return fmt.Errorf("instrument events: %w", err)
	}

	a.resources.EventPublisher = publisher

	return nil
}

func (a *application) instrumentEvents(
	router *message.Router,
	publisher message.Publisher,
) (messaging.Publisher, error) {
	var err error

	// Metrics decorate the raw Watermill subscriber, publisher and handlers.
	if a.metrics != nil {
		metrics := wmmetrics.NewPrometheusMetricsBuilder(
			a.metrics.Registerer(),
			"watermill",
			"events",
		)

		router.AddSubscriberDecorators(
			metrics.DecorateSubscriber,
		)

		router.AddMiddleware(
			metrics.NewRouterMiddleware().Middleware,
		)

		publisher, err = metrics.DecoratePublisher(publisher)
		if err != nil {
			return nil, fmt.Errorf(
				"decorate events publisher with metrics: %w",
				err,
			)
		}
	}

	// Trace outgoing messages before they reach the underlying broker.
	//
	// We intentionally propagate TraceContext only, rather than baggage.
	if a.cfg.Tracing.Enabled {
		tracer := a.resources.TracerProvider.Tracer(
			"github.com/wisalgroup/labena/messaging",
		)

		publisher = messagingmiddleware.NewTracedPublisher(
			publisher,
			tracer,
			propagation.TraceContext{},
			eventsMessagingSystem,
		)
	}

	retry := middleware.Retry{
		MaxRetries:          3,
		InitialInterval:     100 * time.Millisecond,
		MaxInterval:         time.Second,
		Multiplier:          2,
		RandomizationFactor: 0.5,
		Logger:              messaging.Logger{Log: a.resources.Logger},

		// Each retry should behave as a fresh handler attempt rather than
		// inheriting context mutations from the previous attempt.
		ResetContextOnRetry: true,
	}

	// Retries are process-local. Shutdown or a crash can lose pending events.

	router.AddMiddleware(
		middleware.Timeout(20*time.Second),
		messagingmiddleware.LogFailures(a.resources.Logger),
		retry.Middleware,
	)

	if a.cfg.Tracing.Enabled {
		router.AddMiddleware(
			messagingmiddleware.TraceHandler(
				a.resources.TracerProvider.Tracer(
					"github.com/wisalgroup/labena/messaging",
				),
				propagation.TraceContext{},
				a.resources.Logger,
				eventsMessagingSystem,
			),
		)
	}

	router.AddMiddleware(
		middleware.Recoverer,
	)

	// Only after all Watermill-level decorators are installed do we expose
	// the higher-level application publisher.
	return messaging.NewPublisher(publisher)
}

// newCapabilities constructs platform behavior from the already opened clients.
func (a *application) newCapabilities() error {
	notifier, err := notification.New(a.resources.DB, notification.Options{
		Logger:        a.resources.Logger,
		Meter:         a.resources.MeterProvider.Meter("github.com/wisalgroup/labena/notification"),
		Queue:         a.resources.Queue,
		Mailer:        a.resources.Mailer,
		MailRenderer:  a.resources.MailRenderer,
		Pusher:        a.resources.Pusher,
		PushProject:   a.cfg.Push.ProjectID,
		SessionActive: a.pushSessionActive(),
	})
	if err != nil {
		return fmt.Errorf("notifications: %w", err)
	}
	a.resources.Notifier = notifier
	mediaConfig, err := a.cfg.Media.LibraryConfig()
	if err != nil {
		return err
	}
	mediaConfig.Disks = a.cfg.Filesystem.MediaConfigs()
	assets, err := media.New(a.resources.DB, a.resources.Disks, a.resources.DefaultDisk, mediaConfig)
	if err != nil {
		return fmt.Errorf("media: %w", err)
	}
	a.resources.Media = assets
	return nil
}

// registerCapabilities installs generic transports and schema definitions.
// User-specific HTTP operations are registered by Identity.
func (a *application) registerCapabilities() error {
	if err := a.targets.Migrations.Register("internal/modules/shared/migrations", sharedmigrations.Files); err != nil {
		return err
	}
	if err := mailintegration.Register(a.resources.Mailer, a.targets); err != nil {
		return fmt.Errorf("register mail: %w", err)
	}
	if err := notificationintegration.Register(a.resources.Notifier, a.targets); err != nil {
		return fmt.Errorf("register notifications: %w", err)
	}
	if err := mediaintegration.Register(a.resources.Media, a.targets); err != nil {
		return fmt.Errorf("register media: %w", err)
	}
	return nil
}
