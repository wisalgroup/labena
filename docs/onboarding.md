# Account onboarding reactions

Identity's welcome flow demonstrates an event, a job, notification content and a
mail view working together. It runs after account creation and is optional:
registration succeeds even if this reaction does not complete.

```text
account committed
  → UserRegistered event (process-local)
  → welcome task accepted by Redis
  → worker reloads the account
  → notification channel intents persisted in PostgreSQL
  → dispatcher and workers deliver each channel
```

Keep required work in the [business transaction](transactional-notifications.html).
The first event hop is local and can be lost on process failure before Redis accepts
the task. Separate event-subscriber processes cannot receive another process's
GoChannel events. The combined app hosts HTTP and these local subscribers together.

## React to the event

An event names a fact; a task requests work. Identity owns both contracts. The
event definition lives in `internal/modules/shared/events`; the task stays in
`internal/modules/identity/jobs`. Identity's `register.go` registers their handlers alongside
HTTP, commands and migrations. No handler is registered in a service or discovered
at runtime.

This is `internal/modules/identity/delivery/events/user_registered.go`:

```go
// Package events reacts to Identity events using borrowed delivery clients.
package events

import (
	"encoding/json"
	"errors"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"
	"github.com/wisalgroup/labena/internal/modules/identity/jobs"
	sharedevents "github.com/wisalgroup/labena/internal/modules/shared/events"
	"github.com/wisalgroup/labena/pkg/asynqx"
)

// UserRegistered queues onboarding work after account creation. GoChannel is local
// and best-effort; Redis acceptance cannot recover an event lost before this handler.
type UserRegistered struct{ Queue asynqx.Client }

func (h UserRegistered) Handle(msg *message.Message) error {
	var event sharedevents.UserRegistered
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return errors.New("identity: invalid user registration event")
	}
	if h.Queue == nil {
		return domain.ErrQueueRequired
	}
	task, err := jobs.NewWelcome(event.UserID)
	if err != nil {
		return err
	}
	_, err = h.Queue.EnqueueContext(msg.Context(), task)
	return err
}
```

The handler borrows the existing queue client. It decodes the event, creates the
native Asynq task and acknowledges success only after enqueue succeeds. See
[task definitions and handlers](queues.html#define-a-task) for the compiled task definition and full job handler.

The welcome job reloads the account, skips deleted or inactive accounts, and
selects eligible channels. Email is omitted when no mailer is provided or the address
is unverified. Completing the job means channel intents were saved; it does not
mean the email was received.

## Define the content once

This is `internal/modules/identity/notifications/welcome.go`:

```go
// Package notifications defines Identity's named notifications and channel content.
package notifications

import (
	"encoding/json"

	"github.com/wisalgroup/labena/internal/modules/identity/mailviews"
	"github.com/wisalgroup/labena/internal/platform/notification"
)

// NewWelcome describes one occurrence per account. Its stable key lets duplicate
// events, jobs and operator replays converge on the same per-channel intents.
// The caller selects eligible channels before enqueueing (for example, omitting
// email when the address is unverified or no mailer is provided).
func NewWelcome(name string) notification.Message {
	email := mailviews.NewWelcome(name)
	return notification.Message{
		Key:      "welcome",
		Type:     "identity.welcome",
		Database: json.RawMessage(`{"title":"Welcome!","body":"Your account is ready."}`),
		Email:    &email,
	}
}
```

`mailviews.NewWelcome` returns ordinary `mailview.Email` content for the shared layout.
There is no base notification class, per-message registration or duplicated
transport contract. Rendering happens before queued delivery so retries preserve
what was accepted. [Mail](mail.html) shows that same renderer without notification
persistence; choose either entry point according to the operation's needs.

The stable `welcome` key identifies one welcome occurrence per recipient. Duplicate
events and operator replays converge on the existing channel intents. A database
notification is stored once. External SMTP delivery can still duplicate after an
uncertain provider result or crash; a persisted key cannot make SMTP exactly once.

## Observe the shipped flow

Follow [installation](index.html) to run the combined application, then create a
user through OTP or social login. The minimal quickstart disables authentication; configure authentication and SMTP before testing signup. Workers and the notification
dispatcher process the committed welcome intents.

Use the existing operator commands with the same profile:

```sh
go run ./cmd/cli notifications deliveries --status sent
go run ./cmd/cli identity welcome '<user-id>'
```

The second command enqueues a deliberate welcome replay; it does not bypass the
normal worker checks. Inspect [queue failures and recovery](queues.html#inspect-and-recover) when work
is pending or failed. Use a sandbox SMTP provider when testing email;
OTP mail remains synchronous and is never placed in this welcome job.

## Verify the complete path

`TestModulesPostgresRedisWorkflow` runs real HTTP OTP signup, the local event
router, a Redis worker and PostgreSQL notification writes. It also checks replay,
recipient access and logout behavior, with captured email instead of an external
provider. Run the required [integration gate](testing-database.html); skipped
backend tests do not establish that this workflow works.

Use [Identity](identity.html) for the detailed account, session and verification
contracts. Keep schedules centralized in `internal/app/schedules.go` when
you add a recurring operation.
