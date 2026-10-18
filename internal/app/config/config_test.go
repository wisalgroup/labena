package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// The operator reference is checked against decoded defaults, including the
// named queue/disk definitions that do not appear in Config's static env tags.
func TestConfigurationReferenceMatchesDefaults(t *testing.T) {
	want := make(map[string]reflect.Value)
	var collect func(reflect.Value, string)
	collect = func(value reflect.Value, prefix string) {
		for i := 0; i < value.NumField(); i++ {
			field, data := value.Type().Field(i), value.Field(i)
			key := field.Tag.Get("env")
			if key == "-" {
				continue
			}
			if key != "" {
				want[prefix+key] = data
			} else if data.Kind() == reflect.Struct {
				collect(data, prefix+field.Tag.Get("envPrefix"))
			}
		}
	}
	cfg := Defaults()
	collect(reflect.ValueOf(cfg), "")
	for name, options := range cfg.Queue.Queues {
		collect(reflect.ValueOf(options), "QUEUE_"+strings.ToUpper(name)+"_")
	}
	var disk DiskConfig
	if err := (environment{}).parse(&disk, ""); err != nil {
		t.Fatal(err)
	}
	collect(reflect.ValueOf(disk), "FILESYSTEM_<NAME>_")
	for name, options := range cfg.Filesystem.Disks {
		value := reflect.ValueOf(*options)
		for i := 0; i < value.NumField(); i++ {
			key := value.Type().Field(i).Tag.Get("env")
			if key != "" && key != "-" && !reflect.DeepEqual(value.Field(i).Interface(), reflect.ValueOf(disk).Field(i).Interface()) {
				want["FILESYSTEM_"+strings.ToUpper(name)+"_"+key] = value.Field(i)
			}
		}
	}
	source, err := os.ReadFile("../../../deployments/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile("(?m)^\\| `([A-Z][A-Z0-9_<>]*)` \\| (.*?) \\|$").FindAllStringSubmatch(string(source), -1)
	seen := make(map[string]bool)
	for _, row := range rows {
		key, documented := row[1], strings.Trim(row[2], "`")
		value, ok := want[key]
		if !ok || seen[key] {
			t.Errorf("unknown or duplicate reference key %s", key)
			continue
		}
		seen[key] = true
		if documented == "unset" {
			documented = ""
		}
		if value.Type() == reflect.TypeFor[time.Duration]() {
			parsed, err := time.ParseDuration(documented)
			if err != nil || parsed != value.Interface().(time.Duration) {
				t.Errorf("reference default for %s differs from config.Defaults()", key)
			}
			continue
		}
		expected := fmt.Sprint(value.Interface())
		switch value.Kind() {
		case reflect.Slice:
			expected = strings.Join(value.Interface().([]string), ",")
		case reflect.Map:
			if value.Len() == 0 {
				expected = ""
			}
		}
		if expected != documented {
			t.Errorf("reference default for %s differs from config.Defaults()", key)
		}
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("missing configuration reference row for %s", key)
		}
	}
}

func TestEnvironmentExamplesMatchConfig(t *testing.T) {
	files, err := filepath.Glob("../../../.env*.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no environment example files found")
	}
	for _, file := range files {
		values, err := godotenv.Read(file)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(file) == ".env.example" {
			// The full profile intentionally needs operator-supplied credentials.
			// Keep real keys out of the template; supply isolated values only here.
			if _, err := FromEnvironment(values); err == nil {
				t.Fatal("full profile accepted unfilled credentials")
			}
			credentials := map[string]string{
				"QUEUE_DASHBOARD_PASSWORD": "test-only-dashboard",
				"MAIL_USERNAME":            "test-only-smtp",
				"MAIL_PASSWORD":            "test-only-smtp-password",
				"AUTH_VERIFICATION_KEY":    base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32))),
				"AUTH_ACCESS_TOKEN_KEY":    base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))),
				"AUTH_GOOGLE_CLIENT_ID":    "test-only.apps.googleusercontent.com",
				"PUSH_EXPO_PROJECT_ID":     "00000000-0000-4000-8000-000000000001",
				"PUSH_EXPO_ACCESS_TOKEN":   "test-only-expo-token",
			}
			for key, value := range credentials {
				if existing, ok := values[key]; !ok || existing != "" {
					t.Fatalf("full profile must have an empty %s placeholder", key)
				}
				values[key] = value
			}
			cfg, err := FromEnvironment(values)
			if err != nil {
				t.Fatalf("full profile with supplied credentials: %v", err)
			}
			if !cfg.HTTP.CORS.Enabled || !cfg.HTTP.ReadinessDependencies || !cfg.RateLimit.Enabled ||
				!cfg.Queue.DashboardEnabled ||
				!cfg.Auth.Enabled || !cfg.Auth.GoogleEnabled ||
				!cfg.Push.Enabled || !cfg.Tracing.Enabled || !cfg.Metrics.Enabled || cfg.Cache.Driver != "redis" || cfg.Filesystem.Default != "minio" {
				t.Fatal("full profile must enable the implemented capabilities with Redis and MinIO")
			}
		}
		if _, err := loadTestConfig(t, values); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
}

