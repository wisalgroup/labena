# Identity implementation plan

Identity owns accounts, verified contacts, external login identities, challenges,
and sessions. Profile, onboarding, discovery, and entitlements belong elsewhere.
Existing UUIDs remain `github.com/google/uuid.UUID`; entities remain Bun mappings.

## Database policy

Use TEXT columns with typed Go constants and application validation. Do not add
PostgreSQL enum types or CHECK constraints. Retain primary keys, foreign keys,
NOT NULL, and uniqueness: these protect relationships and concurrent ownership.
Direct SQL can bypass business validation; writes must go through Identity.
Preserve applied migrations and evolve them with new migrations.

## Phases and review gates

1. **Storage foundation**: add external identities, verification challenges,
   sessions, and refresh-token history; typed records; structural validation;
   PostgreSQL round-trip, uniqueness, FK, and migration tests. Remove existing
   users CHECK constraints through a forward migration. Preserve current account
   behavior and data. No authentication endpoints are enabled in this phase.
2. **Accounts and contacts**: required email and optional phone on users, separate
   nullable verification timestamps, account status and lifecycle timestamps.
   Existing emails stay unverified. Contact changes clear verification. Keep
   profile names temporarily for compatibility until a profile owner exists.
3. **Verification**: separate registration/sign-in unless configured otherwise;
   mature mail/SMS delivery integrations, keyed OTP digests, expiry, attempt
   limits, atomic consumption and account changes, cross-challenge throttling,
   resend behavior, and enumeration-resistant responses. Never return OTPs in
   production responses or logs. Bind sensitive changes to recent authentication.
4. **Sessions**: random opaque refresh credentials, atomic rotation, retained
   consumed-token hashes, reuse revocation, access-token validation, logout,
   session management, and status enforcement. Inject `config.AllowMultipleSessions`
   (default true); when false, lock the user row and revoke other sessions in
   the same transaction as login. Test concurrent logins, not just sequential
   replacement. Switching the policy affects subsequent logins; existing sessions
   require explicit revocation if immediate reconciliation is desired. Choose explicit concurrency and
   lost-response behavior; do not silently allow reuse. Session ID identifies
   the refresh-token family. Preserve history while reuse detection is needed.
5. **Providers and recovery**: use maintained OIDC verification libraries;
   validate issuer, audience, expiry, and nonce/state; require explicit linking
   authorization. No email-based automatic merge; no stored provider access
   tokens for sign-in-only integrations. Recovery must not bypass account status.
6. **Composition and operations**: typed startup configuration, explicit injection,
   API boundaries, scheduled bounded cleanup, safe logs and bounded metrics,
   threat/failure review, full tests, and user-facing documentation.

After every phase review responsibility ownership, API size, failure and concurrent
behavior, privacy, and compatibility. Run focused tests and the repository race/vet
baseline. Record actual results and known blockers below. Do not claim that schema
validation establishes authentication security or that a phase enables a later one.

## Decisions awaiting user input

- Contact reuse after deletion: recommended explicit release/anonymization only.
- Separate registration/sign-in or a combined continue flow.
- Immediate revocation via session checks versus access-token expiry delay.
- Delivery providers, recovery proof, and OIDC client credentials before those
  integrations are enabled. No secrets or usable production defaults in source.

## Progress

Phase 1 complete: four authentication tables, matching Bun records, structural Go
validation, removal of legacy users business CHECK constraints, and validation at
the existing account write boundary. Hashes use BYTEA; timestamps use TIMESTAMPTZ.
`config.AllowMultipleSessions` declares the future session policy; it is not yet
wired to authentication because authentication is not implemented.

Review: no generic framework, new repository wrappers, hidden startup, or automatic
DDL. Existing account fields and registration behavior remain unchanged. Validators
are structural checks, not substitutes for authorization or transactional operations.
No production database was migrated; tests used disposable PostgreSQL 17.

