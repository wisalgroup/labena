package config

import (
	"errors"
	"time"

	"github.com/wisalgroup/labena/pkg/push"
)

type PushConfig struct {
	Enabled     bool   `env:"PUSH_ENABLED" envDefault:"false"`
	ProjectID   string `env:"PUSH_EXPO_PROJECT_ID"`
	AccessToken string `env:"PUSH_EXPO_ACCESS_TOKEN" json:"-"`
}

func (c PushConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if err := (push.ExpoConfig{AccessToken: c.AccessToken}).Validate(); err != nil {
		return err
	}
	if c.Enabled && c.ProjectID == "" {
		return errors.New("config: PUSH_EXPO_PROJECT_ID required")
	}
	return nil
}

type NotificationConfig struct {
	DispatchInterval time.Duration `env:"NOTIFICATION_DISPATCH_INTERVAL" envDefault:"1s"`
}
