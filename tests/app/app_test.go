package app_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
	logcollector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/wisalgroup/labena/internal/app"
	"github.com/wisalgroup/labena/internal/app/config"
	tasks "github.com/wisalgroup/labena/internal/modules/identity/jobs"
	"github.com/wisalgroup/labena/internal/modules/identity/repository"
	"github.com/wisalgroup/labena/internal/modules/shared/domain/valueobjects"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/notification"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
	"github.com/wisalgroup/labena/pkg/database"
	"github.com/wisalgroup/labena/pkg/storage"
)

// These tests use the same entry points as cmd/app and cmd/cli. No private
// application fixture or exported test hooks participate in assembly.
func TestServeRequiresExplicitProcessesAndDatabase(t *testing.T) {
	cfg := config.Defaults()
	if err := app.Serve(t.Context(), cfg, app.Processes{}); err == nil || !strings.Contains(err.Error(), "select at least one process") {
		t.Fatal(err)
	}
	for _, processes := range []app.Processes{{HTTP: true}, {Workers: true}, {Scheduler: true}} {
		if err := app.Serve(t.Context(), cfg, processes); err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
			t.Fatalf("processes=%+v: %v", processes, err)
		}
	}
	consoleEnv(t)
	if _, err := execute(t, t.Context(), "serve"); err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatal(err)
	}
	if err := app.Execute(t.Context(), cfg, app.Processes{}, []string{"serve"}); err == nil || !strings.Contains(err.Error(), "select at least one process") {
		t.Fatal(err)
	}
	if err := app.Execute(t.Context(), cfg, app.Processes{}, []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := app.Serve(ctx, cfg, app.Processes{HTTP: true}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestServeHTTPMetricsAndQueue(t *testing.T) {
	cfg, db := databaseConfig(t)
	redisURL := os.Getenv("QUEUE_TEST_REDIS")
	if redisURL == "" {
		t.Skip("QUEUE_TEST_REDIS not set")
	}
	var logMu sync.Mutex
	loggedRequests := make(map[string]bool)
	logServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		var batch logcollector.ExportLogsServiceRequest
		if err != nil || proto.Unmarshal(body, &batch) != nil {
			http.Error(w, "invalid OTLP batch", http.StatusBadRequest)
			return
		}
		logMu.Lock()
		defer logMu.Unlock()
		for _, resource := range batch.ResourceLogs {
			for _, scope := range resource.ScopeLogs {
				for _, record := range scope.LogRecords {
					for _, attr := range record.Attributes {
						if attr.Key == "request_id" {
							loggedRequests[attr.Value.GetStringValue()] = true
						}
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(logServer.Close)
	cfg.Logging.DefaultChannels = []string{"observability"}
	cfg.Logging.OTLPEndpoint = logServer.URL + "/v1/logs"
	consoleEnv(t)
	t.Setenv("DATABASE_URL", cfg.Database.DSN)
	if _, err := execute(t, t.Context(), "migrate", "up"); err != nil {
		t.Fatal(err)
	}
	cfg.Queue.RedisURL = redisURL
	cfg.Queue.Default = "app_" + strings.ToLower(rand.Text())
	cfg.Queue.Queues = map[string]config.QueueOptions{cfg.Queue.Default: {Priority: 1}}
	cfg.Queue.ShutdownTimeout = time.Second
	cfg.Metrics.Enabled = true
	cfg.Metrics.Address = unusedAddress(t)
	cfg.HTTP.ReadinessDependencies = true
	cfg.Queue.DashboardEnabled = true
	cfg.Queue.DashboardUsername, cfg.Queue.DashboardPassword = "operator", "test-password"
	cfg.HTTP.CORS.Enabled = true
	cfg.HTTP.CORS.AllowOrigins = []string{"https://app.example"}
	cfg.HTTP.CORS.AllowCredentials = true
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(opts)
	t.Cleanup(func() { pool.Close() })
	inspector := asynq.NewInspectorFromRedisClient(pool)
	t.Cleanup(func() { inspector.DeleteQueue(cfg.Queue.Default, true) })
	stop := serve(t, cfg, app.Processes{HTTP: true, Workers: true})
	base := "http://" + cfg.HTTP.Address
	seen := map[string]bool{}
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/healthz", 200}, {"/readyz", 200}, {"/metrics", 404}, {"/users", 404},
		{"/api/v1/users/me", 401}, {"/api/v1/media/invalid", 401}, {"/api/v1/notifications", 401},
		{"/admin/queues/", 401}, {"/admin/queues/api/queues", 401},
	} {
		request, _ := http.NewRequestWithContext(t.Context(), "GET", base+tc.path, nil)
		request.Header.Set("X-Request-ID", "untrusted-id")
		response, body := requestBody(t, request)
		if response.StatusCode != tc.code {
			t.Fatalf("%s: %d %s", tc.path, response.StatusCode, body)
		}
		id := response.Header.Get("X-Request-ID")
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			t.Fatal("invalid or reused request ID", id, err)
		}
		seen[id] = true
		if strings.HasPrefix(tc.path, "/api/") {
			var problem struct {
				Type   string `json:"type"`
				Status int    `json:"status"`
			}
			if err := json.Unmarshal(body, &problem); err != nil || problem.Status != tc.code || problem.Type == "" || response.Header.Get("Content-Type") != "application/problem+json" {
				t.Fatal(problem, err)
			}
		}
	}
	for _, path := range []string{"/docs", "/openapi.json"} {
		req, _ := http.NewRequestWithContext(t.Context(), "GET", base+path, nil)
		res, body := requestBody(t, req)
		if res.StatusCode != 200 {
			t.Fatal(path, res.StatusCode)
		}
		if path == "/openapi.json" && (!json.Valid(body) || !strings.Contains(string(body), "/api/v1/users")) {
			t.Fatal("live specification missing real routes")
		}
		if path == "/openapi.json" {
			var document struct{ Info struct{ Version string } }
			if err := json.Unmarshal(body, &document); err != nil || document.Info.Version != "dev" {
				t.Fatal("development build advertised an incorrect release", document.Info.Version, err)
			}
		}
	}
	request, _ := http.NewRequestWithContext(t.Context(), "GET", base+"/admin/queues/", nil)
	request.SetBasicAuth("operator", "test-password")
	response, body := requestBody(t, request)
	if response.StatusCode != 200 || !strings.Contains(string(body), "<html") {
		t.Fatal("dashboard", response.StatusCode, string(body))
	}
	for _, origin := range []string{"https://app.example", "https://unlisted.example"} {
		request, _ := http.NewRequestWithContext(t.Context(), "OPTIONS", base+"/api/v1/users", nil)
		request.Header.Set("Origin", origin)
		request.Header.Set("Access-Control-Request-Method", "POST")
		request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
		response, _ := requestBody(t, request)
		if origin == "https://app.example" {
			if response.StatusCode != 204 || response.Header.Get("Access-Control-Allow-Origin") != origin || response.Header.Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatal("CORS preflight", response.StatusCode, response.Header)
			}
		} else if response.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("unlisted origin accepted", response.StatusCode)
		}
	}
	request, _ = http.NewRequestWithContext(t.Context(), "POST", base+"/api/v1/users", strings.NewReader(`{"email":"app@example.com","name":"App test"}`))
	request.Header.Set("Content-Type", "application/json")
	response, body = requestBody(t, request)
	if response.StatusCode != 404 {
		t.Fatal("removed registration route", response.StatusCode)
	}
	// This lifecycle fixture has authentication disabled. Create its account
	// directly; the module integration suite exercises actual OTP onboarding.
	email, _ := valueobjects.NewEmail("app@example.com")
	registered, err := entities.NewUser("App test", email)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.NewUserRepository(db).Create(t.Context(), registered); err != nil {
		t.Fatal(err)
	}
	client := asynq.NewClientFromRedisClient(pool)
	initialTask, err := tasks.NewWelcome(registered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnqueueContext(t.Context(), initialTask, asynq.Queue(cfg.Queue.Default)); err != nil {
		t.Fatal(err)
	}
	dispatcher, err := notification.New(db, notification.Options{Queue: asynqmiddleware.EnqueueFunc(func(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
		return client.EnqueueContext(ctx, task, append(opts, asynq.Queue(cfg.Queue.Default))...)
	})})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		if _, err := dispatcher.Dispatch(t.Context()); err != nil {
			t.Fatal(err)
		}
		var count int
		err := db.NewRaw("SELECT count(*) FROM notifications WHERE recipient_type = 'user' AND recipient_id = ?", registered.ID.String()).Scan(t.Context(), &count)
		return err == nil && count == 1
	})
	// Real queued work must reach the module's registered handler as well.
	task, err := tasks.NewWelcome(registered.ID)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := client.EnqueueContext(t.Context(), task, asynq.Queue(cfg.Queue.Default), asynq.Retention(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		info, err := inspector.GetTaskInfo(cfg.Queue.Default, queued.ID)
		return err == nil && info.State == asynq.TaskStateCompleted
	})
	request, _ = http.NewRequestWithContext(t.Context(), "GET", "http://"+cfg.Metrics.Address+"/metrics", nil)
	response, body = requestBody(t, request)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	for _, name := range []string{"go_goroutines", "asynq_queue_size", "queue_task_duration_seconds_count", "http_server_request_duration_seconds_count"} {
		if !strings.Contains(string(body), name) {
			t.Fatal("missing metric", name)
		}
	}
	if strings.Contains(string(body), queued.ID) {
		t.Fatal("task ID leaked into metrics")
	}
	stop()
	assertListenerClosed(t, cfg.HTTP.Address)
	assertListenerClosed(t, cfg.Metrics.Address)
	// App shutdown flushes request logs even with tracing disabled.
	logMu.Lock()
	defer logMu.Unlock()
	for id := range seen {
		if !loggedRequests[id] {
			t.Errorf("request %s missing from exported logs after shutdown", id)
		}
	}
}

func TestStartupFailureClosesAcquiredDatabase(t *testing.T) {
	cfg, db := databaseConfig(t)
	// Filesystem construction follows database acquisition. A file cannot be a
	// bucket directory, so this fails after a real connection has been acquired.
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Filesystem.Default = "local"
	cfg.Filesystem.Disks["local"].Root = file
	if err := app.Serve(t.Context(), cfg, app.Processes{HTTP: true}); err == nil || !strings.Contains(err.Error(), "filesystem") {
		t.Fatal(err)
	}
	assertAppConnectionsClosed(t, db, cfg)
}

func TestReadinessDependencyDeadline(t *testing.T) {
	cfg, _ := databaseConfig(t)
	blocked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Accept Redis connections but never answer the handshake. Readiness must
	// respect its own budget without changing liveness or blocking shutdown.
	accepted := make(chan []net.Conn, 1)
	go func() {
		var connections []net.Conn
		for {
			conn, err := blocked.Accept()
			if err != nil {
				accepted <- connections
				return
			}
			connections = append(connections, conn)
		}
	}()
	defer func() {
		blocked.Close()
		for _, conn := range <-accepted {
			conn.Close()
		}
	}()
	cfg.Queue.RedisURL = "redis://" + blocked.Addr().String() + "/0"
	cfg.HTTP.ReadinessDependencies = true
	cfg.HTTP.ReadinessTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, cfg, app.Processes{HTTP: true}) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("app did not stop")
		}
	}()
	base := "http://" + cfg.HTTP.Address
	waitFor(t, func() bool {
		response, err := (&http.Client{Timeout: time.Second}).Get(base + "/healthz")
		if err != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode == 200
	})
	request, _ := http.NewRequestWithContext(t.Context(), "GET", base+"/readyz", nil)
	started := time.Now()
	response, body := requestBody(t, request)
	if response.StatusCode != 503 || string(body) != "unavailable\n" || time.Since(started) > time.Second {
		t.Fatal("readiness was not bounded", response.StatusCode, string(body))
	}
}

