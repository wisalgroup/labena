package app_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleReportsBuildVersion(t *testing.T) {
	consoleEnv(t)
	output, err := execute(t, t.Context(), "--version")
	if err != nil || !strings.HasPrefix(output, "cli version dev (") || !strings.HasSuffix(output, ")\n") {
		t.Fatalf("missing development version or revision: %q (%v)", output, err)
	}
}

func TestConsoleHelpListsNestedCommandsWithoutStartingServices(t *testing.T) {
	consoleEnv(t)
	t.Setenv("LOG_CHANNELS", "stdout,stderr")
	// Construction can succeed with these settings, but starting application
	// processing would fail: no database, unreachable queue and occupied listeners.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	t.Setenv("HTTP_ADDRESS", occupied.Addr().String())
	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_ADDRESS", occupied.Addr().String())
	diagnostics, err := os.CreateTemp(t.TempDir(), "diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostics.Close()
	previous := os.Stderr
	os.Stderr = diagnostics
	defer func() { os.Stderr = previous }()
	for _, args := range [][]string{{"help"}, {"--help"}, nil} {
		output, err := execute(t, t.Context(), args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, command := range []string{"identity show", "identity welcome", "migrate status", "media upload", "notifications dispatch", "queue inspect", "route list", "completion bash"} {
			if !strings.Contains(output, command) {
				t.Fatalf("missing %q in root help:\n%s", command, output)
			}
		}
		if !strings.HasPrefix(output, "Application and module commands\n") || !strings.HasSuffix(output, "Use \"cli [command] --help\" for more information about a command.\n") {
			t.Fatalf("unexpected output around help:\n%s", output)
		}
	}
	data, err := os.ReadFile(diagnostics.Name())
	if err != nil || len(data) != 0 {
		t.Fatalf("unexpected diagnostics: %s (%v)", data, err)
	}
}

func TestRouteListUsesCompleteRegistrationWithoutStartingListeners(t *testing.T) {
	consoleEnv(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	t.Setenv("HTTP_ADDRESS", occupied.Addr().String())
	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_ADDRESS", occupied.Addr().String())
	output, err := execute(t, t.Context(), "route", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if strings.Join(strings.Fields(lines[0]), " ") != "METHOD PATH HANDLER" {
		t.Fatal(output)
	}
	seen := map[string]bool{}
	previous := ""
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Fatal(line)
		}
		key := fields[1] + " " + fields[0]
		if key <= previous {
			t.Fatalf("unsorted or duplicate route: %s", line)
		}
		previous = key
		seen[key] = true
	}
	for _, expected := range []string{"/healthz GET", "/readyz GET", "/api/v1/users/me PATCH", "/api/v1/users/{id} GET", "/api/v1/notifications GET", "/api/v1/media/{id} GET"} {
		if !seen[expected] {
			t.Fatalf("missing %s in %s", expected, output)
		}
	}
	for key := range seen {
		if strings.HasPrefix(key, "/admin/queues") || strings.HasPrefix(key, "/admin/logs") {
			t.Fatal("dashboard enabled by default")
		}
	}
	if _, err := execute(t, t.Context(), "queue", "list"); err == nil {
		t.Fatal("queue administration enabled by default")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := execute(t, ctx, "route", "list"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLogViewerRouteIsOptional(t *testing.T) {
	consoleEnv(t)
	t.Setenv("LOG_VIEWER_ENABLED", "true")
	t.Setenv("LOG_VIEWER_USERNAME", "operator")
	t.Setenv("LOG_VIEWER_PASSWORD", "test-only")
	output, err := execute(t, t.Context(), "route", "list")
	if err != nil || !strings.Contains(output, "/admin/logs/") {
		t.Fatal(err, output)
	}
}

func TestRouteListPreservesOutputFailure(t *testing.T) {
	consoleEnv(t)
	output, err := os.CreateTemp(t.TempDir(), "closed-output")
	if err != nil {
		t.Fatal(err)
	}
	output.Close()
	previous := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = previous }()
	if err := executeConfigured(t.Context(), []string{"route", "list"}); !errors.Is(err, os.ErrClosed) {
		t.Fatal("lost output error", err)
	}
}

func TestSeedSelectionAndDeadlineValidation(t *testing.T) {
	consoleEnv(t)
	if output, err := execute(t, t.Context(), "seed"); err != nil || output != "" {
		t.Fatal(output, err)
	}
	if _, err := execute(t, t.Context(), "seed", "missing"); err == nil {
		t.Fatal("unknown seeder accepted")
	}
	if _, err := execute(t, t.Context(), "seed", "--timeout=0"); err == nil || !strings.Contains(err.Error(), "timeout must be positive") {
		t.Fatal(err)
	}
}

func TestConfiguredCacheAndFilesystemConstruction(t *testing.T) {
	for _, driver := range []string{"disabled", "memory", "redis", "database"} {
		t.Run(driver, func(t *testing.T) {
			consoleEnv(t)
			t.Setenv("CACHE_DRIVER", driver)
			switch driver {
			case "redis":
				endpoint := os.Getenv("CACHE_TEST_REDIS")
				if endpoint == "" {
					t.Skip("CACHE_TEST_REDIS not set")
				}
				t.Setenv("CACHE_REDIS_URL", endpoint)
			case "database":
				cfg, db := databaseConfig(t)
				t.Setenv("DATABASE_URL", cfg.Database.DSN)
				defer assertAppConnectionsClosed(t, db, cfg)
			}
			t.Setenv("METRICS_ENABLED", "true")
			t.Setenv("FILESYSTEM_DISK", "local")
			root := filepath.Join(t.TempDir(), "default")
			t.Setenv("FILESYSTEM_LOCAL_ROOT", root)
			unused := filepath.Join(t.TempDir(), "unused")
			t.Setenv("FILESYSTEM_S3_ROOT", unused)
			if _, err := execute(t, t.Context(), "route", "list"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal("default filesystem not opened", err)
			}
			if _, err := os.Stat(unused); !os.IsNotExist(err) {
				t.Fatal("unused filesystem opened", err)
			}
		})
	}
}

func TestTableCommandsPublishForApplicationDatabase(t *testing.T) {
	const driver = "postgres"
	consoleEnv(t)
	t.Setenv("DATABASE_DRIVER", driver)
	dir := t.TempDir()
	for range 2 {
		if err := executeConfigured(context.Background(), []string{"cache", "tables", "--dir", dir}); err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	for _, filename := range files {
		body, err := os.ReadFile(filename)
		if err != nil || !strings.HasPrefix(string(body), "-- platform: "+driver+" ") {
			t.Fatal(filename, err)
		}
	}
}

func TestConsoleRejectsMismatchedMigrationDialect(t *testing.T) {
	consoleEnv(t)
	t.Setenv("DATABASE_DRIVER", "mysql")
	for _, args := range [][]string{{"--help"}, {"cache", "tables", "--dir", t.TempDir()}} {
		if _, err := execute(t, t.Context(), args...); err == nil || !strings.Contains(err.Error(), "use one database dialect") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestTablePublicationDefaultsToSharedMigrations(t *testing.T) {
	consoleEnv(t)
	if err := executeConfigured(t.Context(), []string{"cache", "tables"}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("internal/modules/shared/migrations/*_cache_entries.sql")
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	if _, err := os.Stat("migrations"); !os.IsNotExist(err) {
		t.Fatal("created the old migration directory", err)
	}
}

func TestRefreshRejectsProductionEvenWithForce(t *testing.T) {
	consoleEnv(t)
	t.Setenv("APP_ENV", "production")
	if err := executeConfigured(t.Context(), []string{"migrate", "refresh", "--force"}); err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("production refresh was not rejected: %v", err)
	}
}

func TestConsoleAlwaysValidatesApplicationSettings(t *testing.T) {
	consoleEnv(t)
	t.Setenv("HTTP_READ_TIMEOUT", "invalid-private-value")
	for _, args := range [][]string{{"--help"}, {"identity", "show", "--help"}, {"completion", "bash"}, {"identity", "show", "bad-id"}, {"cache", "tables"}, {"migrate", "status"}, {"queue", "list"}} {
		err := executeConfigured(t.Context(), args)
		if err == nil || !strings.Contains(err.Error(), "config:") || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("%v: expected safe configuration error, got %v", args, err)
		}
	}
}

func TestConsoleClosesResourcesAfterHelpAndInputErrors(t *testing.T) {
	consoleEnv(t)
	cfg, db := databaseConfig(t)
	t.Setenv("DATABASE_URL", cfg.Database.DSN)
	for _, args := range [][]string{{"--help"}, {"completion", "bash"}, {"identity", "show", "bad-id"}, {"media", "list", "bad-id"}, {"notifications", "read", "bad-id", "bad-id"}} {
		_, err := execute(t, t.Context(), args...)
		wantError := args[0] != "--help" && args[0] != "completion"
		if (err != nil) != wantError {
			t.Fatalf("%v: %v", args, err)
		}
		assertAppConnectionsClosed(t, db, cfg)
	}
}

func TestQueueAdministrationUsesApplicationResources(t *testing.T) {
	url := os.Getenv("QUEUE_TEST_REDIS")
	if url == "" {
		t.Skip("QUEUE_TEST_REDIS not set")
	}
	consoleEnv(t)
	t.Setenv("QUEUE_REDIS_URL", url)
	if _, err := execute(t, t.Context(), "queue", "list"); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteArguments(t *testing.T) {
	consoleEnv(t)
	previous := os.Args
	os.Args = []string{"console", "unexpected-process-argument"}
	t.Cleanup(func() { os.Args = previous })
	for _, tt := range []struct {
		name      string
		args      []string
		wantError bool
	}{
		{"nil", nil, false},
		{"empty", []string{}, false},
		{"help", []string{"--help"}, false},
		{"module help", []string{"identity", "--help"}, false},
		{"completion", []string{"__complete", "ide"}, false},
		{"unknown command", []string{"unknown"}, true},
		{"unknown flag", []string{"identity", "show", "--unknown"}, true},
		{"extra argument", []string{"route", "list", "extra"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := execute(t, t.Context(), tt.args...); (err != nil) != tt.wantError {
				t.Fatal(err)
			}
		})
	}
}

func TestMigrationCommandValidation(t *testing.T) {
	consoleEnv(t)
	for _, tt := range []struct {
		args      []string
		wantError string
	}{
		{[]string{"migrate", "--help"}, ""},
		{[]string{"migrate", "up", "--help"}, ""},
		{[]string{"migrate", "refresh", "--help"}, ""},
		{[]string{"migrate", "refresh"}, "pass --force"},
		{[]string{"migrate", "refresh", "--force"}, "DATABASE_URL is required"},
		{[]string{"migrate", "refresh", "--timeout=0s"}, "timeout must be positive"},
		{[]string{"migrate", "up", "--timeout=0s"}, "timeout must be positive"},
		{[]string{"migrate", "up", "--timeout=-1s"}, "timeout must be positive"},
		{[]string{"migrate", "status"}, "DATABASE_URL is required"},
		{[]string{"migrate", "version", "--timeout=1s"}, "DATABASE_URL is required"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := execute(t, t.Context(), tt.args...)
			if tt.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("want %q, got %v", tt.wantError, err)
			}
		})
	}
}
