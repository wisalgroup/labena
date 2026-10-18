package platform_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/push"
)

func TestProviderConfigRedactsSecrets(t *testing.T) {
	const secret = "private-config-token"
	for _, cfg := range []any{mail.SMTPConfig{Password: secret}, push.ExpoConfig{AccessToken: secret}} {
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		slog.New(slog.NewJSONHandler(&output, nil)).Info("config", "config", cfg)
		for _, value := range []string{string(data), fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg), output.String()} {
			if strings.Contains(value, secret) {
				t.Fatal("secret exposed", value)
			}
		}
	}
	var smtp mail.SMTPConfig
	if err := json.Unmarshal([]byte(`{"Password":"`+secret+`"}`), &smtp); err != nil || smtp.Password != secret {
		t.Fatal("decoding changed", err)
	}
	var expo push.ExpoConfig
	if err := json.Unmarshal([]byte(`{"AccessToken":"`+secret+`"}`), &expo); err != nil || expo.AccessToken != secret {
		t.Fatal("decoding changed", err)
	}
}
