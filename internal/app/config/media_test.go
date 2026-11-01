package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMediaConfiguration(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg, err := FromEnvironment(map[string]string{"MEDIA_BASE_URL": "https://files.example/api/v1/media", "MEDIA_SIGNING_KEY": key})
	if err != nil {
		t.Fatal(err)
	}
	options, err := cfg.Media.LibraryConfig()
	if err != nil || options.BaseURL != "https://files.example/api/v1/media" || len(options.SigningKey) != 32 {
		t.Fatal("media options", err)
	}
	for _, values := range []map[string]string{{"MEDIA_SIGNING_KEY": "secret-invalid"}, {"MEDIA_BASE_URL": "https://user:secret@example.com/media"}} {
		_, err := FromEnvironment(values)
		if err == nil || strings.Contains(err.Error(), "secret") && !strings.Contains(err.Error(), "32-byte secret") {
			t.Fatal("invalid or leaked configuration", err)
		}
	}
}

func TestMediaDefaultRoute(t *testing.T) {
	options, err := (MediaConfig{}).LibraryConfig()
	if err != nil || options.BaseURL != "/api/v1/media" {
		t.Fatalf("base URL = %q, error = %v", options.BaseURL, err)
	}
}
