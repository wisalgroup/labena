# Mail

Send an email now, queue its rendered content for later, or reuse the same content
inside a notification. Business modules supply message content; the platform owns
SMTP and optional Asynq delivery. App constructs `Resources.Mailer` and
`Resources.MailRenderer` once. Neither construction nor CLI help sends mail.

| Part | Responsibility | Example |
| --- | --- | --- |
| Module `mailviews` | Named content builders | `mailviews.NewWelcome(name)` |
| `pkg/mailview` | Escaped HTML, plain text and shared branding | `views.Render(content)` |
| `internal/platform/mail` | Immediate SMTP or queued rendered messages | `mailer.Send` / `mailer.Enqueue` |
| [Notifications](notifications.html) | Recipients, multiple channels and SQL delivery intent | `notifier.Enqueue` |

Start with a local preview to learn the content model without SMTP. Then configure
a sandbox sender and choose immediate or queued delivery.

## Preview without sending

The repository includes real Identity welcome and sign-in views. Run:

```sh
APP_ENV_FILE=.env.quickstart.example go run ./examples/mail
```

Open `storage/app/mail-preview/welcome.html` and `otp.html`. Their `.txt` companions
show the plain-text alternatives. The preview uses configured branding and a sample
code, creates no authentication challenge and never connects to SMTP.

Use `-output /path/to/previews` to select a different directory. Review both HTML
and plain text, desktop and narrow layouts. Browser previews and escaping tests do
not establish compatibility with every inbox: send sandbox messages to the actual
email clients you support before launching a branded campaign.

## Send now or enqueue

Send immediately at the call site:

```go
err := mailer.Send(ctx, message)
```

Or enqueue that message for an existing Asynq worker:

```go
info, err := mailer.Enqueue(ctx, message)
```

These are separate function-body fragments. `mailer` is the borrowed
`internal/platform/mail.Mailer`; `message` is a `mail.Message`. Check errors
before using `info`. A nil queue permits `Send`; `Enqueue` returns `ErrQueueUnavailable`
and never falls back to sending. Services that only send immediately can keep
using the narrower `mail.Sender` contract from the same package.

The app always constructs its mailer and installs `mail.send.v1` in its normal
task mux. SMTP connects only when sending, never during construction or CLI help.
Queued sending requires a worker sharing the queue. Routing, tracing and worker
metrics use the existing Asynq integration. `Resources.Mailer` is available after
normal app assembly; standalone capability callers still provide their own sender.

| Operation | Success means | Transaction behavior |
| --- | --- | --- |
| Mailer.Send | Immediate transport accepted the attempt | Rejects a caller SQL transaction |
| Mailer.Enqueue | Redis accepted the task | Rejects a caller SQL transaction |
| Notifier.Enqueue | Notification intent persisted, subject to commit | Can join the business SQL transaction |

Use [Notifications](notifications.html) for recipient models, database read state,
multiple channels, or delivery intent that must commit with SQL. Ordinary queued
mail does not need a notification recipient, occurrence key, or SQL table.
Identity's OTP mail remains synchronous and is never queued automatically.

## Configure delivery and branding

Settings live in `internal/app/config/mail.go`. Delivery is always configured; rendering and startup do not connect to SMTP.

```dotenv
MAIL_HOST=sandbox.smtp.mailtrap.io
MAIL_PORT=2525
MAIL_USERNAME=your-sandbox-username
MAIL_PASSWORD=your-sandbox-password
MAIL_FROM_ADDRESS=no-reply@example.com
MAIL_FROM_NAME="Labena"
MAIL_BRAND_NAME="Labena"
MAIL_ACCENT_COLOR="#1D4ED8"
MAIL_LOGO_URL="https://placehold.co/80x80/172554/FFFFFF.png?text=L"
MAIL_WEBSITE_URL=https://example.com
MAIL_SUPPORT_EMAIL=support@example.com
MAIL_FOOTER="Account updates, thoughtfully delivered."
```

Keep credentials in ignored `.env` for local runs or a secret manager for deployment. The accent accepts a six-digit hex color; logo, website and action links require HTTPS. Replace the placeholder image and example addresses before real delivery. Brand text remains visible when images are blocked. A renderer with an empty `LogoURL` omits the image entirely.

