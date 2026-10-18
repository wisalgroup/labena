// Package config loads and validates this application's typed settings.
package config

import "time"

// Config is the single environment schema. Load it once before starting work.
type Config struct {
	// Application identity and lifecycle budgets.
	App              AppConfig
	MigrationTimeout time.Duration `env:"MIGRATION_TIMEOUT" envDefault:"5m"`

	// Infrastructure settings; construction belongs to app.
	Database     DatabaseConfig
	Cache        CacheConfig
	Filesystem   FilesystemConfig
	Mail         MailConfig
	Auth         AuthConfig
	RateLimit    RateLimitConfig
	Push         PushConfig
	Notification NotificationConfig
	Media        MediaConfig

	// Execution boundaries.
	HTTP   HTTPConfig
	Queue  QueueConfig
	Events EventsConfig

	// Observability.
	Logging LoggingConfig
	Tracing TracingConfig
	Metrics MetricsConfig
}