func TestNamedFilesystemRemainsCallerOwned(t *testing.T) {
	consoleEnv(t)
	cfg := config.Defaults()
	cfg.Filesystem.Disks["spare"] = &config.DiskConfig{Driver: "local", Root: t.TempDir()}
	options, err := cfg.Filesystem.StorageConfig("spare")
	if err != nil {
		t.Fatal(err)
	}
	spare, err := storage.Open(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer spare.Close()
	if _, err := execute(t, t.Context(), "route", "list"); err != nil {
		t.Fatal(err)
	}
	if err := spare.Bucket.WriteAll(t.Context(), "example", []byte("caller-owned"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Filesystem.StorageConfig("missing"); err == nil {
		t.Fatal("unknown disk accepted")
	}
	if _, err := cfg.Filesystem.StorageConfig("s3"); err == nil {
		t.Fatal("incomplete disk accepted")
	}
}

func TestListenerFailureDrainsAndCloses(t *testing.T) {
	for _, listener := range []string{"http", "metrics"} {
		t.Run(listener, func(t *testing.T) {
			cfg, db := databaseConfig(t)
			occupied, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer occupied.Close()
			if listener == "http" {
				cfg.HTTP.Address = occupied.Addr().String()
			} else {
				cfg.Metrics.Enabled = true
				cfg.Metrics.Address = occupied.Addr().String()
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if err := app.Serve(ctx, cfg, app.Processes{HTTP: true}); err == nil || !strings.Contains(err.Error(), listener) {
				t.Fatal(err)
			}
			assertAppConnectionsClosed(t, db, cfg)
		})
	}
}

func TestConsoleServeUsesExplicitProcessesAndMetrics(t *testing.T) {
	cfg, db := databaseConfig(t)
	consoleEnv(t)
	t.Setenv("DATABASE_URL", cfg.Database.DSN)
	t.Setenv("APP_ROLES", "invalid-ignored-role")
	t.Setenv("HTTP_ADDRESS", unusedAddress(t))
	t.Setenv("METRICS_ENABLED", "true")
	address := unusedAddress(t)
	t.Setenv("METRICS_ADDRESS", address)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- executeConfigured(ctx, []string{"serve"}) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("console did not stop")
		}
		assertListenerClosed(t, address)
		assertAppConnectionsClosed(t, db, cfg)
	}()
	waitFor(t, func() bool {
		response, err := (&http.Client{Timeout: time.Second}).Get("http://" + address + "/metrics")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == 200
	})
}

func TestConsoleTracingFlushesAndOmitsArguments(t *testing.T) {
	consoleEnv(t)
	var mu sync.Mutex
	var spans []*tracepb.Span
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var export collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &export); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		for _, resource := range export.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				spans = append(spans, scope.Spans...)
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	t.Setenv("TRACING_ENABLED", "true")
	t.Setenv("TRACING_SAMPLE_RATIO", "1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL+"/v1/traces")
	if _, err := execute(t, t.Context(), "seed", "secret-seeder-argument"); err == nil {
		t.Fatal("unknown seeder accepted")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(spans) != 1 || spans[0].Name != "console.execute" || spans[0].Status.Code != tracepb.Status_STATUS_CODE_ERROR {
		t.Fatalf("missing command failure span: %v", spans)
	}
	if strings.Contains(spans[0].String(), "secret-seeder-argument") {
		t.Fatal("argument leaked into trace")
	}
}

func databaseConfig(t *testing.T) (config.Config, *bun.DB) {
	t.Helper()
	dsn := os.Getenv("DATABASE_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("DATABASE_TEST_POSTGRES not set")
	}
	cfg := config.Defaults()
	cfg.Database.DSN = dsn
	db, err := database.Open(t.Context(), cfg.Database.ConnectionConfig())
	if err != nil {
		t.Fatal(err)
	}
	name := "app_" + strings.ToLower(rand.Text())
	if _, err := db.ExecContext(t.Context(), "CREATE SCHEMA "+name); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := db.ExecContext(ctx, "DROP SCHEMA "+name+" CASCADE"); err != nil {
			t.Error(err)
		}
		db.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", name)
	query.Set("application_name", name)
	parsed.RawQuery = query.Encode()
	cfg.Database.DSN = parsed.String()
	// The observer uses a different application_name, so connection counts only
	// include resources opened by Serve or Execute.
	observerCfg := cfg.Database.ConnectionConfig()
	query.Set("application_name", name+"_observer")
	parsed.RawQuery = query.Encode()
	observerCfg.DSN = parsed.String()
	observer, err := database.Open(t.Context(), observerCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { observer.Close() })
	cfg.HTTP.Address = unusedAddress(t)
	cfg.App.StartupTimeout = 5 * time.Second
	cfg.App.ShutdownTimeout = 5 * time.Second
	cfg.App.CleanupTimeout = 3 * time.Second
	cfg.Queue.ShutdownTimeout = time.Second
	return cfg, observer
}

func assertAppConnectionsClosed(t *testing.T, db *bun.DB, cfg config.Config) {
	t.Helper()
	parsed, _ := url.Parse(cfg.Database.DSN)
	waitFor(t, func() bool {
		var count int
		err := db.NewRaw("SELECT count(*) FROM pg_stat_activity WHERE application_name = ?", parsed.Query().Get("application_name")).Scan(t.Context(), &count)
		return err == nil && count == 0
	})
}

func serve(t *testing.T, cfg config.Config, roles app.Processes) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, cfg, roles) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Error("app did not stop")
			}
		})
	}
	t.Cleanup(stop)
	waitFor(t, func() bool {
		response, err := (&http.Client{Timeout: time.Second}).Get("http://" + cfg.HTTP.Address + "/readyz")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == 200
	})
	return stop
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("condition did not become true", ctx.Err())
		case <-tick.C:
		}
	}
}

func unusedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

func assertListenerClosed(t *testing.T, address string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Error("listener still open", address)
	}
}

func requestBody(t *testing.T, request *http.Request) (*http.Response, []byte) {
	t.Helper()
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, body
}

func consoleEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("QUEUE_REDIS_URL", "redis://127.0.0.1:1/0")
	t.Setenv("FILESYSTEM_DISK", "disabled")
	t.Setenv("CACHE_DRIVER", "disabled")
}

func execute(t *testing.T, ctx context.Context, args ...string) (string, error) {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "console")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previous := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = previous }()
	err = executeConfigured(ctx, args)
	if _, seekErr := output.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	data, readErr := io.ReadAll(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(data), err
}

func TestConfiguredAppDialectFailsBeforeAcquiringResources(t *testing.T) {
	cfg := config.Defaults()
	cfg.Database.Driver = database.DriverMySQL
	cfg.Database.DSN = "nobody@tcp(127.0.0.1:1)/unavailable"
	if err := app.Serve(t.Context(), cfg, app.Processes{HTTP: true}); err == nil || !strings.Contains(err.Error(), "installed modules require PostgreSQL") {
		t.Fatal(err)
	}
}

func TestPushMaintenanceScheduleIsCentralAndConditional(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			cfg, _ := databaseConfig(t)
			queueURL := os.Getenv("QUEUE_TEST_REDIS")
			if queueURL == "" {
				t.Skip("QUEUE_TEST_REDIS not set")
			}
			consoleEnv(t)
			t.Setenv("DATABASE_URL", cfg.Database.DSN)
			if _, err := execute(t, t.Context(), "migrate", "up"); err != nil {
				t.Fatal(err)
			}
			cfg.Queue.RedisURL = queueURL
			cfg.Push.Enabled = enabled
			cfg.Push.ProjectID = "test-project"
			// Keep one media schedule so the scheduler's first Redis heartbeat can be
			// observed even when push is disabled. No one waits for a cron tick.
			cfg.Filesystem.Default = "local"
			cfg.Filesystem.Disks["local"].Root = t.TempDir()
			opts, err := redis.ParseURL(queueURL)
			if err != nil {
				t.Fatal(err)
			}
			pool := redis.NewClient(opts)
			t.Cleanup(func() { pool.Close() })
			inspector := asynq.NewInspectorFromRedisClient(pool)
			before, err := inspector.SchedulerEntries()
			if err != nil {
				t.Fatal(err)
			}
			prior := map[string]bool{}
			for _, entry := range before {
				prior[entry.ID] = true
			}
			stop := serve(t, cfg, app.Processes{HTTP: true, Scheduler: true})
			var entries []*asynq.SchedulerEntry
			waitFor(t, func() bool {
				all, err := inspector.SchedulerEntries()
				if err != nil {
					return false
				}
				entries = nil
				for _, entry := range all {
					if !prior[entry.ID] {
						entries = append(entries, entry)
					}
				}
				return len(entries) > 0
			})
			found := false
			for _, entry := range entries {
				if entry.Task.Type() == notification.PruneSubscriptionsTask {
					found = true
					if entry.Spec != "@every 1h" || len(entry.Task.Payload()) != 0 {
						t.Fatal("unexpected pruning schedule", entry.Spec)
					}
				}
			}
			if found != enabled {
				t.Fatal("push scheduling condition", found, enabled)
			}
			stop()
		})
	}
}

// Mirror cmd/cli: configuration loading belongs to the caller of Execute.
func executeConfigured(ctx context.Context, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return app.Execute(ctx, cfg, app.Processes{HTTP: true}, args)
}
