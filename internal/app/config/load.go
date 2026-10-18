package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// loadEnvironment reads dotenv into an isolated snapshot. Existing process
// variables win. APP_ENV_FILE selects a required file; an explicitly empty value
// disables dotenv. Only a missing implicit .env is ignored.
func loadEnvironment() (environment, error) {
	path, explicit := os.LookupEnv("APP_ENV_FILE")
	if !explicit {
		path = ".env"
	}
	values := make(environment)
	if path != "" {
		fileValues, err := godotenv.Read(path)
		if err != nil {
			if explicit || !errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("config: cannot load environment file")
			}
		}
		for key, value := range fileValues {
			values[key] = value
		}
	}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	return values, nil
}

// environment owns a snapshot shared read-only by all parsing during one Load.
// A non-nil empty snapshot parses defaults without consulting the process.
type environment map[string]string

// parse decodes an env-tagged struct, optionally under a prefix.
// Parsing errors name the field but never include its supplied value.
func (values environment) parse(target any, prefix string) error {
	if values == nil {
		values = environment{}
	}
	return environmentError(env.ParseWithOptions(target, env.Options{Environment: values, Prefix: prefix}))
}

func environmentError(err error) error {
	if err == nil {
		return nil
	}
	var parseError env.ParseError
	if errors.As(err, &parseError) {
		return fmt.Errorf("config: invalid %s (%s)", parseError.Name, parseError.Type)
	}
	return errors.New("config: invalid environment configuration")
}

// Load reads dotenv and environment settings once. It opens no connections.
func Load() (Config, error) {
	values, err := loadEnvironment()
	if err != nil {
		return Config{}, err
	}
	return FromEnvironment(values)
}

// FromEnvironment decodes and validates one complete environment snapshot without
// reading files or process settings. Missing keys use the declared defaults.
// The map is borrowed only for this call; returned settings do not retain it.
func FromEnvironment(values map[string]string) (Config, error) {
	if values == nil {
		values = map[string]string{}
	}
	cfg, err := environment(values).decode()
	if err != nil {
		return Config{}, err
	}
	return cfg, cfg.Validate()
}

// decode assembles settings without I/O or validation. Both Load and Defaults
// use this path so named definitions and tagged defaults stay consistent.
func (values environment) decode() (Config, error) {
	var cfg Config
	if err := values.parse(&cfg, ""); err != nil {
		return Config{}, err
	}
	if err := cfg.Queue.loadQueues(values); err != nil {
		return Config{}, err
	}
	if err := cfg.Filesystem.load(values); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Defaults returns independent default settings without reading files or process values.
func Defaults() Config {
	cfg, err := (environment{}).decode()
	if err != nil {
		panic(err)
	}
	return cfg
}