func TestHTTPConfig(t *testing.T) {
	if err := (HTTPConfig{}).Validate(); err == nil {
		t.Fatal("accepted missing HTTP address")
	}
	cfg, err := loadTestConfig(t, map[string]string{"HTTP_ADDRESS": ":9090", "HTTP_WRITE_TIMEOUT": "0s", "HTTP_MAX_HEADER_BYTES": "8192"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Address != ":9090" || cfg.HTTP.WriteTimeout != 0 || cfg.HTTP.MaxHeaderBytes != 8192 {
		t.Fatalf("HTTP: %+v", cfg.HTTP)
	}
	for _, values := range []map[string]string{{"HTTP_READ_TIMEOUT": "-1s"}, {"HTTP_MAX_HEADER_BYTES": "-1"}} {
		if _, err := loadTestConfig(t, values); err == nil {
			t.Fatalf("accepted %v", values)
		}
	}
}

func TestCacheSettings(t *testing.T) {
	for _, driver := range []string{"disabled", "memory", "database", "redis"} {
		cfg, err := loadTestConfig(t, map[string]string{
			"CACHE_DRIVER": driver, "DATABASE_URL": "configured-dsn", "DATABASE_DRIVER": "mysql",
			"CACHE_NAMESPACE": "tenant", "CACHE_MAX_ENTRY_BYTES": "2048", "CACHE_MEMORY_CAPACITY": "50", "CACHE_MEMORY_MAX_BYTES": "4096",
			"CACHE_DATABASE_TABLE": "cached_values", "CACHE_OPERATION_TIMEOUT": "2s",
		})
		if err != nil {
			t.Fatal(err)
		}
		mem := cfg.Cache.MemoryConfig()
		if mem.Capacity != 50 || mem.MaxEntryBytes != 2048 || mem.MaxBytes != 4096 {
			t.Fatal("memory cache settings did not reach driver options")
		}
		sql, redis := cfg.Cache.DatabaseConfig(), cfg.Cache.RedisConfig()
		if string(cfg.Cache.Driver) != driver || cfg.Cache.MemoryCapacity != 50 || sql.Table != "cached_values" || sql.Namespace != "tenant" || sql.MaxEntryBytes != 2048 || sql.OperationTimeout != 2*time.Second || redis.Namespace != "tenant" || redis.MaxEntryBytes != 2048 || redis.OperationTimeout != 2*time.Second {
			t.Fatal("cache settings did not reach driver options")
		}
	}
	for _, values := range []map[string]string{
		{"CACHE_DRIVER": "bad"}, {"CACHE_DRIVER": "database"},
		{"CACHE_DRIVER": "memory", "CACHE_MAX_ENTRY_BYTES": "-1"}, {"CACHE_DRIVER": "memory", "CACHE_MEMORY_CAPACITY": "-1"}, {"CACHE_DRIVER": "memory", "CACHE_MEMORY_MAX_BYTES": "-1"}, {"CACHE_DRIVER": "redis", "CACHE_OPERATION_TIMEOUT": "-1s"},
	} {
		if _, err := loadTestConfig(t, values); err == nil {
			t.Fatalf("accepted %v", values)
		}
	}
}

func TestLoggingChannels(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"LOG_CHANNELS": "stdout, file"})
	if err != nil {
		t.Fatal(err)
	}
	channels := cfg.Logging.Channels(cfg.App.AppName, cfg.App.Environment)
	if len(channels.Default) != 2 || channels.Default[1] != "file" || channels.Channels["file"].Path != "storage/logs/app.log" {
		t.Fatal(channels.Default)
	}
	for _, values := range []map[string]string{
		{"LOG_CHANNELS": "missing"},
		{"LOG_CHANNELS": "stdout,stdout"},
	} {
		if _, err := loadTestConfig(t, values); err == nil {
			t.Fatalf("accepted %v", values)
		}
	}
	if !strings.HasSuffix(channels.Channels["file"].Path, filepath.Join("storage", "logs", "app.log")) {
		t.Fatal("missing hardcoded file channel")
	}
}

func TestTracingEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"TRACING_ENABLED": "true", "APP_NAME": "orders-worker", "TRACING_SAMPLE_RATIO": "1", "TRACING_EXPORT_TIMEOUT": "2s"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Tracing.Enabled || cfg.App.AppName != "orders-worker" || cfg.Tracing.SampleRatio != 1 || cfg.Tracing.ExportTimeout != 2*time.Second {
		t.Fatal("tracing settings not loaded")
	}
	if _, err := loadTestConfig(t, map[string]string{"TRACING_ENABLED": "true", "TRACING_SAMPLE_RATIO": "NaN"}); err == nil {
		t.Fatal("invalid ratio accepted")
	}
}

