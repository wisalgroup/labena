# Testing tasks and events

Separate the decision to publish from actual delivery. Native task and handler
unit tests run locally; retries, scheduling and Redis state need a real backend.
Local event tests establish only process-local fanout. There is no durable local
event dead-letter or replay store.

## Test task construction and handlers

Construct a task directly and inspect its native type and payload. Call its handler
with a bounded context and check business invocation or `asynq.SkipRetry` for invalid
input. Do not expect worker metadata or a ResultWriter on a newly constructed task.

```sh
go test -race ./internal/modules/identity/delivery/events ./internal/modules/identity/delivery/jobs
go test ./internal/modules/identity/mailviews ./internal/modules/identity/notifications
```

Identity's job tests check current-account lookup, inactive-account skipping,
verified-address channel selection and retryable persistence failures. The event
handler tests reject malformed JSON or missing IDs and check enqueue failures. Mailview tests render
through the default layout and check escaping and plain text.

`tests/modules/workflow_test.go` uses real PostgreSQL and Redis to follow signup
through the event, welcome job, delivery ledger and channel worker. It captures
mail instead of contacting SMTP and checks both JSON database notifications and rendered email delivery.
Set `DATABASE_TEST_POSTGRES`, `QUEUE_TEST_REDIS` and `RATE_LIMIT_TEST_REDIS` to
run that suite without skips. Test retries through a real worker instead of
emulating Asynq internals.

## Reject malformed tasks without infrastructure

This complete test can live in `tests/identity/task_example_test.go`. The malformed
payload must be rejected before the handler accesses its dependencies:

```go
package identity_test

import (
	"errors"
	"testing"

	"github.com/hibiken/asynq"

	jobhandlers "github.com/wisalgroup/labena/internal/modules/identity/delivery/jobs"
	"github.com/wisalgroup/labena/internal/modules/identity/jobs"
)

func TestMalformedWelcomeTask(t *testing.T) {
	handler := jobhandlers.Welcome{}
	task := asynq.NewTask(jobs.WelcomeType, []byte(`{"user_id":"invalid"}`))
	if err := handler.Handle(t.Context(), task); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("expected a terminal payload error, got %v", err)
	}
}
```

That test establishes validation and retry classification. It does not prove task
persistence, worker acknowledgement, or provider delivery. Use real dependencies
for those guarantees.

## Test event fanout

This complete test can live in `internal/platform/messaging/docs_example_test.go`:

```go
package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

func TestIndependentSubscribers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	bus := gochannel.NewGoChannel(gochannel.Config{OutputChannelBuffer: 1}, watermill.NopLogger{})
	defer bus.Close()
	first, err := bus.Subscribe(ctx, "example.created")
	if err != nil {
		t.Fatal(err)
	}
	second, err := bus.Subscribe(ctx, "example.created")
	if err != nil {
		t.Fatal(err)
	}
	sent := message.NewMessage("event-1", []byte(`{"id":"1"}`))
	if err := bus.Publish("example.created", sent); err != nil {
		t.Fatal(err)
	}

	for _, subscription := range []<-chan *message.Message{first, second} {
		select {
		case received, ok := <-subscription:
			if !ok || received.UUID != sent.UUID {
				t.Fatal("missing expected delivery")
			}
			received.Ack()
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
```

This uses raw subscriptions, so the test acknowledges deliveries. Normal app handlers
return an error or nil and let the router acknowledge. Subscribe before publishing;
a bounded channel wait verifies progress without arbitrary sleeps.

## Test real drivers

Start disposable Redis on a dedicated test port in another terminal:

```sh
docker run --rm --name go-modular-queue-test -p 16379:6379 redis:7-alpine
```

Run the existing suites:

```sh
QUEUE_TEST_REDIS=redis://127.0.0.1:16379/1   go test -race -count=1 -v ./tests/app ./tests/queue ./pkg/asynqx/... ./internal/platform/cli
```

Stop that container with Ctrl+C afterward. Use a disposable Redis instance, not
production credentials. These suites use isolated task/queue identities and clean
their work; missing `QUEUE_TEST_REDIS` causes explicit skips.

## Cover the delivery contract

| Scenario | Verify |
| --- | --- |
| Enqueue and execute | Intended handler receives the native task |
| Transient failure | Retry policy and eventual result |
| Invalid payload | No business effect; terminal task state |
| Result retention | Stored bytes and task state match |
| Cancellation | Handler observes cancellation; work actually exits |
| Shutdown | Worker joins before borrowed clients close |
| Scheduling | Registration, queue routing and due publication |
| Trace propagation | Producer link without losing consumer cancellation |

A local fake cannot establish Redis recovery or crash durability. Likewise, one
successful scheduled run does not establish multi-scheduler coordination. The app
requires one scheduler per schedule set.

Run [database tests](testing-database.html) separately for SQL consistency and cache
storage. Neither backend test makes SQL and Redis publication atomic.

## Verify the notification ledger separately

An Asynq task can finish after persisting notification intent while channel delivery
is still pending. Assert the layer you intend to verify:

| Boundary | Evidence |
| --- | --- |
| Welcome handler | Expected channel intents and stable occurrence key |
| Transactional notification | Business change and intents commit or roll back together |
| Dispatcher | Due committed intent is submitted with its delivery ID |
| Channel worker | Leases, retries, and terminal/receipt state follow the provider outcome |
| SMTP/Expo integration | Captured request/content under a fake; live provider behavior requires separate evidence |

Tests should cover duplicate task execution and an omitted versus stable
notification key. Use fixture mail/push senders so ordinary automated tests do not
contact real recipients. A provider's accepted response is not proof of inbox or
device display.

## Verify local event limits and exhaustion

The app uses `messaging.LocalBus` with its `DeliveryMiddleware` outside retry
middleware. Test capacity rejection, bounded handler concurrency, cancellation and
one terminal failure after the configured retry cycle. Failed cycles are logged
and settled; admission capacity must become available again. A native GoChannel
fixture without this adapter can still redeliver Nacked messages, so it does not
verify the app's exhaustion policy.

For reactions requiring restart recovery, use and test a deliberate durable-intent
workflow. Local publication remains best-effort. See [events](events.html).
