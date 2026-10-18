# SQL and notification intent

When a profile change must produce a notification, commit both in the same
PostgreSQL transaction. Persist the delivery intent during the transaction and
let a worker deliver it after commit. This uses existing Identity, transaction and
notification APIs; it needs no new framework abstraction.

| Boundary | What happens |
| --- | --- |
| Service operation | Validate and update the profile |
| Same SQL transaction | Store one database-channel notification intent |
| Commit | Make both changes visible together |
| Worker dispatcher | Submit a due delivery ID to Redis |
| Delivery worker | Create the recipient's readable notification record |

This workflow is useful when losing the notification intent would violate the
operation's requirements. For optional reactions after a successful commit, compare
the [onboarding workflow](onboarding.html).

## One complete operation

`examples/module/profile/update.go` is a compiled composition example. It does not
install an endpoint or change the shipped profile endpoint. A real module can
place this method on its existing service, receiving these collaborators once in
its constructor.

```go
// Package profile demonstrates a business change with transactional notification intent.
package profile

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
	"github.com/wisalgroup/labena/internal/modules/shared/entities"
	"github.com/wisalgroup/labena/internal/platform/notification"
	"github.com/wisalgroup/labena/pkg/database"
)

// Update borrows already constructed collaborators sharing the same database.
// The caller must authorize userID before invoking it. No new route is installed.
// Success commits the profile and notification intent together unless this call
// joins an outer transaction, in which case that caller still owns the commit.
func Update(ctx context.Context, transactions *database.TxManager, users service.UserService, notifier *notification.Notifier, userID uuid.UUID, name string) error {
	return transactions.Execute(ctx, func(ctx context.Context) error {
		user, err := users.UpdateProfile(ctx, userID, service.UpdateProfileInput{Name: &name})
		if err != nil {
			return err
		}
		_, err = notifier.Enqueue(ctx, notification.Recipient{
			Type: string(entities.UserEntityType), ID: user.ID.String(),
		}, notification.Message{
			Type:     "identity.profile_updated",
			Database: json.RawMessage(`{"title":"Profile updated","body":"Your profile changes were saved."}`),
		})
		return err
	})
}
```

Construct `users`, `notifier` and `transactions` with the same database pool. App
already owns that pool and the shared notifier. Pass collaborators into services;
keep the complete `Resources` bundle in module assembly.

The caller authorizes the user ID before invoking the operation. `UpdateProfile`
validates the name and locks the account through its repository. It participates
in the supplied transaction. `Notifier.Enqueue` uses that same transaction to
persist a JSON database-channel intent. Neither operation contacts Redis or SMTP.

## Commit, rollback and delivery

| Outcome | Result |
| --- | --- |
| Both operations succeed | Profile and intent commit together |
| Invalid profile or failed intent insert | Neither change commits |
| Enclosing transaction rolls back | Both changes roll back, even if this function returned nil |
| Worker is stopped or Redis is unavailable | Committed intent waits in PostgreSQL |
| Delivery runs twice for the same intent | One database notification is stored |

Success means the intent is committed, or pending an outer caller's commit. The
notification is not readable until delivery has created the database record. The
existing notification dispatcher submits due IDs to the worker; with the combined
app's workers enabled it runs independently of the cron scheduler. An application
that uses the package independently must arrange dispatch and delivery itself.

The example intentionally omits `Key`, so each successful invocation creates a new
occurrence. Worker retries reuse its persisted key. Repeating the entire business
operation creates another occurrence; use a stable business operation key when
that is undesirable, and design the business write's own retry semantics too.
Notification deduplication alone does not make an entire use case idempotent.

## Choose the appropriate operation

Use `notifier.Enqueue` when intent must commit with business state. The same
pattern can persist email or push content; external delivery still happens later.
Use [mailer.Send or mailer.Enqueue](mail.html) for ordinary email that does not need
SQL intent. Both reject a caller SQL transaction, because SMTP and Redis cannot
join the PostgreSQL commit. Do not make a local event the only record of required
work; [local events](events.html) are best effort.

Sensitive authentication operations, including signup, own their commit
rules and reject outer transactions. Do not put them inside this example. The
profile update is chosen because its contract explicitly supports composition.

## Verify against PostgreSQL

`TestProfileNotificationWorkflowPostgres` executes this exact example with the
real repositories and migrations. It checks success, outer rollback, a failed
notification insert rolling back the earlier profile write, delayed visibility,
and replay of the same delivery ID.

With `DATABASE_TEST_POSTGRES` pointing to a disposable PostgreSQL database:

```sh
APP_ENV_FILE='' go test -race ./tests/modules -run TestProfileNotificationWorkflowPostgres -count=1
```

The test creates and removes an isolated schema. It does not need Redis or a mail
provider. The wider [integration gate](testing-database.html) also covers dispatch
through real Redis. See [notifications](notifications.html) for channel selection,
occurrence keys, recipient isolation and retry limits.

## Diagnose an unexpected result

| Observation | Meaning | Next check |
| --- | --- | --- |
| Profile returned successfully but notification is not listed | Intent may be committed but not delivered | Delivery ledger and dispatcher/worker state |
| Outer transaction rolled back | Earlier nil return was not a commit guarantee | Outer caller's error handling |
| Profile changed but no intent row exists | A call lost the transaction context, bypassed the shared executor or ran outside this pattern | Transaction context propagation and repository execution |
| Operation returns `database.ErrDifferentDatabase` | A collaborator uses a different pool from the active transaction | Construct participating repositories and notifier with the same pool; the executor rejects a foreign pool |
| Repeating the whole request produces another notification | This example intentionally omits a stable key | Choose a business occurrence key if duplicate requests must converge |
| SMTP/Redis transaction guard rejects an operation | An external effect was called inside the transaction | Use notifier intent here and deliver after commit |

Use [notification delivery inspection](notifications.html#inspect-and-retry-deliveries)
for the next step. Do not retry a committed profile change merely because an
optional external channel has not completed.