Tilt also loads an optional, ignored `.env.mail.yaml` Kubernetes Secret named `app-mail` in namespace `go-modular-dev`. Its `stringData` contains the same `MAIL_*` keys. Both the migration container and application reference it. Local `.env` values are not automatically copied into Kubernetes; configure the Secret separately. Never commit this file or bake credentials into an image.

App SMTP uses verified STARTTLS on the configured port (2525 by default) and a
ten-second connection timeout. A username/password pair enables automatic
authentication negotiation; omitting both permits a relay that requires no SMTP
authentication. Supplying only one is rejected before connection. Mailtrap Sandbox captures test mail; switch to a delivery provider and verified sender for real recipients. Sending opens and closes its connection; construction does not connect.

## Send a message

This complete service function borrows the mailer and sets a deadline:

```go
package service

import (
	"context"
	"time"

	"github.com/wisalgroup/labena/internal/platform/mail"
)

func SendWelcome(ctx context.Context, mailer mail.Sender, recipient string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return mailer.Send(ctx, mail.Message{
		To:       []mail.Address{{Email: recipient}},
		Subject:  "Welcome",
		TextBody: "Your account is ready.",
		HTMLBody: "<p>Your account is ready.</p>",
	})
}
```

A message requires a To recipient, subject, and at least one body. Providing both bodies creates plain text with an HTML alternative. `From` overrides the configured sender when provided. `CC`, `BCC`, and `ReplyTo` accept address slices.

