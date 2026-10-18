// Run with CACHE_DRIVER=memory go run ./examples/cache.
// For database or redis, configure the connection in the central environment.
// For the database driver, run cli cache tables, then cli migrate up first.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/pkg/cache"
	cachedb "github.com/wisalgroup/labena/pkg/cache/database"
	"github.com/wisalgroup/labena/pkg/cache/memory"
	cacheredis "github.com/wisalgroup/labena/pkg/cache/redis"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/logging"
	"github.com/wisalgroup/labena/pkg/redis"
)

func run(ctx context.Context, cfg config.Config) (err error) {
	var store cache.Store
	var close func() error
	defer func() {
		if close != nil {
			err = errors.Join(err, close())
		}
	}()
	switch cfg.Cache.Driver {
	case cache.DriverMemory:
		s, openErr := memory.NewWithConfig(cfg.Cache.MemoryConfig())
		if openErr != nil {
			return openErr
		}
		store = s
		close = func() error { return s.Close(context.WithoutCancel(ctx)) }
	case cache.DriverDatabase:
		db, openErr := database.Open(ctx, cfg.Database.ConnectionConfig())
		if openErr != nil {
			return openErr
		}
		close = db.Close
		store, err = cachedb.New(db, cfg.Cache.DatabaseConfig())
	case cache.DriverRedis:
		client, openErr := redis.OpenURL(ctx, cfg.Cache.RedisURL, cfg.App.StartupTimeout)
		if openErr != nil {
			return openErr
		}
		close = client.Close
		store, err = cacheredis.New(client, cfg.Cache.RedisConfig())
	default:
		return errors.New("set CACHE_DRIVER to memory, database or redis")
	}
	if err != nil {
		return err
	}
	const key = "example:v2:greeting"
	value, err := cache.Remember(ctx, store, key, time.Minute, func(ctx context.Context) (string, error) {
		// Replace this with a context-aware database/API call in your service.
		return "hello", nil
	})
	if err != nil {
		return err
	}
	if err := cache.SetMulti[any](ctx, store, map[string]any{
		"example:v2:language": "en",
		"example:v2:enabled":  true,
	}, time.Minute); err != nil {
		return err
	}
	keys := []string{key, "example:v2:language", "example:v2:enabled"}
	values, err := cache.GetMulti[any](ctx, store, keys)
	if err != nil {
		return err
	}
	exists, err := store.Exists(ctx, key)
	if err != nil {
		return err
	}
	logging.FromContext(ctx).InfoContext(ctx, "cache example", "exists", exists, "value", value, "items", len(values))
	// Delete only this example's keys; Clear is an explicit namespace-wide action.
	for _, key := range keys {
		if err := store.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func execute() (err error) {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logs, err := logging.Open(cfg.Logging.Channels(cfg.App.AppName, cfg.App.Environment))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, logs.Close()) }()
	ctx, cancel := context.WithTimeout(logging.WithLogger(context.Background(), logs.Default()), 15*time.Second)
	defer cancel()
	return run(ctx, cfg)
}
