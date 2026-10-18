# Mail

This capability sends rendered email immediately or through the existing Asynq
queue. Use the [mail and mail views guide](../../../docs/mail.md) for the complete
workflow, examples, configuration, attachment ownership and recovery.

| API | Purpose |
| --- | --- |
| `Message`, `Address`, `Attachment` | Provider-independent rendered content and immediate attachment readers |
| `Sender.Send` | One immediate transport attempt |
| `Mailer.Send` / `Mailer.Enqueue` | Explicit choice between immediate delivery and Redis acceptance |
| `NewSMTP(config)` | Validate and construct the SMTP transport; connect only during Send |
| `New(sender, queue)` | Borrow a sender and optional queue without starting work |
| `NewHandler(sender)` | Decode and deliver `mail.send.v1` in an existing Asynq mux |
| `WithTracing(sender, tracer)` | Instrument sends without recording content or destinations |

App installs the integration once and supplies `Resources.Mailer`. Immediate-only
consumers use the narrower `mail.Sender`; modules do not redeclare that contract.
Rendering remains in `pkg/mailview`, and module `mailviews` packages provide named
content. None of these constructors starts a worker or owns the borrowed queue.

A nil queue supports immediate sends; enqueue returns `ErrQueueUnavailable` and
never silently falls back to SMTP. The facade rejects active SQL transaction
contexts. Use [transactional notification intent](../../../docs/transactional-notifications.md)
when delivery intent must commit with business state.

Queued payloads are rendered value snapshots bounded to 64 KiB, with no attachment
readers. Immediate attachments share a 20 MiB raw-byte default limit. Sending and
cleanup errors can follow provider acceptance; assess uncertainty before retrying.
See [attachments](../../../docs/mail.md#attach-files) and
[delivery results](../../../docs/mail.md#headers-and-delivery-results).

The core imports no app assembly, configuration, registration-target or business
module. Its `integration` package connects it to the framework. “Independent use”
means construction without app bootstrap inside this repository; Go's `internal`
import boundary still applies. Notification workers borrow immediate `Send` while
their SQL ledger owns channel retry state.
