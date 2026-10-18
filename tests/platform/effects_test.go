package platform_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/platform/messaging"
	queuemiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
	"github.com/wisalgroup/labena/pkg/database"
)

type effectPublisher struct{ calls int }

func (p *effectPublisher) Publish(string, ...*message.Message) error { p.calls++; return nil }
func (p *effectPublisher) Close() error                              { return nil }

type countedPayload struct{ calls *int }

func (p countedPayload) MarshalJSON() ([]byte, error) { *p.calls++; return []byte(`{"ok":true}`), nil }

func TestPublicationRejectsSQLScopeBeforeEffects(t *testing.T) {
	db, err := database.Open(t.Context(), database.Config{Name: "effects", Driver: database.DriverSQLite, DSN: filepath.Join(t.TempDir(), "effects.db"), PingTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	transactions, err := database.NewTxManager(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	native := &effectPublisher{}
	publisher, err := messaging.NewPublisher(native)
	if err != nil {
		t.Fatal(err)
	}
	marshaled, queued := 0, 0
	task := asynq.NewTask("test.effects", nil)
	result := &asynq.TaskInfo{ID: "accepted"}
	backendError := errors.New("backend error")
	client := queuemiddleware.Chain(func(ctx context.Context, got *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
		queued++
		if got != task || len(options) != 1 || options[0].Value() != "high" {
			t.Fatal("task/options changed")
		}
		return result, backendError
	}, queuemiddleware.RequireNoTransaction())
	checkRejected := func(ctx context.Context, want error) {
		t.Helper()
		for _, publish := range []func() error{
			func() error { return publisher.Publish(ctx, "test", countedPayload{&marshaled}) },
			func() error { return publisher.PublishWithID(ctx, "test", "id", countedPayload{&marshaled}) },
		} {
			if err := publish(); !errors.Is(err, want) {
				t.Fatalf("publish: %v", err)
			}
		}
		if info, err := client.EnqueueContext(ctx, task); info != nil || !errors.Is(err, want) {
			t.Fatalf("enqueue: %v %v", info, err)
		}
	}
	rollback := errors.New("rollback")
	err = transactions.Execute(t.Context(), func(ctx context.Context) error { checkRejected(ctx, database.ErrTransactionActive); return rollback })
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	checkRejected(canceled, context.Canceled)
	if marshaled != 0 || queued != 0 || native.calls != 0 {
		t.Fatal("rejected operation reached serialization or transport")
	}
	if err := publisher.Publish(t.Context(), "test", countedPayload{&marshaled}); err != nil {
		t.Fatal(err)
	}
	if info, err := client.EnqueueContext(t.Context(), task, asynq.Queue("high")); info != result || !errors.Is(err, backendError) {
		t.Fatal("native result/error changed")
	}
	if marshaled != 1 || queued != 1 || native.calls != 1 {
		t.Fatal("accepted operations not forwarded once")
	}
}
