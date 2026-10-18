# Documentation strategy and representative chapter

This is a proposed editorial plan, not a rewrite of the published guides. Existing sources, navigation and generated assets remain unchanged in this review. The goal is to improve trust and progression in an already substantial documentation set.

## What the repository already does well

`docs/navigation.json` selects 51 pages across 12 sections. The site builder checks sources/links and produces generated HTML/Markdown. Guides cover ownership, failure semantics, security and operations—not only happy paths. `examples` and selected documentation snippets are compiled by tests. The installation and profile workflows already point to real application behavior. These are strengths to preserve.

The main evidenced defect is F09: a valid-looking assembly fragment references a nonexistent Resources field. F10 adds conflicting inherited architectural instructions. The mail guide also repeats send/render/queue examples across many sections, while the long Identity README combines onboarding, OTP, sessions, Google and operations. These latter observations are editorial concerns, not broken runtime behavior. A page's length alone is not a reason to split it into many new files.

## Benchmark against primary framework documentation

Sources inspected 2026-10-11. These are comparisons of documentation techniques, not proposals to port those frameworks' runtime designs.

| Framework | Observed documentation technique | Applicable improvement here | What not to imitate |
| --- | --- | --- | --- |
| Laravel | Installation leads into configuration/directory structure, architecture concepts, common capabilities and deeper references | Put the first successful task before architecture explanation; make mail, notifications and queues cross-link at the choice point | Global facades, inheritance or framework magic solely to resemble PHP. [Official installation and navigation](https://laravel.com/framework/docs) |
| Goravel | Its installation guide offers explicit starter choices and a concrete environment/setup/run sequence | Clearly distinguish quickstart, full development and component-only use; give exact prerequisites and observable success | Multiple new starter distributions or a facade layer without an actual product need. [Official installation](https://www.goravel.dev/getting-started/installation.html) |
| Spring Boot | The documentation overview separates first steps, upgrading, development, features, production and reference appendices | Separate tutorial, how-to, conceptual explanation and exhaustive reference; make production readiness and support scope discoverable | Runtime auto-configuration/container complexity to mimic a documentation benefit. [Official documentation overview](https://docs.spring.io/spring-boot/documentation.html) |
| Rails | The getting-started guide builds a continuing application example and links to focused component/deeper guides | Use one real account/profile workflow across HTTP, SQL, CLI, task and notification chapters; introduce each boundary when needed | Generators or conventions that hide behavior the Go template currently makes explicit. [Official getting-started guide](https://guides.rubyonrails.org/getting_started.html) |

Inference from these sources: the transferable quality is progressive disclosure and a reliable path from first success to production operations. A framework does not need a large runtime abstraction hierarchy to offer that experience.

## Authority and source policy

1. `docs/conventions.md` is the canonical design contract. Approved review decisions are folded into it, not maintained indefinitely as a second competing standard.
2. `docs/navigation.json` remains the published navigation/source map. Do not hand-edit generated `docs/markdown` or HTML.
3. A capability has one authoritative usage chapter. Existing package READMEs selected as source—Identity, Media, Notifications and Push—remain the source until there is a specific relocation reason. Short other READMEs explain purpose/ownership and link to that guide.
4. Exported Go comments own exact method semantics. The config reference owns defaults/env names. Examples own executable code. Guides explain how these fit together instead of duplicating every signature/default in several places.
5. Historical `docs/plans`, `docs/research` and `REWRITE_PLAN.md` stay identifiable as records. A dated result is not a current CI guarantee or current architectural instruction.
6. Review artifacts stay out of beginner navigation; link the approved decisions from contributor guidance after implementation. This nine-document blueprint is an execution resource, not nine new runtime concepts.

## Complete proposed table of contents

Keep all existing 51 page slugs and their navigation source mappings. Reorder content within pages and link at decision points. No wholesale page proliferation, URL migration or theme redesign is required.

| Section | Pages, in intended learning order | Required page-level progression |
| --- | --- | --- |
| Getting started | Installation (`index`); Configuration (`configuration`); Your first module (`modules`); Local development (`local-development`); Troubleshooting (`troubleshooting`) | Requirements → selected profile → run/migrate/request → expected response → follow source → next step. Full-feature setup is a distinct path |
| Core concepts | Directory structure (`structure`); Project conventions (`conventions`); Entry points (`entry-points`); Module registration (`module-registration`); Dependency injection (`dependency-injection`); Shared types (`shared-types`); Console commands (`console`) | Five concepts → ownership map → canonical module → growth → legitimate exceptions. Commands use native Cobra and full app construction |
| HTTP | Routing (`routing`); Handlers (`handlers`); Requests & validation (`requests`); Responses (`responses`); Middleware (`middleware`); Errors & logging (`logging`) | Register → decode bounded input → authorize → invoke → DTO/Problem → error/log policy. Explicit native binder/body-limit escape hatches |
| Database and state | Connections (`database`); Entities and queries (`database-queries`); Transactions (`database-transactions`); Migrations (`migrations`); Seeders (`seeders`); Cache (`cache`) | Borrow pool → owner repository → same-context transaction → schema lifecycle → optional cache. Do not invent generic repositories |
| Identity | Identity and authentication (`identity`) | Start with a flow decision table; anchored subsections: registration/profile, OTP sign-in, verification, sessions/refresh/replay, Google, push/logout, operational recovery. Keep one source, add a compact local contents map |
| Capabilities | Mail (`mail`); Templates (`templates`); Notifications (`notifications`); Push (`push`); File storage (`filesystems`); Media assets (`media`) | Capability choice → minimum independent construction → normal app usage → cross-component workflow → operational semantics → extension |
| Background work | Queues and tasks (`queues`); Writing tasks (`queue-jobs`); Events and subscriptions (`events`); Recurring schedules (`scheduling`); Workers (`workers`); Failures and retries (`failed-jobs`) | Choose durability → define payload/handler → register → enqueue → central schedule → run/inspect/recover. Local events are explicitly not durable |
| Workflows | A persisted HTTP and CLI operation (`persistent-workflow`); SQL and notification intent (`transactional-notifications`); Account onboarding reactions (`onboarding`) | One real operation per page, diagram of guarantees, linked source files and executed test, rollback/failure scenario |
| Operations | Deployment (`deployment`); Application lifecycle (`application-lifecycle`); Observability (`observability`) | Process roles → required storage/secrets → readiness → drain → recovery → bounded signals. Mark verified environments and untested providers |
| Testing | Testing basics (`testing`); HTTP and modules (`testing-http`); Databases and transactions (`testing-database`); Tasks and events (`testing-async`) | Boundary selection → focused test → integration prerequisites → race/failure behavior → explicit skips. External tests for app/business service/repository |
| Reference | Configuration reference (`configuration-reference`); API reference (`api-reference`); Release readiness (`release-readiness`) | Exhaustive owner/env/default/secret/enablement tables; generated endpoint schemas; dated support/verification matrix |
| Advanced extensions | WebSockets (`websockets`) | Existing upgrade/drain contract, no installed endpoint/hub; protocol-specific responsibilities and multi-process limits. No implied realtime feature |

Within each row, tutorial chapters link forward to the next practical operation and sideways to relevant reference/operations chapters. Example: queued mail links to queues and transaction intent exactly where the reader chooses between Redis acceptance and SQL intent, not only in a generic footer.

### Ownership of every platform package's documentation

| Packages | Authoritative guide(s) |
| --- | --- |
| `api`, `api/middleware`, `api/response` | Routing/requests/responses/middleware/logging plus Go comments for exact helpers |
| `app`, `lifecycle` | Application lifecycle and entry points |
| `config` | Configuration and configuration reference |
| `dependencies` | Dependency injection and module registration |
| `cli` | Console, scheduling and queue administration sections |
| `mail`, `mail/integration` | Mail; integration installation described there |
| `media`, `media/integration`, `media/migrations` | Media plus migration guide cross-link |
| `messaging`, `messaging/middleware` | Events and observability |
| `notification`, `notification/integration`, `notification/migrations` | Notifications, transactional notifications and scheduling |
| `seeders` | Seeders |
| `testdb` | Testing databases; explicitly not a production capability |
| `internal/reference` | Shared types/reference-syntax explanation plus package comments; no public tutorial needed |

## Writing and example standard

A substantial capability chapter follows nine sections: purpose/concepts; quickstart; configuration/defaults; common operations; integration; advanced/extension; lifecycle/concurrency/errors; pitfalls/troubleshooting; API/related components. The structure is a content checklist, not a requirement for a tiny private helper to have nine empty headings.

Start with a working common case and what success means. State prerequisites beside the example, including credentials, migration/worker needs, authorization and returned-resource ownership. Use one preferred path first. Introduce alternatives when the reader has a reason to choose them.

Every Go block is one of:

- A **complete example** linked to an actual compiled source file or test fixture.
- A **function-body fragment** with required types, imports and surrounding return signature stated.
- A **proposed API** explicitly unavailable until its roadmap task lands.

Do not imply fragments with `a.resources` compile independently. Do not show unchecked constructor errors or unclosed resources in a copy/paste quickstart. Use fake `.test` destinations and placeholders; never reproduce private config values. Expected failures include their recovery action and whether the prior operation may already have committed or sent.

Defaults are sourced from owner settings; distinguish code defaults, supplied profiles and effective environment. Do not repeat a dozen optional env keys in the first send-email example. Provider-native options belong in the advanced section. Versioned task/event/schema names remain exact.

Diagrams should explain a real boundary—SQL commit versus queue acceptance, startup versus readiness—not decorate every page. Use accessible Mermaid descriptions consistent with the existing docs builder. No new documentation framework is needed.

## Learning paths and checkpoints

| Audience | Path | Observable checkpoint |
| --- | --- | --- |
| New application developer | Installation → first module → persistent workflow → handlers/queries | Registers catalog once; creates/reads a persisted account; finds the owning service/repository without searching unrelated packages |
| Laravel-experienced developer | Conventions → DI/shared types → mail/templates → notifications → queues/events | Understands content builders instead of inheritance, explicit collaborators instead of facades, and SQL intent versus queueing |
| Capability-only user inside template | Structure index → capability independent-use section → lifecycle/error contract | Uses mail, storage, notifier or CLI without importing app; identifies exactly which resources they own |
| Operator | Configuration reference → roles/lifecycle → schedules → observability → troubleshooting | Identifies one scheduler owner, diagnoses pending notification vs Redis task, performs bounded shutdown and safe replay |
| Contributor | Conventions → testing → related canonical workflow → release readiness | Changes one owner, updates consumers/examples, runs the right tests, records actual limitations |

No time-to-completion claim is made yet. Phase 5 measures these paths with a fresh checkout and an unfamiliar developer or a scripted exercise clearly labeled as such.

## Representative chapter: Mail

The following is an editorial specimen using **current exported APIs**. Attachment-hardening statements are explicitly separated as proposed. It should replace overlapping portions of the existing mail guide after approval, not become another competing mail document.

### 1. Purpose and core concepts

Mail sends rendered email immediately or accepts it into Redis for a worker. A **Sender** performs an immediate transport attempt. A **Mailer** adds explicit queueing. A **mail view** describes content and is rendered to HTML and plain text before sending. Module content builders, such as Identity's welcome view, compose with a shared layout; they do not inherit a base class.

Use Mail for ordinary email. Use Notifications when you need model recipients, multiple channels, database read state or intent that commits with a business SQL change.

### 2. Quick start: enqueue an existing report link

Prerequisites: the caller authorizes the destination, a configured `mail.Mailer` and `mailview.Renderer` are supplied, and a worker sharing the queue has `mail.send.v1` registered. This complete example follows [examples/mail/delivery/delivery.go](../../examples/mail/delivery/delivery.go); the checks below also make missing collaborators explicit.

```go
package reportmail

import (
    "context"
    "errors"

    "github.com/hibiken/asynq"
    "go-modular/internal/platform/mail"
    "go-modular/pkg/mailview"
)

// Enqueue borrows collaborators. The caller authorizes the recipient.
func Enqueue(ctx context.Context, mailer mail.Mailer, views *mailview.Renderer, address string) (*asynq.TaskInfo, error) {
    if mailer == nil || views == nil {
        return nil, errors.New("report mail: mailer and views are required")
    }
    content, err := views.Render(mailview.Email{
        Subject: "Your monthly report is ready",
        Title: "Report ready",
        Paragraphs: []string{"Your monthly report is available in your account."},
        Action: &mailview.Action{
            Label: "View report", URL: "https://example.com/reports",
        },
    })
    if err != nil {
        return nil, err
    }
    return mailer.Enqueue(ctx, mail.Message{
        To: []mail.Address{{Email: address}},
        Subject: content.Subject,
        HTMLBody: content.HTML,
        TextBody: content.Text,
    })
}
```

Check the returned error before using TaskInfo. Success means Redis accepted a snapshot of the rendered content. It does not mean SMTP accepted the message or the recipient saw it. The worker does not rerender a later version of the view. The destination and rendered content are stored in Redis; restrict queue/dashboard/backup access accordingly. Never enqueue OTPs or credentials.

### 3. Configuration and defaults

In the normal app, enable mail and supply provider credentials through the selected profile/secret source. App constructs `Resources.Mailer`, `Resources.MailViews` and the send handler once. `MAIL_ENABLED` is false in code defaults and the quickstart; the supplied full profile explicitly enables it. Do not confuse those layers.

`MailConfig.SMTPOptions` uses verified STARTTLS. The current default port is 2525; it is not an implicit-TLS selector. Set provider-specific host/port/from/credential values as required. Branding is separate from transport credentials. See the configuration reference instead of copying every optional setting into `.env`.

Disabled mail leaves `Resources.Mailer` nil. Validate that dependency before registering an operation requiring mail. Rendering works without SMTP.

### 4. Common operations

| Call | Success means | Required context/resources |
| --- | --- | --- |
| `mailer.Send(ctx, message)` | Immediate sender accepted its attempt | Sender; no framework SQL transaction |
| `mailer.Enqueue(ctx, message, options...)` | Redis accepted serialized content | Sender, queue and registered worker handler; no framework SQL transaction |
| `views.Render(content)` | HTML/text rendered | Renderer only; no SMTP/queue/network |

Enqueue accepts native Asynq options. Current defaults are three retries and a 30-second attempt. The full encoded payload is bounded to 64 KiB. Readers and `CloseAttachments` cannot be queued. A nil queue returns `ErrQueueUnavailable`; it never falls back to synchronous delivery.

For attachments, use a module job with an authorized storage reference and reopen the reader per attempt. Keep the job payload small; do not serialize readers or local temporary paths. Prefer a report link when an attachment is unnecessary.

### 5. Integration with other components

Module constructors borrow app resources and pass `mail.Sender` to immediate-only services or `mail.Mailer` where queueing is needed. The module owns content in `mailviews`; the platform owns sending/queue policy.

A business SQL transaction cannot atomically include SMTP or Redis acceptance. If a notification must survive with a database change, use `Notifier.Enqueue` in that transaction. The ledger commits intent first, then workers perform external delivery. This is a different workflow, not a stronger promise from ordinary queued mail.

Identity verification intentionally sends security codes immediately. The welcome job uses notification content and per-channel intent. Do not convert both flows to queueing merely to make them look uniform.

### 6. Independent use and extension

Construct a `mail.Sender`, for example with `mail.NewSMTP(cfg)`, then call `mail.New(sender, nil)` for immediate-only use. For queueing, supply an existing `asynqx.Client`, call `mail.NewHandler(mailer)`, register it on the native mux under `mail.SendTask`, and run the worker you own. Every constructor error must be checked.

This construction requires no app bootstrap, HTTP route, migration or notification recipient. The caller owns transport/queue/worker lifetime. In app assembly, `mail/integration.Register` performs the native handler registration; do not register it again in every business module.

A replacement sender implements `Send(context.Context, mail.Message) error`. Keep provider SDK types behind that implementation. Native Asynq options remain available; no new mail-driver registry is necessary.

### 7. Lifecycle, concurrency and error handling

Mail construction starts no worker and opens no SMTP connection. Each SMTP Send owns its connection. Callers must not concurrently mutate a Message, headers, slices or readers while a call uses them; construct a fresh message/reader per attempt. Borrowed clients must outlive all calls.

Give the operation a meaningful deadline. SMTP acceptance followed by a timeout or failed result write can leave the outcome uncertain. Retrying can send duplicates; task uniqueness cannot make the external effect exactly-once. Worker errors are sanitized for storage/reporting while preserving their cause for policy decisions.

**Current attachment limit:** the SMTP wrapper reads attachment bodies into memory, and the caller must bound them. Prefer caller-owned cleanup with `defer file.Close()`. Current `CloseAttachments` only closes successfully added readers. **Proposed F03 contract:** add a bounded aggregate default, check cancellation before/between reads, and close all explicitly owned readers on every return path. Do not document that future behavior as shipped until implementation and tests land. An arbitrary blocking reader cannot be interrupted merely by checking context.

### 8. Troubleshooting and pitfalls

| Symptom | Check and recovery |
| --- | --- |
| Mailer missing | Enable mail in the effective profile; verify required credentials, then reconstruct the app |
| `ErrQueueUnavailable` | Supply the queue for independent use; use Send explicitly only if synchronous delivery is intended |
| Enqueued task does not execute | Same Redis DB/queue, running worker, mail handler registered/enabled in that worker |
| SQL transaction error | Use post-commit ordinary email when loss is acceptable, or SQL notification intent when durability is required |
| Queued payload too large / attachments rejected | Store the content securely and enqueue a bounded module job/reference, or send a link |
| Provider timeout after submission | Treat delivery as uncertain; choose retry policy with duplicate tolerance |
| Message absent from inbox | SMTP acceptance does not establish inbox placement; inspect provider diagnostics through an authorized channel |

Routine logs must not include content, recipient lists or provider credential text. The framework does not currently implement bounce webhooks, inbox tracking or automatic exactly-once delivery.

### 9. API and related components

Use `go doc ./internal/platform/mail` for exact signatures. Continue with [templates](../templates.md), [queues](../queue-jobs.md), [transactional notification intent](../transactional-notifications.md), [notifications](../../internal/platform/notification/README.md), [observability](../observability.md) and the [mail source example](../../examples/mail/delivery/delivery.go). The app's ownership/registration rules apply to all of them.

## Documentation validation plan

- Keep the current deterministic builder and link checks. Preserve all 51 slugs unless an explicit migration is approved.
- Extend source-backed examples for app Resources assembly, named storage and transaction composition; keep them under examples/external tests, not app or business service/repository packages.
- Record which blocks are compiled and which are fragments. A successful HTML build does not establish Go API correctness.
- Build/generate in a disposable checkout when checking drift, especially while the working copy is dirty. Compare outputs; do not silently overwrite source conventions.
- Verify that each guide's independent-use path avoids app imports unless it is explicitly an app guide, and that prerequisites/ownership/failure semantics are present.
- Pair API changes with their source example, config reference and troubleshooting update in the same reviewable task. Do not postpone all documentation until the final phase; this representative chapter is an early design check.
