package config

import (
	"os"
	"strings"
	"testing"

	"github.com/caarlos0/env/v11"
)

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	fields, err := env.GetFieldParams(&Config{})
	if err != nil {
		t.Fatal(err)
	}

	keys := map[string]bool{"APP_ENV_FILE": true}
	for _, field := range fields {
		keys[field.Key] = true
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "HTTP_") || strings.HasPrefix(key, "QUEUE_") || strings.HasPrefix(key, "EVENTS_") || strings.HasPrefix(key, "FILESYSTEM_") {
			keys[key] = true
		}
	}
	for key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("APP_ENV_FILE", "")
}

func loadTestConfig(t *testing.T, values map[string]string) (Config, error) {
	t.Helper()
	clearConfigEnvironment(t)
	for key, value := range values {
		t.Setenv(key, value)
	}
	return Load()
}