Verification: targeted race tests and vet passed for Identity, shared entities, and
configuration. PostgreSQL tests cover migration repeatability, nullable/UUID/hash
round trips, retained consumed tokens, unique provider subjects/token hashes,
foreign keys, and absence of business CHECK constraints. Full build/race/vet are
blocked by an unrelated Notifications import of delivery/subscribers while the
working tree contains delivery/events. Earlier application tests also used the
old event publisher API; rerun the baseline after those changes settle.

Phase 2 complete: forward migration adds contact verification, optional unique
phone, status, activity, and deletion timestamps without enum/CHECK constraints.
New and legacy accounts are active with unverified emails; active does not mean
verified. Contact fields remain on users. NewUser still requires a name for existing
API compatibility; profile extraction is deferred rather than deleting user data.

Lifecycle: active -> suspended or pending_deletion; suspended -> active or
pending_deletion; pending_deletion -> deleted. Repeating a status is a no-op.
Deleted is terminal and hidden from ordinary Get/List. Contacts remain globally
reserved; no automatic release, anonymization, or cancellation of deletion yet.
This conservative policy can be changed explicitly once retention rules are settled.

Contact methods validate canonical E.164 shape only (not telephone assignment or
reachability). Replacing/removing a contact resets verification. Verification checks
the current destination to reject stale proofs, but these methods are not proof
verifiers: the challenge service must authorize and invoke them atomically.

Repository.Edit locks one account and joins the caller transaction or starts its
own. The callback is in-memory only. It validates before updating explicit columns;
unique constraints protect contact ownership across different accounts. The internal
UserService.ChangeStatus operation uses it. No lifecycle or verification HTTP routes
are exposed. Session revocation must join these transactions when sessions are enabled.

Review: explicit entities and enum values; no base Entity/AggregateRoot dependency,
new generic repository framework, new phone library, or database business checks.
Tests: focused race/vet and PostgreSQL migration, contact round trips, uniqueness,
rollback, concurrent edits, and deletion reservations passed. Full race/vet remain
blocked by the unrelated Notifications subscriber import mismatch.

Phases 3–6 remain pending. Resolve remaining authentication policies and delivery
configuration before implementing their dependent operations.


## Current follow-up

The approved shared Entity now contains only UUID ID, CreatedAt, and UpdatedAt.
User embeds it through Bun; User retains contact/status/deletion invariants.
Constructors return UUID generation errors. AggregateRoot remains unembedded and
its event collection is not a dispatch or durability guarantee.

Phase 3 started with email-only verification primitives and a rate-budget migration,
then paused at the user's request to align Media and Notifications. These verification
components have no application configuration, routes, or SMTP wiring yet and are
not enabled. Their security/concurrency review and verification tests remain pending;
do not treat the phase as complete. SMS is explicitly deferred.

Media and Notifications now follow the same entity/error/service/repository/HTTP
conventions. Forward migrations preserve records, add timestamps and user foreign
keys, and rename media_libraries to media. Module READMEs document API/CLI changes.

Alignment verification: the full `go test -race ./...` and `go vet ./...` now pass.
Disposable PostgreSQL/Redis tests also passed for migration preservation, concurrent
idempotency, transaction rollback, module HTTP handlers, event fanout, queue handling,
and the complete registration-to-notification/media workflow. Other external-driver
suites were not enabled. `govulncheck` is not installed in this environment; the
unfinished verification phase still needs its security validation before activation.


## Email verification and inbox follow-up

Phase 3 email ownership verification is now implemented behind an explicit config
switch, stable HMAC secret and mail/database prerequisites. Its public request and
confirm endpoints issue no login tokens. Identity owns proof rules and its typed
email sender; shared mailview owns presentation. A bounded pruning command runs
through the existing scheduler/worker setup. See internal/identity/README.md for
configuration, peer-IP assumptions, limits and SMTP failure behavior.

Notifications now owns a persistent recipient-scoped inbox with per-occurrence
idempotency and read state. New inbox HTTP routes fail closed until the session
phase supplies authenticated identity; CLI operations are available. Media owns
its ready-message content and atomically records it with its starter Media record.
Email, SMS and push are not routed through a speculative generic dispatcher.

