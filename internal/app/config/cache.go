package config

import (
	"time"

	"github.com/wisalgroup/labena/pkg/cache"
	cachedb "github.com/wisalgroup/labena/pkg/cache/database"
	cachememory "github.com/wisalgroup/labena/pkg/cache/memory"
	cacheredis "github.com/wisalgroup/labena/pkg/cache/redis"
)

type CacheConfig struct {
	Driver           cache.Driver  `env:"CACHE_DRIVER" envDefault:"disabled"`
	Namespace        string        `env:"CACHE_NAMESPACE" envDefault:"default"`
	MaxEntryBytes    int           `env:"CACHE_MAX_ENTRY_BYTES" envDefault:"1048576"`
	MemoryMaxBytes   int64         `env:"CACHE_MEMORY_MAX_BYTES" envDefault:"67108864"`
	MemoryCapacity   int           `env:"CACHE_MEMORY_CAPACITY" envDefault:"10000"`
	RedisURL         string        `env:"CACHE_REDIS_URL" envDefault:"redis://127.0.0.1:6379/0" json:"-"`
	DatabaseTable    string        `env:"CACHE_DATABASE_TABLE" envDefault:"cache_entries"`
	OperationTimeout time.Duration `env:"CACHE_OPERATION_TIMEOUT" envDefault:"5s"`
}

func (c CacheConfig) MemoryConfig() cachememory.Config {
	return cachememory.Config{Capacity: c.MemoryCapacity, MaxEntryBytes: c.MaxEntryBytes, MaxBytes: c.MemoryMaxBytes}
}

func (c CacheConfig) DatabaseConfig() cachedb.Config {
	return cachedb.Config{Table: c.DatabaseTable, Namespace: c.Namespace, MaxEntryBytes: c.MaxEntryBytes, OperationTimeout: c.OperationTimeout}
}

func (c CacheConfig) RedisConfig() cacheredis.Config {
	return cacheredis.Config{Namespace: c.Namespace, MaxEntryBytes: c.MaxEntryBytes, OperationTimeout: c.OperationTimeout}
}

func (c CacheConfig) Validate() error {
	if err := c.Driver.Validate(); err != nil {
		return err
	}
	switch c.Driver {
	case cache.DriverMemory:
		return c.MemoryConfig().Validate()
	case cache.DriverRedis:
		return c.RedisConfig().Validate()
	case cache.DriverDatabase:
		return c.DatabaseConfig().Validate()
	}
	return nil
}
