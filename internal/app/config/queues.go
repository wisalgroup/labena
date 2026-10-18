package config

import (
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/wisalgroup/labena/internal/platform/adminhttp"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
)

// QueueConfig is operational Asynq configuration.
// Concurrency is the total active tasks in this process, not a per-queue count.
type QueueConfig struct {
	DashboardEnabled  bool   `env:"QUEUE_DASHBOARD_ENABLED" envDefault:"false"`
	DashboardUsername string `env:"QUEUE_DASHBOARD_USERNAME"`
	DashboardPassword string `env:"QUEUE_DASHBOARD_PASSWORD" json:"-"`
	// Routes maps exact task names to queues; unlisted tasks use Default.
	Routes map[string]string `env:"QUEUE_ROUTES" envKeyValSeparator:"="`
	// Nil callbacks retain native Asynq defaults; configure before app construction.
	RetryDelay asynq.RetryDelayFunc `env:"-" json:"-"`
	IsFailure  func(error) bool     `env:"-" json:"-"`

	RedisURL        string                  `env:"QUEUE_REDIS_URL" envDefault:"redis://127.0.0.1:6379/1" json:"-"`
	Default         string                  `env:"QUEUE_DEFAULT" envDefault:"default"`
	Queues          map[string]QueueOptions `env:"-"`
	Concurrency     int                     `env:"QUEUE_CONCURRENCY" envDefault:"4"`
	StrictPriority  bool                    `env:"QUEUE_STRICT_PRIORITY" envDefault:"false"`
	ShutdownTimeout time.Duration           `env:"QUEUE_SHUTDOWN_TIMEOUT" envDefault:"15s"`
}

// QueueOptions contains Asynq's per-queue worker settings. Retry limits and
// execution timeouts belong to tasks; concurrency belongs to the worker pool.
type QueueOptions struct {
	Priority int `env:"PRIORITY"`
}

// loadQueues is the central list of application queues. Add definitions here;
// QUEUE_<NAME>_PRIORITY can override their weights without changing the list.
func (q *QueueConfig) loadQueues(values environment) error {
	q.Queues = map[string]QueueOptions{
		"default": {Priority: 1},
		"high":    {Priority: 3},
	}
	for _, name := range slices.Sorted(maps.Keys(q.Queues)) {
		options := q.Queues[name]
		if err := values.parse(&options, "QUEUE_"+strings.ToUpper(name)+"_"); err != nil {
			return err
		}
		q.Queues[name] = options
	}
	return nil
}

var queueName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (q QueueConfig) Validate() error {
	if q.DashboardEnabled {
		if err := (adminhttp.Config{Username: q.DashboardUsername, Password: q.DashboardPassword}).Validate(); err != nil {
			return err
		}
	}
	_, err := q.RedisOptions()
	if err != nil {
		return errors.New("config: invalid QUEUE_REDIS_URL")
	}
	if q.Concurrency < 1 || q.Concurrency > 1024 {
		return errors.New("config: queue concurrency must be between 1 and 1024")
	}
	if q.ShutdownTimeout <= 0 {
		return errors.New("config: queue shutdown timeout must be positive")
	}
	if q.Queues[q.Default].Priority <= 0 {
		return errors.New("config: default queue must have a positive priority")
	}
	for name, options := range q.Queues {
		if !queueName.MatchString(name) || options.Priority <= 0 {
			return errors.New("config: invalid queue name or priority")
		}
	}
	return q.Routing().Validate()
}

// Priorities returns an independent map for the native Asynq worker settings.
func (q QueueConfig) Priorities() map[string]int {
	priorities := make(map[string]int, len(q.Queues))
	for name, options := range q.Queues {
		priorities[name] = options.Priority
	}
	return priorities
}

// Routing returns the queue policy used by client middleware and schedules.
// Consumers snapshot maps when retaining the configuration beyond assembly.
func (q QueueConfig) Routing() asynqmiddleware.Routing {
	return asynqmiddleware.Routing{Default: q.Default, Queues: q.Priorities(), Tasks: q.Routes}
}

// RedisOptions returns fresh connection settings shared by queue clients and
// dashboard setup. It parses configuration without opening a connection.
func (q QueueConfig) RedisOptions() (*redis.Options, error) {
	options, err := redis.ParseURL(q.RedisURL)
	if err != nil {
		return nil, errors.New("config: invalid QUEUE_REDIS_URL")
	}
	options.ContextTimeoutEnabled = true
	options.DialTimeout = time.Second
	options.ReadTimeout = time.Second
	options.WriteTimeout = time.Second
	return options, nil
}
