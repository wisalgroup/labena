package app_test

import (
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/wisalgroup/labena/internal/app"
)

func TestQueueDashboardRejectsCrossOriginMutation(t *testing.T) {
	redisURL := os.Getenv("QUEUE_TEST_REDIS")
	if redisURL == "" {
		t.Skip("QUEUE_TEST_REDIS not set")
	}
	cfg, _ := databaseConfig(t)
	cfg.Queue.RedisURL = redisURL
	cfg.Queue.DashboardEnabled = true
	cfg.Queue.DashboardUsername, cfg.Queue.DashboardPassword = "operator", "test-only"
	cfg.HTTP.CORS.AllowOrigins = []string{"https://client.example"}
	cfg.HTTP.CORS.AllowCredentials = true
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(opts)
	t.Cleanup(func() { pool.Close() })
	client, inspector := asynq.NewClientFromRedisClient(pool), asynq.NewInspectorFromRedisClient(pool)
	queue := "csrf_" + uuid.NewString()
	t.Cleanup(func() { inspector.DeleteQueue(queue, true) })
	if _, err := client.Enqueue(asynq.NewTask("review:noop", nil), asynq.Queue(queue)); err != nil {
		t.Fatal(err)
	}
	serve(t, cfg, app.Processes{HTTP: true})
	base := "http://" + cfg.HTTP.Address
	for _, method := range []string{"POST", "OPTIONS", "GET"} {
		r, _ := http.NewRequestWithContext(t.Context(), method, base+"/admin/queues/api/queues/"+queue+":pause", nil)
		r.SetBasicAuth("operator", "test-only")
		r.Header.Set("Origin", "https://client.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if method == "OPTIONS" {
			r.Header.Set("Access-Control-Request-Method", "POST")
		}
		res, body := requestBody(t, r)
		if res.StatusCode != 403 || res.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s: %d %s", method, res.StatusCode, body)
		}
	}
	info, err := inspector.GetQueueInfo(queue)
	if err != nil || info.Paused {
		t.Fatalf("foreign form paused queue: %+v %v", info, err)
	}
	r, _ := http.NewRequestWithContext(t.Context(), "POST", base+"/admin/queues/api/queues/"+queue+":pause", nil)
	r.SetBasicAuth("operator", "test-only")
	r.Header.Set("Origin", base)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	res, body := requestBody(t, r)
	if res.StatusCode != 204 {
		t.Fatal(res.StatusCode, string(body))
	}
	info, err = inspector.GetQueueInfo(queue)
	if err != nil || !info.Paused {
		t.Fatal("same-origin dashboard action failed", err)
	}
}
