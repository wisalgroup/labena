package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/modules/identity/jobs"
	sharedevents "github.com/wisalgroup/labena/internal/modules/shared/events"
	asynqmiddleware "github.com/wisalgroup/labena/pkg/asynqx/middleware"
)

func TestUserRegisteredQueuesOnlyIdentityAndPropagatesFailures(t *testing.T) {
	event := sharedevents.NewUserRegistered(uuid.New())
	data, _ := json.Marshal(event)
	msg := message.NewMessage(event.ID.String(), data)
	msg.SetContext(t.Context())
	calls := 0
	failure := errors.New("queue unavailable")
	handler := UserRegistered{Queue: asynqmiddleware.EnqueueFunc(func(ctx context.Context, task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
		calls++
		var payload map[string]string
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			t.Fatal(err)
		}
		if ctx != t.Context() || task.Type() != jobs.WelcomeType || len(payload) != 1 || payload["user_id"] != event.UserID.String() {
			t.Fatal("job identity/context changed", payload)
		}
		return nil, failure
	})}
	if err := handler.Handle(msg); !errors.Is(err, failure) || calls != 1 {
		t.Fatal("queue error lost", err, calls)
	}
	for _, payload := range []string{`{}`, `null`, `broken`, `{"user_id":"invalid"}`} {
		if err := handler.Handle(message.NewMessage(uuid.NewString(), []byte(payload))); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid event queued work", calls)
	}
}
