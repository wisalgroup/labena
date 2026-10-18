package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/rs/cors"
)

// CORSConfig controls browser cross-origin access. It does not authenticate
// requests or replace CSRF protection when cookie authentication is introduced.
type CORSConfig struct {
	Enabled          bool          `env:"HTTP_CORS_ENABLED" envDefault:"true"`
	AllowOrigins     []string      `env:"HTTP_CORS_ALLOW_ORIGINS" envDefault:"*"`
	AllowMethods     []string      `env:"HTTP_CORS_ALLOW_METHODS" envDefault:"GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS"`
	AllowHeaders     []string      `env:"HTTP_CORS_ALLOW_HEADERS" envDefault:"Origin,Content-Type,Authorization"`
	ExposeHeaders    []string      `env:"HTTP_CORS_EXPOSE_HEADERS" envDefault:"X-Request-ID,X-API-Version"`
	AllowCredentials bool          `env:"HTTP_CORS_ALLOW_CREDENTIALS" envDefault:"false"`
	MaxAge           time.Duration `env:"HTTP_CORS_MAX_AGE" envDefault:"12h"`
}

// Options returns a fresh native configuration. Origins are exact HTTP(S)
// origins or a single '*'; regular expressions and subdomain wildcards are excluded.
func (c CORSConfig) Options() cors.Options {
	return cors.Options{
		AllowedOrigins:   append([]string(nil), c.AllowOrigins...),
		AllowedMethods:   append([]string(nil), c.AllowMethods...),
		AllowedHeaders:   append([]string(nil), c.AllowHeaders...),
		ExposedHeaders:   append([]string(nil), c.ExposeHeaders...),
		AllowCredentials: c.AllowCredentials,
		MaxAge:           int(c.MaxAge / time.Second),
	}
}

func (c CORSConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.AllowOrigins) == 0 {
		return errors.New("config: HTTP_CORS_ALLOW_ORIGINS is required when CORS is enabled")
	}
	for _, origin := range c.AllowOrigins {
		if origin == "*" {
			if c.AllowCredentials || len(c.AllowOrigins) != 1 {
				return errors.New("config: CORS '*' must be the only origin and cannot allow credentials")
			}
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || parsed.String() != origin || strings.ContainsAny(origin, "*# \t\r\n") {
			return errors.New("config: CORS origins must be exact http(s) origins without paths, credentials, queries, or wildcards")
		}
	}
	if c.MaxAge < 0 || c.MaxAge%time.Second != 0 {
		return errors.New("config: CORS max age must be a nonnegative whole number of seconds")
	}
	if len(c.AllowMethods) == 0 {
		return errors.New("config: CORS allowed methods cannot be empty")
	}
	for _, method := range c.AllowMethods {
		if !headerToken(method) || method == "*" || method != strings.ToUpper(method) {
			return errors.New("config: CORS methods must be uppercase HTTP tokens")
		}
	}
	for _, headers := range [][]string{c.AllowHeaders, c.ExposeHeaders} {
		for _, header := range headers {
			if !headerToken(header) || header == "*" {
				return errors.New("config: CORS headers must be explicit HTTP header names")
			}
		}
	}
	return nil
}

func headerToken(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			continue
		}
		return false
	}
	return true
}
