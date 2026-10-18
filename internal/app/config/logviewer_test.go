package config

import "testing"

func TestLogViewerConfiguration(t *testing.T) {
	defaults := Defaults()
	if defaults.Logging.Viewer.Enabled || defaults.Logging.Viewer.BufferEntries != 2000 || defaults.Logging.Viewer.BufferBytes != 8388608 {
		t.Fatal(defaults.Logging.Viewer)
	}
	cfg, err := loadTestConfig(t, map[string]string{"LOG_VIEWER_ENABLED": "true", "LOG_VIEWER_USERNAME": "operator", "LOG_VIEWER_PASSWORD": "test-only", "LOG_CHANNELS": "stderr", "TRACING_ENABLED": "false", "METRICS_ENABLED": "false"})
	if err != nil || !cfg.Logging.Viewer.Enabled || cfg.Tracing.Enabled || cfg.Metrics.Enabled {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"LOG_VIEWER_USERNAME": "bad:user", "LOG_VIEWER_PASSWORD": "", "LOG_VIEWER_BUFFER_ENTRIES": "0", "LOG_VIEWER_BUFFER_BYTES": "67108865"} {
		values := map[string]string{"LOG_VIEWER_ENABLED": "true", "LOG_VIEWER_USERNAME": "operator", "LOG_VIEWER_PASSWORD": "test-only"}
		values[key] = value
		if _, err := loadTestConfig(t, values); err == nil {
			t.Fatal("accepted invalid", key)
		}
	}
}
