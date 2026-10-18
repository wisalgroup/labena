package queue_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/wisalgroup/labena/examples/mail/delivery"
	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
	"github.com/wisalgroup/labena/internal/platform/mail"
	mailintegration "github.com/wisalgroup/labena/internal/platform/mail/integration"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
	"github.com/wisalgroup/labena/pkg/mailview"
)

type mailCapture chan mail.Message

func (c mailCapture) Send(ctx context.Context, message mail.Message) error {
	select {
	case c <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRedisQueuedMailWorkflow(t *testing.T) {
	url := os.Getenv("QUEUE_TEST_REDIS")
	if url == "" {
		t.Skip("set QUEUE_TEST_REDIS to disposable Redis")
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	pool := redis.NewClient(options)
	defer pool.Close()
	queueName := "mail_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	inspector := asynq.NewInspectorFromRedisClient(pool)
	defer inspector.DeleteQueue(queueName, true)
	client := asynq.NewClientFromRedisClient(pool)
	queue := asynqmiddleware.Chain(client.EnqueueContext, asynqmiddleware.Route(asynqmiddleware.Routing{
		Default: queueName, Queues: map[string]int{queueName: 1},
	}))
	delivered := make(mailCapture, 1)
	mailer, err := mail.New(delivered, queue)
	if err != nil {
		t.Fatal(err)
	}
	views, err := mailview.New(config.Defaults().Mail.Brand())
	if err != nil {
		t.Fatal(err)
	}
	info, err := delivery.EnqueueReport(t.Context(), mailer, views, "reader@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if info.Queue != queueName || info.MaxRetry != 3 || info.Timeout != 30*time.Second || len(delivered) != 0 {
		t.Fatal("routing, defaults or deferred-delivery contract failed", info)
	}
	mux := asynq.NewServeMux()
	if err := mailintegration.Register(mailer, dependencies.Targets{TaskRouter: mux}); err != nil {
		t.Fatal(err)
	}
	worker := newWorker(t, pool, mux, asynq.Config{Concurrency: 1, Queues: map[string]int{queueName: 1}, ShutdownTimeout: time.Second})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case message := <-delivered:
		if len(message.To) != 1 || message.To[0].Email != "reader@example.com" || message.Subject != "Your monthly report is ready" || message.HTMLBody == "" || message.TextBody == "" {
			t.Fatal("rendered delivery did not survive Redis")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
