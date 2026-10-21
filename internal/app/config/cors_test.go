package config

import (
	"slices"
	"testing"
)

func TestCORSConfiguration(t *testing.T) {
	cfg := Defaults()
	if !cfg.HTTP.CORS.Enabled || len(cfg.HTTP.CORS.AllowOrigins) != 1 || cfg.HTTP.CORS.AllowOrigins[0] != "*" || cfg.HTTP.CORS.AllowCredentials {
		t.Fatal("CORS must allow all origins without credentials by default")
	}
	if !slices.Contains(cfg.HTTP.CORS.ExposeHeaders, "X-Request-ID") || !slices.Contains(cfg.HTTP.CORS.ExposeHeaders, "X-API-Version") {
		t.Fatal("response metadata headers must be readable by browser clients")
	}
	if !slices.Equal(cfg.HTTP.CORS.AllowHeaders, []string{"*"}) {
		t.Fatal("custom request headers must be allowed by default")
	}
	empty := cfg.HTTP.CORS
	empty.AllowOrigins = nil
	if err := empty.Validate(); err == nil {
		t.Fatal("accepted missing origins")
	}
	loaded, err := loadTestConfig(t, map[string]string{
		"HTTP_CORS_ENABLED": "true", "HTTP_CORS_ALLOW_ORIGINS": "http://localhost:3000,https://app.example.com",
		"HTTP_CORS_ALLOW_CREDENTIALS": "true", "HTTP_CORS_MAX_AGE": "1h", "HTTP_CORS_EXPOSE_HEADERS": "X-Request-ID",
	})
	if err != nil {
		t.Fatal(err)
	}
	options := loaded.HTTP.CORS.Options()
	if len(options.AllowedOrigins) != 2 || !options.AllowCredentials || options.MaxAge != 3600 || len(options.ExposedHeaders) != 1 {
		t.Fatal(options)
	}
	options.AllowedOrigins[0] = "changed"
	if loaded.HTTP.CORS.AllowOrigins[0] != "http://localhost:3000" {
		t.Fatal("options mutated config")
	}
	for _, origins := range []string{"null", "https://app.example.com/", "https://app.example.com/path", "https://user:pass@app.example.com", "https://*.example.com", "https://app.example.com?", "https://app.example.com#", "https://app.example.com,*", "https://"} {
		_, err := loadTestConfig(t, map[string]string{"HTTP_CORS_ENABLED": "true", "HTTP_CORS_ALLOW_ORIGINS": origins})
		if err == nil {
			t.Fatalf("accepted invalid origin %q", origins)
		}
	}
	for key, value := range map[string]string{
		"HTTP_CORS_MAX_AGE": "-1s", "HTTP_CORS_ALLOW_HEADERS": "X-Bad\r\nInjected", "HTTP_CORS_ALLOW_METHODS": "get", "HTTP_CORS_EXPOSE_HEADERS": "*",
	} {
		_, err := loadTestConfig(t, map[string]string{"HTTP_CORS_ENABLED": "true", "HTTP_CORS_ALLOW_ORIGINS": "https://app.example.com", key: value})
		if err == nil {
			t.Fatalf("accepted invalid %s", key)
		}
	}
	if _, err := loadTestConfig(t, map[string]string{"HTTP_CORS_ENABLED": "true", "HTTP_CORS_ALLOW_ORIGINS": "*"}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTestConfig(t, map[string]string{"HTTP_CORS_ENABLED": "true", "HTTP_CORS_ALLOW_ORIGINS": "*", "HTTP_CORS_ALLOW_CREDENTIALS": "true"}); err == nil {
		t.Fatal("accepted credentials with wildcard origin")
	}
	for _, headers := range []string{"*", "Authorization,Content-Type,X-Client-Version"} {
		if _, err := loadTestConfig(t, map[string]string{"HTTP_CORS_ALLOW_HEADERS": headers}); err != nil {
			t.Fatalf("rejected request header policy %q: %v", headers, err)
		}
	}
	if _, err := loadTestConfig(t, map[string]string{"HTTP_CORS_ALLOW_HEADERS": "*,Authorization"}); err == nil {
		t.Fatal("accepted wildcard mixed with explicit request headers")
	}
}