func TestMetricsConfiguration(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"METRICS_ENABLED": "true", "METRICS_ADDRESS": "127.0.0.1:9091"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.Address != "127.0.0.1:9091" || cfg.Tracing.Enabled {
		t.Fatal("metrics should load independently of tracing")
	}
	for _, address := range []string{"localhost", "localhost:http", ":65536", " 127.0.0.1:9090"} {
		if err := (MetricsConfig{Enabled: true, Address: address}).Validate(); err == nil {
			t.Errorf("accepted invalid address %q", address)
		}
	}
	if err := (MetricsConfig{Address: "unused"}).Validate(); err != nil {
		t.Fatal("disabled metrics should ignore address", err)
	}
}

func TestProcessSelectionIsNotEnvironmentConfiguration(t *testing.T) {
	cfg, err := FromEnvironment(map[string]string{
		"APP_ROLES":                        "unknown,unknown",
		"QUEUE_SCHEDULER_ENABLED":          "invalid",
		"QUEUE_COMMAND_SCHEDULING_ENABLED": "invalid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, Defaults()) {
		t.Fatal("retired process settings changed application configuration")
	}
}

func TestTelemetryServiceIdentity(t *testing.T) {
	for _, mode := range []struct {
		name             string
		metrics, tracing bool
	}{
		{name: "disabled"}, {name: "metrics", metrics: true},
		{name: "tracing", tracing: true}, {name: "both", metrics: true, tracing: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.App.AppName = "identity-api"
			cfg.Metrics.Enabled, cfg.Tracing.Enabled = mode.metrics, mode.tracing
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			if got := cfg.Tracing.ProviderConfig(cfg.App.AppName).ServiceName; got != "identity-api" {
				t.Fatalf("service name = %q", got)
			}
			cfg.App.AppName = " "
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "APP_NAME") {
				t.Fatalf("blank service name: %v", err)
			}
		})
	}
}

func TestReadinessConfiguration(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"HTTP_READINESS_DEPENDENCIES": "true", "HTTP_READINESS_TIMEOUT": "250ms"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HTTP.ReadinessDependencies || cfg.HTTP.ReadinessTimeout != 250*time.Millisecond {
		t.Fatal("readiness settings not loaded")
	}
	for _, timeout := range []string{"0s", "-1s"} {
		if _, err := loadTestConfig(t, map[string]string{"HTTP_READINESS_DEPENDENCIES": "true", "HTTP_READINESS_TIMEOUT": timeout}); err == nil {
			t.Fatal("invalid readiness timeout accepted")
		}
	}
}

func TestExplicitTransportEnvironment(t *testing.T) {
	values := environment{
		"HTTP_ADDRESS": "127.0.0.1:8181", "HTTP_READ_TIMEOUT": "19s",
		"HTTP_CORS_ALLOW_ORIGINS": "https://client.example", "HTTP_CORS_MAX_AGE": "3m",
		"EVENTS_DRIVER": "gochannel", "EVENTS_SHUTDOWN_TIMEOUT": "17s",
	}
	var http HTTPConfig
	var cors CORSConfig
	var events EventsConfig
	for _, target := range []any{&http, &cors, &events} {
		if err := values.parse(target, ""); err != nil {
			t.Fatal(err)
		}
	}
	if http.Address != "127.0.0.1:8181" || http.ReadTimeout != 19*time.Second || !reflect.DeepEqual(http.CORS, cors) || len(cors.AllowOrigins) != 1 || cors.AllowOrigins[0] != "https://client.example" || cors.MaxAge != 3*time.Minute || events.Driver != "gochannel" || events.ShutdownTimeout != 17*time.Second {
		t.Fatal("transport configs must load their full environment names independently")
	}
}

func TestPublishedApplicationSettings(t *testing.T) {
	cfg, err := FromEnvironment(map[string]string{"APP_NAME": "orders", "CACHE_DRIVER": "memory"})
	if err != nil || cfg.App.AppName != "orders" || cfg.Cache.Driver != "memory" {
		t.Fatal("application names not loaded", err)
	}
	values, err := godotenv.Read("../../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"APP_NAME", "CACHE_DRIVER", "DATABASE_DRIVER", "EVENTS_DRIVER", "MEDIA_BASE_URL"}
	for _, config := range []any{MailConfig{}, PushConfig{}} {
		kind := reflect.TypeOf(config)
		for i := 0; i < kind.NumField(); i++ {
			key := kind.Field(i).Tag.Get("env")
			if key != "" && key != "-" {
				keys = append(keys, key)
			}
		}
	}
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			t.Errorf("missing published setting %s", key)
		}
	}
}
