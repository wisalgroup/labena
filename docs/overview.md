# Meet Labena

Labena is a batteries-included Go framework delivered as an application template.
Build a modular monolith with explicit module registration, HTTP APIs, background
workers and shared capabilities for authentication, persistence, mail,
notifications, storage and observability.

You own the copied application and its dependencies. This documentation follows
the shipped code: ordinary Go constructors, typed handlers and clear ownership
of resources, data and business operations.

## Start building

New to Labena? Follow this short path. Each guide leaves you with something you
can run or change.

1. **[Create an application](creating-applications.md).** Choose your Go module
   and display name; the installer prepares your source and configuration.
2. **[Run the application](getting-started.md).** Start PostgreSQL and Redis, apply
   migrations and check your first HTTP response.
3. **[Create your first module](modules.md).** Register a small Catalog endpoint
   and test it entirely in process, without a database.
4. **[Maintain your application](upgrading.md).** Keep template provenance and
   incorporate upstream fixes into your customized source.

Already running? Use [configuration](configuration.md) for settings and
[local development](local-development.md) for a persistent development stack.

## Find your next task

| I want to… | Read… |
| --- | --- |
| Add an API endpoint | [Routing](routing.md), [handlers](handlers.md), [requests and validation](requests.md) |
| Save data safely | [Entities and queries](database-queries.md), [transactions](database-transactions.md), [migrations](migrations.md) |
| Sign users in | [Authentication and sessions](../internal/modules/identity/README.md) |
| Run work in the background | [Queues and tasks](queues.md), [events](events.md), [schedules](scheduling.md) |
| Send email or a notification | [Mail](mail.md), [notifications](../internal/platform/notification/README.md), [push](../pkg/push/README.md) |
| Upload and serve files | [File storage](filesystems.md), [media assets](../internal/platform/media/README.md) |
| Verify a change | [Testing basics](testing.md), [HTTP and modules](testing-http.md) |
| Run in production | [Deployment](../deployments/README.md), [logging](logging.md), [observability](observability.md) |

## Understand the design

Business modules own their use cases and persistence. App constructs dependencies
and selects the processes to run. Platform capabilities support modules, while
`pkg` holds technical packages that can be reused independently.

Start with the [directory structure](structure.md), then follow the
[application lifecycle](application-lifecycle.md) and
[module registration](module-registration.md). The
[project conventions](conventions.md) are the architecture contract for changes.

Labena uses PostgreSQL for application data, Asynq for Redis-backed tasks and
Watermill for process-local events. These have different guarantees: a local event
is best-effort, queue acceptance is not a SQL commit, and notification intent can
be committed with a business transaction. Each guide explains its boundary.

## Learn through a complete recipe

- **[A persisted HTTP and CLI operation](persistent-workflow.md):** follow the
  same use case through two transports, a service and a repository.
- **[SQL and notification intent](transactional-notifications.md):** commit a
  profile change and its notification intent together.
- **[Account onboarding reactions](onboarding.md):** connect registration events,
  background tasks, notifications and mail.

## Keep the reference close

The [configuration reference](../deployments/configuration.md) lists settings and
defaults. The [API reference](api/README.md) explains how to inspect the
running application's OpenAPI schema and Go packages. See
[support and compatibility](../SUPPORT.md) before adopting or updating the template.

Use the topic groups to browse, **Search** to find a guide, section or API symbol,
and **On this page** to move within a guide. Every page has a Markdown download;
direct links work on ordinary static hosting and with JavaScript disabled.
