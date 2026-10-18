package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/wisalgroup/labena/pkg/cache"
)

// Validate checks settings without opening connections or selecting runners.
func (c Config) Validate() error {
	if err := c.Push.Validate(); err != nil {
		return err
	}
	if c.Notification.DispatchInterval != 0 && c.Notification.DispatchInterval < 100*time.Millisecond {
		return errors.New("config: notification dispatch interval must be positive")
	}
	if c.RateLimit.Enabled || c.Auth.Enabled {
		if _, err := redis.ParseURL(c.RateLimit.RedisURL); err != nil {
			return errors.New("config: invalid RATE_LIMIT_REDIS_URL")
		}
		if strings.TrimSpace(c.RateLimit.Namespace) == "" {
			return errors.New("config: rate limit namespace is required")
		}
	}

	if c.RateLimit.Enabled {
		if err := c.RateLimit.Limit().Validate(); err != nil {
			return err
		}
	}
	if err := c.Auth.Validate(); err != nil {
		return err
	}
	if c.Auth.Enabled && c.Database.DSN == "" {
		return errors.New("config: authentication challenges require a database")
	}
	if _, err := c.Media.LibraryConfig(); err != nil {
		return err
	}
	if err := c.Mail.Validate(); err != nil {
		return err
	}
	if err := c.Filesystem.Validate(); err != nil {
		return err
	}
	if err := c.Queue.Validate(); err != nil {
		return fmt.Errorf("config: queues: %w", err)
	}
	if err := c.Events.Validate(); err != nil {
		return fmt.Errorf("config: events: %w", err)
	}

	if err := c.Metrics.Validate(); err != nil {
		return err
	}
	if c.Metrics.Enabled && strings.TrimSpace(c.App.AppName) == "" {
		return errors.New("config: APP_NAME is required for metrics")
	}
	if err := c.Tracing.ProviderConfig(c.App.AppName).Validate(); err != nil {
		return err
	}
	if err := c.Logging.Channels(c.App.AppName, c.App.Environment).Validate(); err != nil {
		return err
	}
	if err := c.Logging.Viewer.Validate(); err != nil {
		return err
	}
	for _, channel := range c.Logging.DefaultChannels {
		if strings.TrimSpace(channel) == "observability" && strings.TrimSpace(c.App.AppName) == "" {
			return errors.New("config: APP_NAME is required for log export")
		}
	}
	if err := c.Cache.Validate(); err != nil {
		return err
	}
	if err := c.App.Validate(); err != nil {
		return err
	}
	if c.MigrationTimeout <= 0 {
		return errors.New("config: migration timeout must be positive")
	}
	if c.Cache.Driver == cache.DriverDatabase || c.Database.DSN != "" {
		if err := c.Database.Validate(); err != nil {
			return err
		}
	}
	if c.Queue.ShutdownTimeout >= c.App.ShutdownTimeout {
		return errors.New("config: queue shutdown timeout must be less than app shutdown timeout")
	}
	if err := c.validateRedisIsolation(); err != nil {
		return err
	}
	return c.HTTP.Validate()
}

// validateRedisIsolation keeps independently administered stores apart. Cache
// clearing must never erase queued tasks or reset authentication abuse budgets.
func (c Config) validateRedisIsolation() error {
	type store struct{ name, url string }
	stores := []store{{"queue", c.Queue.RedisURL}}
	if c.Cache.Driver == cache.DriverRedis {
		stores = append(stores, store{"cache", c.Cache.RedisURL})
	}
	if c.RateLimit.Enabled || c.Auth.Enabled {
		stores = append(stores, store{"rate limit", c.RateLimit.RedisURL})
	}
	parsed := make([]*redis.Options, 0, len(stores))
	for i, current := range stores {
		options, err := redis.ParseURL(current.url)
		if err != nil {
			return fmt.Errorf("config: invalid %s Redis URL", current.name)
		}
		for j, previous := range parsed {
			if options.Network == previous.Network && strings.EqualFold(options.Addr, previous.Addr) && options.DB == previous.DB {
				return fmt.Errorf("config: %s and %s must use separate Redis databases", stores[j].name, stores[i].name)
			}
		}
		parsed = append(parsed, options)
	}
	return nil
}
