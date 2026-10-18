package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDotenvAndTypedEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.env")
	if err := os.WriteFile(path, []byte("CONFIG_TEST_FILE='hello world'\nCONFIG_TEST_OVERRIDE=file\nCONFIG_TEST_EMPTY=file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_TEST_FILE", "")
	os.Unsetenv("CONFIG_TEST_FILE") // t.Setenv restores the original value after this test.
	t.Setenv("CONFIG_TEST_OVERRIDE", "process")
	t.Setenv("CONFIG_TEST_EMPTY", "")
	t.Setenv("APP_ENV_FILE", path)
	var cfg struct {
		File     string        `env:"CONFIG_TEST_FILE"`
		Override string        `env:"CONFIG_TEST_OVERRIDE"`
		Empty    string        `env:"CONFIG_TEST_EMPTY"`
		Timeout  time.Duration `env:"CONFIG_TEST_TIMEOUT" envDefault:"5s"`
	}
	t.Setenv("CONFIG_TEST_TIMEOUT", "5s")
	values, err := loadEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if err := values.parse(&cfg, ""); err != nil {
		t.Fatal(err)
	}
	if cfg.File != "hello world" || cfg.Override != "process" || cfg.Empty != "" || cfg.Timeout != 5*time.Second {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if _, exists := os.LookupEnv("CONFIG_TEST_FILE"); exists {
		t.Fatal("dotenv loading mutated the process environment")
	}
}

func TestMissingAndInvalidFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV_FILE", "")
	os.Unsetenv("APP_ENV_FILE")
	if _, err := loadEnvironment(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV_FILE", "missing.env")
	if _, err := loadEnvironment(); err == nil {
		t.Fatal("accepted missing explicit file")
	}
	if err := os.WriteFile("invalid.env", []byte("SECRET='sensitive unterminated"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV_FILE", "invalid.env")
	if _, err := loadEnvironment(); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unsafe file error: %v", err)
	}
	t.Setenv("APP_ENV_FILE", "")
	if _, err := loadEnvironment(); err != nil {
		t.Fatal(err)
	}
}

func TestParsingErrorDoesNotExposeValue(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("CONFIG_TEST_TIMEOUT", "secret-value")
	var cfg struct {
		Timeout time.Duration `env:"CONFIG_TEST_TIMEOUT"`
	}
	values, err := loadEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	err = values.parse(&cfg, "")
	if err == nil || strings.Contains(err.Error(), "secret-value") || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("unsafe parse error: %v", err)
	}
}

func TestEnvironmentSnapshotIgnoresLaterProcessChanges(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("CONFIG_TEST_TIMEOUT", "7s")
	t.Setenv("CONFIG_TEST_NAMED_TIMEOUT", "3s")
	values, err := loadEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	// Later process changes must not affect any decoding from this load.
	t.Setenv("CONFIG_TEST_TIMEOUT", "invalid-later-value")
	t.Setenv("CONFIG_TEST_NAMED_TIMEOUT", "invalid-later-value")
	var base struct {
		Timeout time.Duration `env:"CONFIG_TEST_TIMEOUT" envDefault:"5s"`
	}
	var named struct {
		Timeout time.Duration `env:"TIMEOUT" envDefault:"5s"`
	}
	if err := values.parse(&named, "CONFIG_TEST_NAMED_"); err != nil || named.Timeout != 3*time.Second {
		t.Fatalf("named snapshot: timeout=%v err=%v", named.Timeout, err)
	}
	if err := values.parse(&base, ""); err != nil || base.Timeout != 7*time.Second {
		t.Fatalf("base snapshot: timeout=%v err=%v", base.Timeout, err)
	}
}

func TestDefaultsMatchLoadingWithoutEnvironment(t *testing.T) {
	clearConfigEnvironment(t)
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Defaults must also ignore file selection and invalid process values.
	t.Setenv("APP_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("HTTP_READ_TIMEOUT", "invalid")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "invalid")
	t.Setenv("QUEUE_CONCURRENCY", "invalid")
	defaults := Defaults()
	if !reflect.DeepEqual(loaded, defaults) {
		t.Fatal("Defaults differs from loading with no environment overrides")
	}
}
