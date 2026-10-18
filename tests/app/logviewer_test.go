package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wisalgroup/labena/internal/app"
)

func TestLogViewerWithoutObservability(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			cfg, db := databaseConfig(t)
			cfg.App.AppName = "MyApp"
			cfg.Logging.DefaultChannels = []string{"stderr"}
			cfg.Tracing.Enabled = false
			cfg.Metrics.Enabled = false
			cfg.Logging.Viewer.Enabled = enabled
			cfg.Logging.Viewer.Username = "operator"
			cfg.Logging.Viewer.Password = "test-only"
			stop := serve(t, cfg, app.Processes{HTTP: true})
			base := "http://" + cfg.HTTP.Address
			r, _ := http.NewRequestWithContext(t.Context(), "GET", base+"/admin/logs/", nil)
			res, _ := requestBody(t, r)
			want := 404
			if enabled {
				want = 401
			}
			if res.StatusCode != want {
				t.Fatal(res.StatusCode)
			}
			if enabled {
				r.SetBasicAuth("operator", "test-only")
				res, body := requestBody(t, r)
				if res.StatusCode != 200 || !strings.Contains(string(body), "Export visible") {
					t.Fatal(res.StatusCode, string(body))
				}
				if !strings.Contains(string(body), "Logs · MyApp") {
					t.Fatal("viewer did not use configured app name")
				}
				r, _ = http.NewRequestWithContext(t.Context(), "POST", base+"/admin/logs/query", strings.NewReader(`{"source":"live","search":"HTTP request"}`))
				r.Header.Set("Content-Type", "application/json")
				r.SetBasicAuth("operator", "test-only")
				res, body = requestBody(t, r)
				var payload struct {
					Entries []struct {
						Channel string `json:"channel"`
						Raw     string `json:"raw"`
					} `json:"entries"`
				}
				if res.StatusCode != 200 || json.Unmarshal(body, &payload) != nil || len(payload.Entries) == 0 {
					t.Fatal(res.StatusCode, string(body))
				}
				for _, entry := range payload.Entries {
					if entry.Channel != "stderr" || !strings.Contains(entry.Raw, "request_id") {
						t.Fatal(entry)
					}
				}
			}
			stop()
			assertAppConnectionsClosed(t, db, cfg)
		})
	}
}