Next: implement session/login and refresh-token policy, then inject the authenticated
recipient into inbox HTTP routes. SMS and provider login remain deferred. Earlier
phase notes above are historical; the previous “no routes/config/wiring” limitation
for email ownership verification is superseded by this follow-up.

Verification for this follow-up: full race tests and vet passed; isolated PostgreSQL
17 and Redis 7 suites passed, including the complete HTTP verification/template
flow using a capture mailer and atomic Media/inbox rollback. Other external-driver
suites were skipped. govulncheck reported no reachable vulnerabilities; four
module-only advisories concern unused OpenPGP and Chi RealIP code paths. No
external email was sent and no production database was migrated by the tests.


## Session implementation (phase 4)

Email OTP login, validated JWT access tokens, opaque rotating refresh tokens,
strict replay revocation, logout/session management, and single-session enforcement
are implemented. Authentication checks active account/session state in PostgreSQL
on every protected request; in-flight admitted requests can finish. Account edits
revoke sessions on contact changes or inactive state, preventing resurrection after
reactivation. No password, OIDC, recovery or SMS flow is implied.

Defaults: 15-minute access, seven-day sliding refresh, 30-day absolute session.
Separate registration, verification and sign-in are retained; clients serialize
refresh and sign in again after ambiguous/lost refresh responses. Hash history lasts
until absolute session expiry; bounded cleanup uses the existing scheduled-command
mechanism. Middleware and resource checks protect own-account, Media and inbox reads.
Public user listing is removed until explicit administrative permissions exist.

The implementation reuses golang-jwt/v5 already in go.mod and the existing database
challenge budgets, transaction manager, mail layout and scheduler. There is no new
auth runtime, DI layer, token cache, key discovery endpoint or refresh grace window.
See internal/identity/README.md for configuration, routes, operational tradeoffs and
client examples. Remaining phases are provider linking/recovery and the broader
final production/operational review.


Phase 4 verification: `go test -race ./...` and `go vet ./...` passed with disposable
PostgreSQL 17 and Redis 7. A further PostgreSQL regression confirms that a failed
successor insert rolls back refresh-token consumption. Full HTTP integration covers
registration, verification, sign-in, own-account/Media/inbox access, refresh and
logout with a capture mailer. Other external-driver suites were not enabled.
`govulncheck` reports zero reachable vulnerabilities and four module-only advisories
outside imported packages. No external email was sent during verification.


## Revised authentication direction: unified email OTP

Email OTP confirmation now signs in an existing active account or creates a verified
passwordless account and session together. No account is created on code request.
Existing unverified accounts are verified by successful sign-in proof; disabled or
deleted accounts remain denied. Purpose separation, shared rate budgets, atomic
session creation and strict refresh replay handling are preserved. Registration
reactions still use the existing process-local event after commit, with an explicit
warning on publication failure. The prior phase-4 eligibility restriction is superseded.

Next phases, in order:
1. Optional password credentials: explicit password signup/login, setting/changing
   passwords, purpose-bound reset proofs, throttling and session revocation.
2. Google and Apple login and explicit provider linking; matching email alone does
   not authorize linking an existing account.
3. Lost-email recovery policy, SMS provider support, and operational review.

Passwords will remain optional. Setting one must not remove OTP sign-in or linked
providers. Each phase retains the same account ID and session lifecycle.


Verification for unified OTP: full race tests and vet passed with disposable
PostgreSQL 17 and Redis 7. New tests cover deferred creation, wrong/expired/reused
proofs, concurrent signup, existing unverified users, a registration between request
and confirmation, disabled/deleted users, changed email, transaction rollback and
post-commit publication failure. HTTP integration confirms authentication and Media
setup for a newly created OTP account. govulncheck found zero reachable vulnerabilities
(four module-only advisories outside imported packages). Other external-driver
suites were not enabled. All mail tests used capture senders; no real email was sent.


## Optional passwords and package simplification

Password signup/login, authenticated first-password setup, current-password change,
and email OTP reset are implemented. Signup creates only after ownership proof.
Password changes revoke all sessions and invalidate older password proofs using
an account credential version. The existing OTP entry point remains available.

