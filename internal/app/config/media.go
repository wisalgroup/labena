package config

import (
	"encoding/base64"
	"errors"
	"github.com/wisalgroup/labena/internal/platform/media"
)

// MediaConfig controls externally reachable media links, never bucket permissions.
type MediaConfig struct {
	BaseURL    string `env:"MEDIA_BASE_URL" envDefault:"http://localhost:8080/api/v1/media"`
	SigningKey string `env:"MEDIA_SIGNING_KEY" json:"-"`
}

func (c MediaConfig) LibraryConfig() (media.Config, error) {
	o := media.Config{BaseURL: c.BaseURL}
	if c.SigningKey != "" {
		key, err := base64.StdEncoding.DecodeString(c.SigningKey)
		if err != nil || len(key) != 32 {
			return o, errors.New("config: MEDIA_SIGNING_KEY must be a base64-encoded 32-byte secret")
		}
		o.SigningKey = key
	}
	return o, o.Validate()
}