Use [mail views](#render-an-email) to render variable HTML with escaping. Do not concatenate untrusted text into HTML bodies.

## Render an email

The app supplies a shared renderer as `Resources.MailRenderer`. A module passes that
specific dependency to the service that renders messages. This function-body
fragment assumes `views`, `mailer`, a destination `address` and a caller `ctx`:

```go
content, err := views.Render(mailview.Email{
    Subject: "Your report is ready",
    Title: "Report ready",
    Paragraphs: []string{"Your monthly report is available in your account."},
    Action: &mailview.Action{
        Label: "View report",
        URL: "https://example.com/reports",
    },
})
if err != nil {
    return err
}
return mailer.Send(ctx, mail.Message{
    To: []mail.Address{{Email: address}},
    Subject: content.Subject,
    HTMLBody: content.HTML,
    TextBody: content.Text,
})
```

Import `github.com/wisalgroup/labena/pkg/mailview` and `github.com/wisalgroup/labena/internal/platform/mail`.
Rendering returns a subject plus both body formats; it performs no delivery.
Use the destination validated and authorized by your operation.

| `mailview.Email` field | Use |
| --- | --- |
| `Subject` | Required, single-line subject |
| `Title` | Required visible heading |
| `Preheader` | Inbox preview text; never put an OTP or secret here |
| `Eyebrow` | Short contextual label above the title |
| `Paragraphs` | Escaped text paragraphs |
| `Code` | Optional value, label and hint; value must be nonempty, single-line and at most 32 bytes |
| `Action` | Optional button label and absolute HTTPS URL, with a plain-text link |
| `Note` | Optional supporting or security note |

All text is escaped in HTML. Raw HTML and CSS are intentionally not accepted by
this content model. This does not sanitize an arbitrary `mail.Message.HTMLBody`
that you construct yourself.

## Define a module mail view

Keep named content beside its owning module. There is no base class or view registry.
This is the existing **File:** `internal/modules/identity/mailviews/welcome.go`:

```go
// Package mailviews defines Identity email content for the default branded layout.
package mailviews

import (
	"strings"

	"github.com/wisalgroup/labena/pkg/mailview"
)

// NewWelcome is Identity's mailable content builder. The shared Renderer supplies
// layout, logo, colors, footer, escaping and a plain-text alternative. This content
// can be rendered for a local preview, sent directly, or snapshotted by a Notifier.
func NewWelcome(name string) mailview.Email {
	greeting := "Welcome aboard."
	if name = strings.TrimSpace(name); name != "" {
		greeting = "Welcome, " + name + "."
	}
	return mailview.Email{
		Subject:   "Welcome to your account",
		Preheader: "Your account is ready. Here are your next steps.",
		Eyebrow:   "YOUR ACCOUNT",
		Title:     greeting,
		Paragraphs: []string{
			"Your account is ready to use.",
			"Review your profile and keep your sign-in methods up to date. You can review and sign out your active sessions from your account settings.",
		},
		Note: "We will never ask you to share a sign-in code by email.",
	}
}
```

Call `views.Render(mailviews.NewWelcome(user.Name))` for a preview or direct mail.
A notification builder can use `Email: &content` after constructing the same value.
It does not need to render or send it itself: the notifier prepares the snapshot.
Keep eligibility, business policy and recipient selection outside the layout.

## Queue a rendered mail view

This complete, compiled example is `examples/mail/delivery/delivery.go`. Module
mailviews can supply the same `mailview.Email` value instead of building it inline.

```go
// Package delivery demonstrates queued mail using borrowed application clients.
package delivery

import (
	"context"

	"github.com/hibiken/asynq"

	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/mailview"
)

// EnqueueReport renders once before Redis accepts the immutable message snapshot.
// The caller authorizes and validates the destination before invoking this operation.
func EnqueueReport(ctx context.Context, mailer mail.Mailer, views *mailview.Renderer, address string) (*asynq.TaskInfo, error) {
	content, err := views.Render(mailview.Email{
		Subject: "Your monthly report is ready",
		Title:   "Report ready",
		Paragraphs: []string{
			"Your monthly report is available in your account.",
		},
		Action: &mailview.Action{Label: "View report", URL: "https://example.com/reports"},
	})
	if err != nil {
		return nil, err
	}
	return mailer.Enqueue(ctx, mail.Message{
		To:       []mail.Address{{Email: address}},
		Subject:  content.Subject,
		HTMLBody: content.HTML,
		TextBody: content.Text,
	})
}
```

The worker sends the saved subject and bodies without rerendering a changed
view. If From is omitted, the worker transport's configured sender applies.
Enqueue forwards native Asynq options, including `ProcessIn`, `MaxRetry`,
`TaskID` and `Retention`. Its defaults are three retries and a 30-second attempt.
Successful tasks are removed unless a retention option is supplied.

Queued messages support From, To, CC, BCC, ReplyTo, subject, text/HTML bodies,
headers and transport tags. The full encoded payload is limited to 64 KiB.
Attachment readers and `CloseAttachments` are rejected before enqueue. Immediate
sending retains attachment support. For large reports or attachments, use a
module-owned job containing an authorized storage reference and reopen bounded
readers in that job; do not serialize readers or temporary file paths.

Redis stores recipient addresses and rendered content. Restrict queue, dashboard,
backup and inspection access accordingly; never enqueue credentials or OTPs.
Worker errors omit provider response text while preserving wrapped causes for retry
policies. Successful Redis acceptance does not guarantee delivery, and an uncertain
SMTP result or worker crash can cause duplicate email on retry. Task uniqueness
limits enqueue duplication; it cannot make SMTP exactly-once.

### Delay a message or retain its result

This fragment uses the same `mailer`, `message` and caller context as above:

```go
info, err := mailer.Enqueue(ctx, message,
    asynq.ProcessIn(5*time.Minute),
    asynq.MaxRetry(3),
    asynq.Timeout(30*time.Second),
    asynq.Retention(24*time.Hour),
)
if err != nil {
    return err
}
// Persist or return info.ID when the caller needs to inspect task state.
```

Import `time` and Asynq. Delaying changes eligibility, not an exact delivery time.
Use the same queue routing settings on the producer and worker. The normal module
registration already installs `mail.send.v1`; no per-email worker registration is
needed. See [queues](queues.html#configure-options-and-delays) for options and retries.

## Attach files

```go
message.Attachments = []mail.Attachment{
    mail.NewAttachment("invoice.pdf", "application/pdf", bytes.NewReader(pdf)),
}
```

This fragment assumes `message` and bounded `pdf` bytes already exist. Import `bytes`. SMTP buffers attachments under one aggregate raw-byte budget: 20 MiB by default.
`SMTPConfig.MaxAttachmentBytes` overrides it; zero uses the default and negative
values are invalid. App callers can set `cfg.Mail.MaxAttachmentBytes` in Go before
assembly. No new environment variable is required. MIME encoding, message bodies
and library overhead are additional memory. Oversized input returns
`mail.ErrAttachmentsTooLarge` before SMTP delivery, after reading at most the
remaining budget plus one byte.

Seekable readers are rewound. Other readers are consumed, so recreate them before retries. A nil body is skipped, while the filename is required. Empty content type defaults to `application/octet-stream`.

Prefer caller-owned cleanup for file readers, usually a `defer file.Close()` in
the operation that opened them, with close errors handled there. With
`CloseAttachments=true`, SMTP closes every closable input once on every return
path, including validation, read and transport failures. Do not list the same
owned reader twice. Close errors join the returned error, even after SMTP accepted
the message; do not blindly resend on a cleanup failure.

Already canceled calls read nothing. Cooperative cancellation is checked before
and between reads. An arbitrary reader blocked inside `Read` or `Seek` cannot be
interrupted by context alone; give remote readers their own deadlines. SMTP starts
no detached reader goroutine. The Mailer wrapper may reject a transaction before
entering the SMTP transport, so keep caller-owned cleanup when rejection can occur.

## Headers and delivery results

`Headers` adds SMTP headers. Supply only trusted names and values; the wrapper does not define an application allowlist. `Tags` is currently unused by the SMTP implementation.

A successful Send means the SMTP operation succeeded, not that the recipient read
the message or that it reached an inbox. Immediate SMTP sending does not retry;
queued mail uses its configured Asynq retry policy. There is no built-in bounce
handling, tracking or provider event consumer.

## Send through a task

Inject the mailer into a task's service and send using the handler context. Enqueue identifiers and serializable data; do not place readers or client instances in the payload. Workers reconstruct attachment readers for each attempt.

A timeout or connection failure can leave delivery uncertain. Retrying may send another email; queue uniqueness cannot make SMTP effects atomic with task completion. Choose retry behavior appropriate to the message.

Use `logging.FromContext(ctx)` at the boundary responsible for reporting failures. Avoid recording message bodies, credentials, or recipient lists in routine logs. The mail package itself does not install logging or persistent delivery storage.

## Reuse the layout

The shared templates live in `pkg/mailview/templates` and are embedded in the
binary. Change these files for a framework-wide layout change; change a module's
builder for one message. Rebuild after changing templates.

The layout uses semantic headings and paragraphs inside presentation tables,
inline CSS, a mobile enhancement and an Outlook width fallback. Each render
produces HTML and plain text. No JavaScript, remote stylesheet or CSS build step is
required. Branding remains visible when images are blocked; an empty logo URL omits
the image.

Identity's sign-in view uses a code block with an expiry hint and safety note. It
keeps the code out of the subject and preheader. Rendering does not generate,
store or validate a challenge; those rules remain in the Identity service. OTP
mail is sent synchronously after the challenge is prepared, with its own failure
policy. See [authentication](identity.html) for the complete operation.

## Reuse components

Use `mailview.Email`, `Code` and `Action` for semantic content instead of copying
layout HTML. The renderer parses templates once, copies its brand settings and is
safe for concurrent rendering. Changing brand configuration requires constructing
a new renderer (normally an application restart).

For an unrelated HTML template, use Go's `html/template` with embedded files owned
by the module. For plain text, use `text/template`. There is no generic application
template engine to configure; `mailview` is deliberately email-specific.

## Use mail independently

Construct an SMTP sender with `mail.NewSMTP(mail.SMTPConfig{...})`, or provide any
`mail.Sender`. Then compose it with an optional native or instrumented Asynq client:

```go
mailer, err := mail.New(sender, queue)
if err != nil {
    return err
}
handler, err := mail.NewHandler(mailer)
if err != nil {
    return err
}
mux.Handle(mail.SendTask, handler)
```

This function-body fragment imports `mail` from
`github.com/wisalgroup/labena/internal/platform/mail`. The caller owns `sender`, `queue`, the native
Asynq `mux`, and worker execution/cleanup. Use `New(sender, nil)` for immediate-only
mail. Application modules borrow the already configured `deps.Mailer`; they do
not construct another worker or register the platform handler again.

The mail package is under this application's `internal` boundary. These examples
mean “without app bootstrap inside this repository,” not a public package another
unrelated Go module can import. The reusable rendering package `pkg/mailview` has
no dependency on application internals.

Construct an independent renderer with public branding only:

```go
views, err := mailview.New(mailview.Brand{
    Name: "Example",
    AccentColor: "#2563eb",
    WebsiteURL: "https://example.com",
    SupportEmail: "support@example.com",
})
if err != nil {
    return err
}
```

`LogoURL` is optional. The website and optional logo require HTTPS; the accent is a
six-digit hex color. Credentials belong to the SMTP sender, never to the renderer.

## Configure SMTP

```go
mailer, err := mail.NewSMTP(mail.SMTPConfig{
    Host: "smtp.example.com",
    Port: 587,
    Username: username,
    Password: password,
    From: mail.Address{Email: "notifications@example.com", Name: "Example"},
    UseTLS: true,
    InsecureSkipVerify: false,
})
if err != nil {
    return err
}
```

Import `github.com/wisalgroup/labena/internal/platform/mail`. These host and address values are placeholders; credentials should come from your typed configuration. Construction validates settings and creates the client; sending performs network I/O.

`UseTLS` sets the SMTP client's mandatory TLS policy. It is not an implicit-TLS port selector; this wrapper exposes no separate implicit-TLS option. Configure and verify the connection mode your provider requires.

Use `NewSMTP` with explicit verification settings. Mailtrap Sandbox works through SMTP; no Mailtrap API driver is implemented.

## Trace a send

App assembly already wraps the configured mailer with the borrowed tracer. For a separately constructed mailer, use:

```go
mailer := mail.WithTracing(smtpMailer, tracer)
err := mailer.Send(ctx, message)
```

This fragment borrows `smtpMailer`, an OpenTelemetry `tracer`, `ctx` and `message`.
It records timing and failure without mail content or recipient data. It adds no
retries or ownership. Successful submission does not prove delivery to the inbox.
See [observability](observability.html#trace-mail-sends) for the full contract.

## Test without sending real mail

Use a test sender implementing the package-owned `mail.Sender`; do not redefine
that contract in a module. Capture the message passed to `Send`, then assert its
recipient, subject and content in the owning module's external test suite. A
sender returning a chosen error lets you verify the operation's failure policy.

```sh
go test ./pkg/mailview/...
go test ./tests/platform -run 'Mail|SMTP'
go test ./tests/queue -run Mail
go test ./tests/modules -run TestWorkflowGuidesMatchCompiledSources -count=1
```

Rendering tests check escaping and content. Mail tests cover serialization,
limits, transaction rejection and retry classification without contacting your
SMTP provider. The compiled `examples/mail/delivery` example is checked against
the guide. Real provider connectivity and inbox rendering need a separate sandbox
send using your credentials.

## Troubleshoot delivery

| Symptom | Check |
| --- | --- |
| App starts but SMTP cannot send | Construction makes no SMTP connection; verify host, port, credentials and provider TLS mode |
| SMTP accepts mail but no inbox message appears | Sandbox capture, sender verification, provider delivery/bounce reports and spam filtering |
| `ErrQueueUnavailable` | Independent mailer was constructed with no queue; supply one or explicitly call `Send` |
| Queued message never runs | Worker, Redis database, routing and `mail.send.v1` registration |
| Enqueue rejects attachments | Use immediate `Send` or a module task that reopens an authorized file reference |
| Message exceeds 64 KiB | Reduce rendered payload or use a job with an external artifact reference |
| Email appears twice | Uncertain SMTP outcome, worker crash, task replay or repeated original call |
| A template action fails validation | Absolute HTTPS URL and nonempty action label |
| A transaction guard fails | Move SMTP/Redis calls after commit, or persist [notification intent](transactional-notifications.html) in the transaction |

Mail submission, inbox delivery and reading are separate states. The mail facade
has no built-in bounce receiver, tracking pixel or provider event consumer. For
recipient read state use the [database notification channel](notifications.html#read-stored-notifications).
For worker inspection use the [queue guide](queues.html#inspection-and-administration).
