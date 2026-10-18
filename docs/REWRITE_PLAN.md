# Documentation rewrite plan

The active follow-up is the [framework maturity plan](plans/framework-maturity.md),
including every platform package and optional queued mail. The review record below
is historical; it is not evidence that later edits remain synchronized.

A reader should be able to run the application, add a module, and operate its
background work without reading framework internals first. Follow Laravel's
progressive teaching style while documenting the explicit Go APIs actually present.

## Page pattern

1. Explain the concept and when to use it.
2. Show the smallest useful example and its expected result.
3. Explain configuration, registration, and ownership.
4. Add advanced examples only after the basic workflow works.
5. State delivery, failure, and lifecycle limits beside the relevant operation.
6. Link to the next topic instead of repeating its guide.

Label complete files, function fragments, and proposed additions. Keep task names,
queue names, event topics, and subscriber names distinct. Diagrams use text so the
website and downloaded Markdown work without external scripts.

## Sections and acceptance criteria

| Order | Section | Reader outcome | Verification |
| --- | --- | --- | --- |
| 1 | Getting started | Start Redis and the app, send a request, find settings and module files | Commands, profiles, paths, defaults |
| 2 | Application and modules | Follow bootstrap, inject dependencies, register handlers, understand shutdown | Constructors, registration signatures, runtime order |
| 3 | HTTP and logging | Add routes, decode inputs, return responses, use middleware and context logs | Current Gin handlers and logging contracts |
| 4 | Database | Open a pool, query, transact, migrate and seed | Executor ownership, migration paths, command names |
| 5 | Background work | Enqueue and handle tasks; schedule; broadcast and consume events; recover failures | Native Asynq/Watermill APIs, diagrams, guarantees |
| 6 | Other capabilities and observability | Select optional capabilities and follow logs, metrics and traces | Actual wiring versus opt-in examples; operational limits |
| 7 | Testing | Test services and handlers locally, then verify real Redis/database behavior | Test fixtures, environment selectors, executable examples |

## Completion gate

Build the site and downloadable Markdown; validate local links and anchors; run
the documentation builder tests; check examples against source. Report any checks
blocked by unrelated application changes. Do not describe unrun infrastructure
checks as passed. Keep existing published URLs stable even when titles improve.

## Review record

All seven sections have been reviewed against the current implementation. Replaced
placeholder background/testing pages, corrected stale paths and delivery claims,
added file storage, and condensed observability. Existing public page URLs remain.

Verification: 38 pages build; local links and anchors resolve; all 25 complete Go
examples compile in isolated temporary packages; documentation unit examples pass
with the race detector. Embedded-file examples use temporary sample assets. The
SQL test example compiles but skips without a disposable external database.
Statement fragments are checked against their containing APIs, not run as complete
programs. Infrastructure/deployment recipes have not been executed in this rewrite.
