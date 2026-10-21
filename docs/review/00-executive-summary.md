# Executive summary

Review date: 2026-10-11. Subject: the current `labena` working copy. **Original review baseline: production files were not changed during review.**

The subsequent approved implementation is recorded in [07 — Implementation record](07-refactoring-roadmap.md#implementation-record--2026-10-11). The assessment and findings below intentionally preserve the evidence that motivated it.

## Assessment

The framework has a credible, well-tested application foundation. It already implements much of the simplicity requested: explicit module construction/registration, a four-file app package, owner service/repository contracts, shared business entities, independently constructible platform capabilities, native transport APIs and deliberate resource ownership.

It does **not** need another broad rewrite. The most consequential problems are inconsistent boundaries: raw HTTP failure diagnostics, incomplete transaction-effect enforcement, caller-heavy SMTP attachment policy, maintenance outside central scheduling, and divergent configuration validation. Documentation has one confirmed broken assembly fragment and some conflicting conventions. Those problems make the framework harder to trust and teach despite its otherwise coherent design.

Core build, vet, race, documentation and required PostgreSQL/Redis integration checks passed during this review. Four isolated probes reproduced specific gaps. The vulnerability scan found no reachable or imported-package findings; four module-only advisories remain. These results do not establish live-provider compatibility, sustained-load behavior or a public v1 support commitment. See [01](01-repository-assessment.md) for the factual report and limits.

## Target vision

**Construct explicitly. Register locally. Run centrally. Call capabilities directly. Preserve their guarantees.**

Teach configuration, borrowed resources, registration targets, operations and runtime ownership. Keep distinct semantics for SQL transactions, local events, Redis tasks, durable notification intent and external providers. Add a framework abstraction only where it removes a repeated decision or enforces a real guarantee.

Retain every current platform package. Improve responsibilities inside the existing structure, keeping app at four files and nested business-module directories. The intended result is fewer exceptions and developer decisions, not a new framework vocabulary or a mandatory reduction in file count.

## Ranked highest-impact improvements

| Rank | Improvement | Why it matters | Finding / task |
| --- | --- | --- | --- |
| 1 | Make HTTP diagnostics bounded and consistent | Raw downstream error text currently reaches logs even though HTTP bodies are safe | F01 / T2.1 |
| 2 | Enforce external-effect transaction rules for framework publication and app queueing | Prevents accidental effects before a rollback; aligns existing mail/media/notification policy | F02 / T2.2 |
| 3 | Bound SMTP attachments and make requested cleanup predictable | Removes repeated memory/cancellation/failure-path responsibilities from callers | F03 / T2.3 |
| 4 | Centralize subscription retention scheduling | Makes all maintenance cadence discoverable and gives deployment ownership one answer | F04 / T2.4 + T3.2 |
| 5 | Reuse component-owned validation before acquisition | App and standalone users get the same rules and earlier deterministic errors | F05 / T2.5 |
| 6 | Correct retry telemetry | A successful retry must not appear to have exhausted its budget | F06 / T2.6 |
| 7 | Move pure named-storage selection out of app | Independent storage no longer imports the entire application composition graph | F07 / T2.7 + T3.1 |
| 8 | Make canonical examples source-backed and correct | Green site builds currently miss an invalid Resources field and competing contract guidance | F09 / T1.2 + Phase 4 |
| 9 | Establish one authoritative convention source | Prevents future contributors/agents restoring obsolete registration/model/help patterns | F10 / T1.1 |
| 10 | Localize notification SQL using private helpers | Improves maintenance without adding repositories/services/packages; optional lower-priority refinement | F08 / T2.8 |

The detailed findings include evidence, API proposals, trade-offs, impact, compatibility, effort and acceptance criteria in [03](03-platform-harmonization.md). No critical P0 defect was established. The ranking is about expected impact, not a claim that all ten are runtime bugs.

## Key architectural decisions

- Keep `Resources` and `Targets` at construction/registration boundaries; services receive narrow explicit collaborators.
- Keep owner interfaces in module `service/service.go` and `repository/repository.go`, with private implementations and constructors beside related methods. Large use cases can be topic files on the same receiver.
- Keep platform records/contracts local, business entities/type constants shared, and polymorphic authorization explicit in owning adapters.
- Keep native Gin, Bun, Cobra, Asynq, Watermill and Go CDK APIs where they provide useful power. No universal component, async abstraction, service locator or driver registry.
- Keep capability `integration` subpackages, including small mail integration: they separate installation from independently usable core operations and avoid cycles.
- Retain explicit lifecycle/readiness/drain and eager CLI construction. Preserve passwordless Identity/session defaults and approved environment feature enablement.
- Separate ordinary mail queueing from SQL notification intent. Keep dispatcher protocol polling, move retention maintenance to the central schedule, and preserve persisted task/schema contracts.
- Do not restore chat, mobile or realtime broadcasting. Existing WebSocket lifecycle hooks are not a broadcast feature.

## Execution order and principal risks

**Phase 0:** preserve inputs and establish focused safeguards. **Phase 1:** make conventions authoritative and fix learning examples. **Phase 2:** targeted platform fixes, introducing the pruning handler and storage resolver additively. **Phase 3:** migrate consumers, remove the app storage helper and cut pruning over centrally. **Phase 4:** finish source-backed guides/reference/operations documentation. **Phase 5:** verify integrations, resource bounds, active-work shutdown and developer workflows, then reassess release scope.

Documentation examples should validate designs during implementation, not only at the end. The [roadmap](07-refactoring-roadmap.md) has independently reviewable tasks and dependency/rollout order.

Material risks are new rejection of unsafe transaction calls, an SMTP attachment bound/stronger explicit cleanup contract, the exported storage helper removal, and workers-only deployments no longer pruning subscriptions automatically. Each has an explicit migration path. Private notification SQL extraction is optional and should follow behavior safeguards; no schema rewrite is proposed.

Quick wins: correct the stale assembly example, sanitize HTTP logs and fix retry telemetry. They complement rather than replace the reliability work.

## Deliverables index

| Document | Purpose |
| --- | --- |
| [00 — Executive summary](00-executive-summary.md) | Assessment, decisions, priorities and open choices |
| [01 — Repository assessment](01-repository-assessment.md) | Understanding gate, complete package inventory, dependencies/lifecycle, workflows and executed baseline |
| [02 — Framework philosophy](02-framework-design-philosophy.md) | Canonical patterns and justified semantic exceptions |
| [03 — Platform harmonization](03-platform-harmonization.md) | Evidence-backed findings and all-package harmonization matrix |
| [04 — Developer experience](04-developer-experience.md) | Grounded current/target workflows and concrete Go examples |
| [05 — Documentation strategy](05-documentation-strategy.md) | Primary-source benchmarks, full 51-page contents plan and representative mail chapter |
| [06 — Target architecture](06-target-architecture.md) | Package/ownership boundaries, exact structural changes and compatibility consequences |
| [07 — Refactoring roadmap](07-refactoring-roadmap.md) | Phases 0–5, file-level tasks, dependencies, rollout and acceptance |
| [08 — Validation and quality gates](08-validation-and-quality-gates.md) | Subjective baseline/target scores, objective indicators, behavior gates and definition of done |

## Open decisions

No unanswered architectural choice blocks the proposed core harmonization. The blueprint recommends concrete defaults rather than asking the user to redesign every detail.

Before public release, the owner still needs to choose the **public module path**, **private security-reporting destination**, and **supported-version/provider/platform scope**. These are product/support commitments, not values inferable from code. This plan assumes the existing clone-and-customize template model; extracting `internal/platform` into an externally importable framework would be a separate project.

The proposed 20 MiB aggregate raw SMTP attachment default is a reviewable engineering choice with a typed override, not a provider guarantee. Provider-specific limits and real deployment performance/recovery targets require subsequent operational evidence.
