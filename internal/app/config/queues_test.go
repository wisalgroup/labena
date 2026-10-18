package config

import (
	"strings"
	"testing"
	"time"
)

func TestQueueSettings(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"QUEUE_HIGH_PRIORITY": "5", "QUEUE_DEFAULT": "high", "QUEUE_CONCURRENCY": "8"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Queue.Queues["high"].Priority != 5 || cfg.Queue.Queues["default"].Priority != 1 || cfg.Queue.Concurrency != 8 || cfg.Queue.Default != "high" {
		t.Fatal("queue configuration not applied")
	}
	for _, values := range []map[string]string{
		{"QUEUE_DEFAULT_PRIORITY": "0"}, {"QUEUE_HIGH_PRIORITY": "-1"}, {"QUEUE_HIGH_PRIORITY": "private-value"}, {"QUEUE_DEFAULT": "missing"},
		{"QUEUE_REDIS_URL": "private-value"}, {"QUEUE_CONCURRENCY": "0"}, {"QUEUE_CONCURRENCY": "private-value"},
		{"QUEUE_SHUTDOWN_TIMEOUT": "1h"}, {"EVENTS_DRIVER": "postgres"},
	} {
		_, err := loadTestConfig(t, values)
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("validation: %v", err)
		}
	}
}

func TestQueueAndCacheRedisSeparation(t *testing.T) {
	cfg := Defaults()
	if !strings.HasSuffix(cfg.Queue.RedisURL, "/1") || !strings.HasSuffix(cfg.Cache.RedisURL, "/0") {
		t.Fatal("queue and cache defaults must select databases 1 and 0")
	}
	for _, tt := range []struct {
		name, queue, cache, driver string
		wantError                  bool
	}{
		{"separate databases", "redis://localhost:6379/1", "redis://localhost:6379/0", "redis", false},
		{"same database", "redis://localhost:6379/0", "redis://localhost:6379/0", "redis", true},
		{"implicit zero", "redis://localhost:6379", "redis://localhost:6379/0", "redis", true},
		{"query database override", "redis://localhost:6379/1?db=0", "redis://localhost:6379/0", "redis", true},
		{"credentials do not isolate data", "redis://queue:secret@localhost:6379/1", "redis://cache:private@localhost:6379/1", "redis", true},
		{"distinct servers", "redis://queue:6379/0", "redis://cache:6379/0", "redis", false},
		{"disabled cache", "redis://localhost:6379/0", "redis://localhost:6379/0", "disabled", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadTestConfig(t, map[string]string{"QUEUE_REDIS_URL": tt.queue, "CACHE_REDIS_URL": tt.cache, "CACHE_DRIVER": tt.driver})
			if (err != nil) != tt.wantError {
				t.Fatalf("unexpected validation result: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private")) {
				t.Fatal("error exposed credentials")
			}
		})
	}
}

func TestQueueDefinitions(t *testing.T) {
	cfg := Defaults()
	if len(cfg.Queue.Queues) != 2 || cfg.Queue.Queues["default"].Priority != 1 || cfg.Queue.Queues["high"].Priority != 3 {
		t.Fatalf("unexpected queues: %+v", cfg.Queue.Queues)
	}
	priorities := cfg.Queue.Priorities()
	priorities["high"] = 99
	if cfg.Queue.Queues["high"].Priority != 3 {
		t.Fatal("priorities modified configuration")
	}
	cfg.Queue.Queues["bad-name"] = QueueOptions{Priority: 1}
	if err := cfg.Queue.Validate(); err == nil {
		t.Fatal("invalid queue accepted")
	}
	if len(Defaults().Queue.Queues) != 2 {
		t.Fatal("defaults share mutable definitions")
	}
}

func TestTaskQueueRoutes(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"QUEUE_ROUTES": "identity.welcome.v1=high,user:email=high"})
	if err != nil || cfg.Queue.Routes["identity.welcome.v1"] != "high" {
		t.Fatal(cfg.Queue.Routes, err)
	}
	if _, err := loadTestConfig(t, map[string]string{"QUEUE_ROUTES": "identity.welcome.v1=missing"}); err == nil {
		t.Fatal("unknown task destination accepted")
	}
}

func TestQueueDashboardSettings(t *testing.T) {
	if Defaults().Queue.DashboardEnabled {
		t.Fatal("dashboard enabled by default")
	}
	cfg, err := loadTestConfig(t, map[string]string{
		"QUEUE_DASHBOARD_ENABLED": "true", "QUEUE_DASHBOARD_USERNAME": "operator", "QUEUE_DASHBOARD_PASSWORD": "private-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Queue.DashboardEnabled || cfg.Queue.DashboardUsername != "operator" || cfg.Queue.DashboardPassword != "private-value" {
		t.Fatal("dashboard settings not loaded")
	}
	for _, values := range []map[string]string{
		{"QUEUE_DASHBOARD_ENABLED": "true"},
		{"QUEUE_DASHBOARD_ENABLED": "true", "QUEUE_DASHBOARD_USERNAME": "operator"},
		{"QUEUE_DASHBOARD_ENABLED": "true", "QUEUE_DASHBOARD_USERNAME": "bad:user", "QUEUE_DASHBOARD_PASSWORD": "private-value"},
	} {
		_, err := loadTestConfig(t, values)
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("unsafe validation result: %v", err)
		}
	}
}

func TestQueueRedisOptions(t *testing.T) {
	cfg := Defaults().Queue
	cfg.RedisURL = "rediss://operator:password@localhost:6380/3?db=4"
	opts, err := cfg.RedisOptions()
	if err != nil {
		t.Fatal(err)
	}
	if opts.DB != 4 || opts.Addr != "localhost:6380" || opts.Username != "operator" || opts.Password != "password" || opts.TLSConfig == nil {
		t.Fatal("Redis connection settings not preserved")
	}
	if !opts.ContextTimeoutEnabled || opts.DialTimeout != time.Second || opts.ReadTimeout != time.Second || opts.WriteTimeout != time.Second {
		t.Fatal("queue timeout policy not applied")
	}
	opts.DB = 99
	fresh, err := cfg.RedisOptions()
	if err != nil || fresh.DB != 4 {
		t.Fatal("connection options are shared", err)
	}
	cfg.RedisURL = "invalid-private-value"
	if _, err := cfg.RedisOptions(); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("invalid URL error exposes input", err)
	}
}

func TestRateLimitRedisIsolation(t *testing.T) {
	for _, test := range []struct {
		name, limit        string
		enabled, wantError bool
	}{
		{"separate", "redis://localhost:6379/2", true, false},
		{"queue collision", "redis://localhost:6379/1", true, true},
		{"cache collision", "redis://localhost:6379/0", true, true},
		{"different server", "redis://other:6379/1", true, false},
		{"disabled", "redis://localhost:6379/1", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.Queue.RedisURL = "redis://localhost:6379/1"
			cfg.Cache.Driver = "redis"
			cfg.Cache.RedisURL = "redis://localhost:6379/0"
			cfg.RateLimit.Enabled = test.enabled
			cfg.RateLimit.RedisURL = test.limit
			if err := cfg.validateRedisIsolation(); (err != nil) != test.wantError {
				t.Fatalf("isolation: %v", err)
			}
		})
	}
}