Reusable crypto moved out of Identity: `pkg/hasher` (Go bcrypt), `pkg/token`
(session JWT and opaque credentials), and `pkg/otp` (generation/digests). Password
input policy is a shared domain value object. The temporary Argon2 dependency and
unused original bcrypt value-object implementation were replaced with this single
bcrypt path. No existing local development account had a password at the switch.
Identity retains application policy, persistence and transaction ownership; its HTTP
layer still has three handlers and centralized request/response/route files.

New limits: 15 characters minimum, 72 UTF-8 bytes maximum, bcrypt cost 10, bounded
CPU work and persistent attempt budgets. The common-password list is intentionally
a small baseline, not a complete breach corpus. Password loss recovery is supported;
loss of email access, provider linking and SMS remain later work.


Verification after extraction and bcrypt simplification: `go test -race ./...` and
`go vet ./...` passed. Disposable PostgreSQL/Redis suites exercised password signup,
login, set/change/reset, concurrent reset, revoked sessions, stale credentials,
rollback on revocation failure, and persistent guessing budgets. Root package tests
cover bcrypt boundaries/overload, redacted password values, JWT validation, opaque
credentials and purpose-separated OTP digests. Migration upgrade fixtures use their
historical columns rather than the current entity. govulncheck reports zero reachable
vulnerabilities and four module-only advisories outside imported packages. Other
external-driver suites were not enabled. Local rollout and route guards were checked;
no real email was sent. Disposable test containers were removed after verification.


## Google social-login follow-up

Google SDK/ID-token login is implemented through the unified social provider API.
It creates a new verified account or signs into an existing provider-subject link,
using the existing sessions and registration event flow. A single-use nonce challenge
is persisted and consumed before verification; Google handles signature/key rotation
through its existing Go library. Matching email never authorizes linking.

The supplied installed-app client can be tested with `examples/social-login` using
loopback, state and PKCE; browser GIS needs its own Web application client. The API
uses only the public client ID. Credentials are excluded from source/build context.

Still pending: explicit provider linking/unlinking and last-login-method protection,
Apple, lost-email recovery, phone flows, contact changes and durable registration
publication. Google accounts using non-authoritative third-party emails must use
email OTP until an explicit verified linking flow exists. See the Identity README
for the current API and failure behavior; older pending notes above are historical.

Verification: full race tests and vet passed with disposable PostgreSQL/Redis;
Google tests use signed RSA tokens and local signing keys to exercise audience,
issuer, signature, expiry, nonce and presenter rejection. PostgreSQL tests cover
replay, concurrent first login, single-session policy, email conflicts, rollback,
inactive accounts, rate limits, cleanup and HTTP wiring. Govulncheck found zero
reachable vulnerabilities (four module-only advisories outside imported packages).
The running development API served a challenge and rejected an invalid credential.
Real Google consent/sign-in was not performed; it requires the user's browser.
Other external-backend suites were skipped.

## October 5 simplification (supersedes challenge and SQL budget notes above)

Authentication budgets now use the shared Redis GCRA limiter, with optional broad
API middleware and mandatory stricter service policies. PostgreSQL retains per-code
attempt counts. A forward migration removes verification_limits and
social_login_challenges. Social sign-in accepts verified Google ID tokens directly;
client state/PKCE/nonce checks protect the initiating flow. Tokens can be exchanged
again until expiry. See the Identity README for deployment and failure semantics.

## Pre-release baseline and route cleanup

The owner authorized a fresh baseline and deletion of local development data.
Identity now has five complete creation migrations. This supersedes the historical
incremental migration filenames discussed above; released baselines must still be
preserved. HTTP registration now has one entry point with explicit public and
protected routers and optional capability dependencies. Existing URLs are retained.
Further service-flow simplification should preserve authentication invariants and
proceed separately from this schema baseline change.

## Explicit authentication dependency wiring

App assembly now creates verification once and injects it into sessions, passwords
and social sign-in. Those flows no longer construct duplicate verification or reach
through Sessions for it. Repository contracts and sender interfaces were narrowed;
unused proof client metadata and duplicate device validation were removed. Shared
send admission is checked across verification and sign-in against real Redis/SQL.
