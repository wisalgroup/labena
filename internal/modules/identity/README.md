# Identity

Identity provides passwordless accounts, email OTP and Google sign-in, profile
updates, access tokens and revocable sessions. A successful proof either creates
an account or signs into an existing one; clients use the same flow for both.

## Choose a flow

| Task | Start here |
| --- | --- |
| Configure your first sign-in | [Email OTP setup](#email-otp-configuration-and-security) |
| Register or sign in with email | [Request and confirm a code](#sign-in) |
| Complete the user's name | [Update a profile](#update-a-profile) |
| Keep the user signed in | [Refresh](#refresh-and-replay-behavior) and [lifetimes](#understand-token-and-session-lifetimes) |
| Add Google sign-in | [Social login](#google-and-unified-social-login) |
| Protect your own operation | [Protected routes](#protected-routes-and-compatibility) |
| Investigate authentication failure | [Troubleshooting](#troubleshoot-sign-in) |

For a guided end-to-end example, follow the
[persisted HTTP and CLI operation](../../../docs/persistent-workflow.md).

## Email OTP configuration and security

Start with the PostgreSQL/Redis setup in [installation](../../../docs/getting-started.md).
Copy the quickstart profile into your ignored local configuration, retaining its
connection addresses. Configure these additional values:

```dotenv
AUTH_ENABLED=true
AUTH_VERIFICATION_KEY=<first-independent-base64-32-byte-secret>
AUTH_ACCESS_TOKEN_KEY=<second-independent-base64-32-byte-secret>
RATE_LIMIT_REDIS_URL=redis://127.0.0.1:56379/2
MAIL_HOST=<your-SMTP-host>
MAIL_PORT=587
MAIL_USERNAME=<your-SMTP-user>
MAIL_PASSWORD=<your-SMTP-password>
MAIL_FROM_ADDRESS=<your-verified-sender-address>
```

This is a **configuration fragment** for the tutorial Redis port. Use your own
SMTP provider's port and credentials. Mail has no enable switch; it is constructed
by the app and connects when sending. Authentication additionally requires a
working rate-limit Redis connection, even when the broad API limiter is off.

Generate each key independently:

```sh
openssl rand -base64 32
```

Run it twice, save different outputs in the two fields, and keep them stable across
restarts and shared across replicas. Never publish the values. Replacing the OTP
verification key invalidates outstanding challenges and changes rate-limit keys.

Apply migrations and restart with that profile:

```sh
export APP_ENV_FILE=.env
go run ./cmd/cli migrate up
go run ./cmd/app
```

Check [configuration precedence](../../../docs/configuration.md#understand-precedence)
if a shell variable overrides the file. Use a fresh local schema for a first run;
startup never resets or migrates a database automatically.

### Code policy

| Rule | Default | Where it is enforced |
| --- | --- | --- |
| Code format | Six decimal digits, including leading zeros | HTTP and proof processing |
| Lifetime | 10 minutes, including email delivery time | Stored challenge expiry |
| Resend cooldown | 1 minute per normalized address/purpose | PostgreSQL |
| Evaluated attempts | 5 per challenge, including a successful submission | PostgreSQL, even when proof is denied |
| Shared request/attempt budgets | Destination and accepted client IP | Redis |
| Synchronous email-send deadline | 10 seconds | Authentication service |

A new request invalidates earlier unused codes for that address. Use the challenge
ID and code from the **same email request**. Resending does not reset shared Redis
budgets. Only keyed code digests are stored; plaintext codes are not put in logs,
responses, events, jobs or the notification ledger.

SMTP acceptance is not inbox delivery. Delivery failure returns a safe **503**,
creates no account and does not refund abuse budgets. Wait for the cooldown, then
request a fresh code. See [Mail](../../../docs/mail.md) for delivery behavior.

## Email OTP sign-in and bearer sessions

```text
Request code → temporary challenge → confirm email proof
                                      ├─ unused email → create account + session → 201
                                      ├─ active account → create session          → 200
                                      └─ disabled/deleted account → deny
```

Sending a code creates no account. Successful confirmation consumes the proof,
creates or verifies the account, and creates its session in one transaction.
There is no separate signup or standalone email-verification endpoint.

### Sign in

Request a code for an address you control:

```sh
curl -i http://127.0.0.1:8080/api/v1/auth/otp/send \
  -H 'Content-Type: application/json' \
  --data '{"email":"person@example.com"}'
```

Expect **202 Accepted** for both new and existing addresses:

```json
{
  "data": {
    "challenge_id": "<challenge-uuid>",
    "message": "Check your email for a sign-in code."
  }
}
```

Save `data.challenge_id`, read the code from the email, then submit both:

```sh
curl -i http://127.0.0.1:8080/api/v1/auth/otp/verify \
  -H 'Content-Type: application/json' \
  --data '{"challenge_id":"CHALLENGE_UUID","code":"CODE_FROM_EMAIL","platform":"web","device_name":"My browser"}'
```

Replace both placeholders. Send the code as a string so a leading zero survives.
`platform` is `web`, `ios` or `android`; `device_name` is an optional display label,
not a trusted device identifier. An optional `name` applies only to a new account.

Expect **201** for creation or **200** for an existing account. Both use the same
`data` shape:

| Field | Client use |
| --- | --- |
| `access_token` | Send in the Authorization header |
| `refresh_token` | Save securely; replace after every refresh |
| `token_type` | `Bearer` |
| `expires_in` | Remaining access lifetime in seconds |
| `refresh_expires_at` | Refresh deadline, or `null` for no time-based expiry |
| `session_id` | This login's session ID |
| `user` | Public account fields, including `id`, `name`, `email` and `profile_complete` |

The API discloses account existence only after proof succeeds. It does not set
authentication cookies and does not accept tokens in URLs. Identity responses use
`Cache-Control: no-store`.

### Update a profile

After either **200** or **201**, inspect `data.user.profile_complete`. Currently a
nonempty name makes the profile complete. New email accounts can have an empty
name; social accounts use the provider name when available. Returning sign-ins
never overwrite the existing profile.

```sh
curl -i -X PATCH http://127.0.0.1:8080/api/v1/users/me \
  -H 'Authorization: Bearer ACCESS_TOKEN' \
  -H 'Content-Type: application/json' \
  --data '{"name":"Ada Lovelace"}'
```

Expect **200**, the updated public user and `profile_complete: true`. Names are
trimmed and must contain 1–100 characters. At least one editable field is required;
unknown fields, email, account status and user IDs are rejected. The completion
flag is derived from the current profile rather than stored separately.

Read the state on later application launches:

```sh
curl -i http://127.0.0.1:8080/api/v1/users/me \
  -H 'Authorization: Bearer ACCESS_TOKEN'
```

Add future editable fields explicitly to `UpdateProfileRequest` and
`service.UpdateProfileInput`, with validation in the operation/entity as needed.

### Understand token and session lifetimes

| Policy field | Environment key | Default | Meaning |
| --- | --- | --- | --- |
| `AccessTokenLifetime` | `AUTH_ACCESS_TTL` | `168h` (7 days) | Expiry of one access JWT |
| `RefreshTokenLifetime` | `AUTH_REFRESH_TTL` | `0` | Lifetime granted to each new refresh credential; positive values slide on rotation |
| `AbsoluteSessionLifetime` | `AUTH_SESSION_TTL` | `0` | Maximum age from sign-in; activity never moves this deadline |

Access lifetime must be between 1 minute and 7 days. Positive refresh lifetime
must be at least the access lifetime and at most 30 days. Positive session lifetime
must cover the access and positive refresh lifetime and be at most 90 days.
Zero disables only the respective refresh/session time limit.

| Scenario | Result |
| --- | --- |
| Defaults; user returns months later | Access expired; the saved current refresh credential can obtain new tokens |
| Refresh `168h`, session `0`; refresh on day 6 | The replacement refresh credential receives another 7 days |
| Refresh `168h`; no refresh for 8 days | Sign-in required; normal API reads do not renew refresh expiry |
| Session `720h`; continuous activity | Sign-in required after 30 days; refresh cannot extend that deadline |
| Logout or security revocation | Subsequent access and refresh fail regardless of unlimited lifetimes |

Positive token lifetimes are capped by the stored session deadline. Unlimited
expiry is SQL `NULL` and JSON `null`. Configuration changes affect new sessions
and newly issued tokens; existing sessions retain their stored absolute deadline,
and a refresh checks its current stored expiry before issuing a successor.

Unlimited lifetime does not mean automatic client refresh. The client must retain
its refresh credential securely, refresh when needed, and sign in again if that
credential is lost.

### Refresh and replay behavior

```sh
curl -i http://127.0.0.1:8080/api/v1/auth/refresh \
  -H 'Content-Type: application/json' \
  --data '{"refresh_token":"CURRENT_REFRESH_TOKEN"}'
```

Expect **200** with new token fields in `data`. Replace **both** saved credentials.
The response is a token result, without the sign-in response's `user` object.

Rotation consumes the old credential and saves its successor atomically. Tokens
contain 256 bits of randomness; only SHA-256 hashes are stored. Reusing a consumed
credential revokes its session, including the newest access token.

**Serialize refreshes in the client.** Two concurrent refreshes with one token
can cause a rotation followed by replay revocation. If the response is lost, sign
in again instead of replaying the old token. There is no grace period or cached
plaintext successor. An uncertain database commit also requires fresh sign-in.

Consumed hashes remain while a session is usable. Unlimited active sessions
therefore accumulate rotation history until revoked or expired. Replay revocation
commits even though the operation returns an authentication error.

### Manage sessions

All operations below use the current access token:

| Method and route | Result |
| --- | --- |
| `GET /api/v1/auth/sessions?page=1&per_page=20` | Page of active sessions; `current` identifies this login |
| `DELETE /api/v1/auth/sessions/{id}` | Revoke one owned session; **204** |
| `DELETE /api/v1/auth/sessions` | Revoke all owned sessions, including this one; **204** |
| `POST /api/v1/auth/logout` | Revoke the current session; **204** |

```sh
curl -i -X POST http://127.0.0.1:8080/api/v1/auth/logout \
  -H 'Authorization: Bearer ACCESS_TOKEN'
```

Discard the saved credentials after logout. Session `last_used_at` reflects
sign-in/refresh activity, not presence; a device name is an untrusted label.
Sign-in updates the user's `last_active_at`; background refresh does not claim
meaningful account activity.

`domain.AllowConcurrentSessions` defaults to true. Setting it false revokes other
sessions under the account lock during a new sign-in. Concurrent sign-ins cannot
leave two active sessions. Changing the constant alone does not revoke existing
sessions; use explicit revocation to reconcile them.

### Protected routes and compatibility

Register private operations on `targets.AuthenticatedHTTP`. App constructs that
Huma group using Identity's authenticator; middleware verifies the bearer token
and current PostgreSQL account/session state on each request.

Inside a typed handler, obtain the current user ID:

```go
userID, err := auth.CurrentUserID(ctx)
if err != nil {
    return nil, response.Error(ctx, err)
}
```

This **handler fragment** imports platform `api` and `api/response`; use `userID`
for the authorized service operation. For a route containing an account ID, use
`auth.RequireCurrentUser(ctx, id)` before the read. It returns not found on a mismatch
and grants no administrator rights.

`auth.FromContext(ctx)` returns `auth.Principal{UserID, SessionID}` after authentication.
The principal contract lives in `internal/platform/auth`; account/session behavior
and persistence remain owned by Identity.
It is the verified identity for the current operation, not the raw JWT claims or a
role/permission model. A valid principal does not authorize access to every record.

| Boundary | Behavior |
| --- | --- |
| Disabled authentication | Protected routes fail with **401** |
| `/users/{id}` | Only the current user's ID is permitted |
| Notifications/private media | Recipient/owner derived from the authenticated user |
| Forged user headers or query tokens | Do not establish a principal |
| Account suspension, deletion state, contact changes or logout | Subsequent protected requests fail |
| Database outage | Operational failure; never anonymous access |

Reactivating an account does not revive revoked sessions. Requests admitted before
revocation may finish; revocation is not cancellation of in-flight work. Checking
state costs a database read per protected request. Account listing and status
changes remain internal service operations; callers must supply administrative
policy where appropriate.

Use HTTPS beyond loopback. Keep mobile tokens in platform secure storage and
choose browser token custody/BFF design explicitly. Never put bearer credentials
in URLs, logs, analytics or error reports.

### Cleanup and verification

```sh
go run ./cmd/cli identity prune-verification
go run ./cmd/cli identity prune-sessions
```

These operator commands remove bounded batches. Session cleanup removes up to
1000 token rows, then up to 1000 empty revoked/absolutely expired sessions. Active
sessions retain replay history, including when their lifetime is unlimited.

App declares five-minute command schedules when authentication and command
scheduling are enabled. A designated scheduler and workers must run; neither
configuration nor registration executes cleanup by itself. Monitor whether the
batch rate keeps up with expired data. Redis owns limiter-key expiry.

## Google and unified social login

Google exchanges a verified provider ID token for the same application session
used by OTP. Configure the provider alongside authentication:

```dotenv
AUTH_GOOGLE_ENABLED=true
AUTH_GOOGLE_CLIENT_ID=<allowed-audience>.apps.googleusercontent.com
# Optional additional azp presenters, comma-separated without spaces.
AUTH_GOOGLE_TRUSTED_PRESENTER_IDS=<mobile-client>.apps.googleusercontent.com
```

The client ID is the accepted audience. Additional presenters never become extra
audiences. When `azp` exists, it must be a nonempty string matching the audience
client or an explicitly trusted presenter. Tokens without it can pass the other
checks. For a browser Google button, use a Web application client with registered
JavaScript origins; an installed-app client is not interchangeable.

Obtain a Google **ID token** using your client integration, then send JSON:

```sh
curl -i http://127.0.0.1:8080/api/v1/auth/social/google \
  -H 'Content-Type: application/json' \
  --data '{"id_token":"GOOGLE_ID_TOKEN","platform":"web","device_name":"Chrome"}'
```

Expect **201** for a new account or **200** for an already linked provider identity,
with the same tokens-and-user response as OTP. Never send a provider access token,
email address or user ID as a substitute for the ID token. No Google client secret
is needed by this API.

### Account matching and provider validation

| Provider result | Behavior |
| --- | --- |
| Known `(provider, subject)` | Sign into its linked active, verified account |
| New subject, unused authoritative verified email | Create account, provider identity and session atomically |
| New subject, email already belongs to an account | **409**; no automatic linking or merging |
| New account with non-authoritative Google email | **422**; use email OTP instead |
| Suspended/deleted account | Deny access |

Gmail and verified Google Workspace addresses can establish current email
ownership. Other Google-account email addresses may have been verified only in
the past and cannot create an account through this flow. Provider changes do not
overwrite a linked account's local email or name.

The configured verifier uses Google's ID-token validator for signatures, audience,
expiry and cached key rotation, with explicit issuer, subject, issued-at and
presenter checks. It never uses caller-selected issuer/key URLs. The client owns
callback state, PKCE for redirect flows and nonce validation when requested.
Google's direct HTML form-post callback is not supported; submit JSON.

An ID token is reusable until its expiry, subject to account/session policy and
rate limits. There is no one-time provider exchange challenge. No Google
access/refresh tokens are persisted. Protect the ID token as a bearer credential.

### Test with the supplied installed-app credentials

If you have a local installed-app credential file and the API is configured for
its audience:

```sh
go run ./examples/social-login -credentials client_secret_*.json
```

Open the printed URL. The helper uses a loopback callback, state, PKCE and a
locally validated nonce. It saves the API response in `.env.google-session.json`
with owner-only permissions, never prints tokens, and refuses to overwrite an
existing file. Use `-output .env.google-session-2.json` for another attempt.
The Google project's consent/test-user settings must allow your account.

Read [Google server-side verification](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token)
and [native-app authorization](https://developers.google.com/identity/protocols/oauth2/native-app)
when building the client integration.

## Shared rate limiting

Authentication uses shared Redis GCRA budgets: an initial burst replenishes
gradually, not at calendar-hour boundaries. The defaults in
`domain.DefaultAuthRateLimitPolicy()` are:

| Budget | Burst and replenishment per hour |
| --- | --- |
| Code requests per normalized destination | 5 |
| Code requests per accepted client IP | 30 |
| Proof/provider attempts per identity | 10 |
| Proof/provider attempts per peer IP | 50 |

OTP identity budgets cover the destination across purposes. Social verification
checks the peer before provider verification and the verified subject afterward.
Successful and failed admitted work both count. One check may consume budget
before a second denies access; failures do not refund it.

`RATE_LIMIT_REDIS_URL` defaults to Redis database **2**. Keep the namespace and
policies consistent across replicas and separate from queue/cache databases.
Loss, eviction or clearing of limiter data resets budgets; choose persistence and
memory policy accordingly. Logical Redis databases do not isolate server memory
or availability.

The optional broad `/api/v1` limiter is separate:

```dotenv
API_RATE_LIMIT_ENABLED=true
API_RATE_LIMIT_REQUESTS=120
API_RATE_LIMIT_BURST=120
API_RATE_LIMIT_PERIOD=1m
```

It exempts common health routes. Rejections return **429**; the global middleware
includes `Retry-After`. Redis failure returns **503** rather than bypassing
admission. HTTP supplies `api.ClientIP`, which uses the immediate TCP peer by
default. `HTTP_TRUSTED_PROXIES` enables a bounded, validated `X-Forwarded-For` chain
only from explicitly trusted ingress peers. With no trust configured, clients
behind a proxy/NAT share its budget. See the
[ingress contract](../../../deployments/README.md#ingress-and-readiness).

## Boundaries and verification

Authentication repositories intentionally own proof/refresh transactions. Failed
proof counters and replay revocation must commit even when authentication is
denied. Do not wrap these operations in a transaction that rolls back their
security effects. Ordinary profile operations can join a caller-owned transaction.

The proof contains the challenge ID, keyed digest, purpose and policy. The
repository checks and consumes it atomically; a challenge is not an authenticated
session. Wrong, expired, superseded, exhausted or consumed codes create neither
account nor session. Concurrent confirmations cannot create duplicate accounts.

If an account is created between request and proof, confirmation signs into that
account without replacing its profile. A proof bound to an existing account
cannot create another account using its old address after a contact change.
Deleted addresses remain reserved. A different-purpose verification code cannot
be used for sign-in.

Access JWTs use HS256 with issuer, audience, expiry, issued-at, user/session UUIDs
and `typ: at+jwt`. They contain IDs, not contact fields or authorization roles.
The signing key is distinct from the OTP key. Replacing it invalidates access
tokens; valid refresh credentials can obtain tokens signed with the new key.
Overlapping signing-key rotation is not implemented. Share this symmetric key only
with trusted instances able to mint tokens.

## Complete module example: account onboarding

Account creation publishes `UserRegistered` only after commit. Existing-account
sign-in does not publish it again. The canonical walkthrough is
[account onboarding](../../../docs/onboarding.md), which includes complete event,
job, notification and mail-view examples.

### Follow one registration

```text
Committed new account
  → local UserRegistered event
  → identity.welcome.v1 task in Redis
  → reload the current account
  → persist database/email notification intent
  → worker dispatcher enqueues delivery IDs
  → channel handlers attempt delivery
```

The welcome job skips missing, deleted and inactive users. Eligible users receive
database content and, with the configured sender and a verified address, email.
Its stable `welcome` key converges retries for each recipient/channel; saved
content is not overwritten. A replay can add an eligible email channel later.

The initial event remains process-local and best-effort. The SQL ledger cannot
recover a reaction that never reached it. Signup remains committed if publication
fails. Use [transactional notification intent](../../../docs/transactional-notifications.md)
for operations requiring atomic business state and intent. SMTP can still repeat
after an ambiguous external failure.

### Mail views: the mailable equivalent

Identity supplies content through its `mailviews` package; `pkg/mailview` supplies
the common layout, branding, escaping and text alternative. Preview without SMTP:

```sh
APP_ENV_FILE=.env.quickstart.example go run ./examples/mail
```

Open `storage/app/mail-preview/welcome.html` and `otp.html`, plus their text versions.
The sign-in service sends short-lived codes synchronously; they never enter welcome
jobs or durable notifications. See [Mail](../../../docs/mail.md) for rendering and
optional queued ordinary emails.

### Run and inspect the welcome flow

Use the sign-in response's user ID and the same configured environment:

```sh
go run ./cmd/cli identity welcome '<user-id>'
go run ./cmd/cli notifications deliveries --status pending
go run ./cmd/cli notifications list user '<user-id>'
```

The first command enqueues; workers must run before records appear. Signup already
requests this flow, so manual replay is an operator recovery action. Job completion
means intents are saved, not email received. See
[notifications](../../platform/notification/README.md) for delivery inspection.

## HTTP organization

`delivery/http/routes.go` owns typed operation registration, `requests.go` owns
wire inputs and mappings, and `responses.go` selects public outputs. Profile and
authentication handlers call their services; reusable middleware lives in platform
API. Live `/docs` and `/openapi.json` describe the enabled operations.

`media_handler.go`, `notifications_handler.go` and `subscriptions_handler.go`
handle the authenticated user's assets, notifications and push installations.
Their wire inputs live in `requests.go`, output shapes and projections in
`responses.go`, and operation registration in `routes.go`, alongside the existing
account/authentication endpoints. Handlers derive owner/recipient scope from the
platform principal and call the existing platform capabilities.
Push registration calls Identity's AuthService directly so the account lock and
session transaction remain authoritative. Platform media integration separately
serves public/signed content and owns streaming cleanup.

## Module flow and route ownership

All application routes below are prefixed by `/api/v1`:

| Route | Access |
| --- | --- |
| `POST /auth/otp/send`, `POST /auth/otp/verify` | Public proof flow when authentication is enabled |
| `POST /auth/social/{provider}` | Public, explicitly enabled provider |
| `POST /auth/refresh` | Current refresh credential |
| `GET/PATCH /users/me`, `GET /users/{id}` | Bearer session and ownership |
| `/auth/logout`, `/auth/sessions` | Bearer session |
| `/media`, `/media/{id}`, `/media/{id}/download`, `/media/{id}/url`, `/media/{id}/visibility` | Bearer session and user asset ownership |
| `/notifications`, `/notifications/unread-count`, `/notifications/{id}/read` | Bearer session and recipient ownership |
| `/notifications/subscriptions`, `/notifications/subscriptions/{id}` | Bearer session; registration rechecks the session under Identity's account lock |

`register.go` constructs account collaborators and registers transports. App
constructs the AuthService in `internal/app/auth.go`, creates the authenticated
group, and passes the service into `identity.Register`. For architecture examples, see
[module registration](../../../docs/module-registration.md) and
[dependency injection](../../../docs/dependency-injection.md).

## Authentication dependencies

| Owner | Responsibility |
| --- | --- |
| `UserService` | Profile reads/edits, listing and internal account-status operations |
| `AuthService` | OTP/provider proof, token issuance, session management and cleanup |
| `VerificationCodePolicy` | Code lifetime, cooldown and attempts |
| `AuthRateLimitPolicy` | Shared refill/burst budgets |
| `SessionPolicy` | Token/session lifetime and concurrent sign-ins |
| `integrations.SocialProvider` | Configured provider verification |

Service/repository interfaces live in their owner packages; implementations are
private and constructors live beside them. `AuthOptions` borrows repositories,
policies, token issuer, mail renderer/sender, limiter, publisher and provider
implementations. Framework contracts are imported from their owner rather than
redeclared. Construction performs no I/O or background work.

## Organizing a substantial use case

`session.go`, `verification.go` and `social.go` contain methods on the same private
`authService`. A substantial use case can move to its own file without another
service or registration step. Follow the [module growth guide](../../../docs/modules.md#keep-large-use-cases-on-the-same-service).

## Operation inputs

For direct service use, pass trusted transport context explicitly:

```go
result, err := authentication.SignInWithCode(ctx, service.SignInInput{
    Name: "Ada",
    Proof: service.ProofInput{
        ChallengeID: challengeID,
        Code: code,
        ClientIP: trustedClientIP,
    },
    Device: service.Device{Platform: enums.PlatformWeb, Name: "Browser"},
})
if err != nil {
    return err
}
```

This **service-call fragment** assumes a constructed `service.AuthService`, context
and proof values. Imports are Identity `service` and shared `enums`. The caller
consumes `result.User`, `result.Tokens` and `result.Created`; an HTTP adapter maps
Created to 201/200. Client IP comes from the trusted boundary, never a JSON field.
Social sign-in uses `SocialSignInInput{ClientIP, Device}` and a provider credential.

### Push registrations and logout

When Notifier is injected, explicit session revocation detaches matching push
registrations in the same SQL transaction. Revoking all sessions detaches all
registrations for that user. Cleanup failure rolls back revocation; an external
send already admitted cannot be retracted.

Registration validates the active session under the same account lock, so a
request authenticated just before logout cannot reattach a revoked session.
Other account-lifecycle operations must choose their own push-cleanup policy.
See [notification subscriptions](../../platform/notification/README.md).

## Passwordless scope

Implemented: email OTP, Google sign-in, profiles, refresh rotation and session
revocation. Not implemented: passwords, phone/SMS proof, Apple, provider linking or
unlinking, lost-email recovery, roles and public administrative account APIs.
`pkg/hasher` remains available independently for other needs.

## Troubleshoot sign-in

| Result | Check |
| --- | --- |
| OTP **422** | Same challenge/code pair, six digits, expiry, resend replacement and consumed/attempt-exhausted state |
| OTP send **503** | SMTP configuration/reachability or shared rate-limit Redis; wait for cooldown before retry |
| **429** | Destination/peer budgets; resending does not replenish them |
| Protected request **401** | Authorization header, token expiry, enabled auth and current session state |
| Tokens stop working after refresh | Concurrent refresh or replay may have revoked the session; sign in again |
| Google **409** | Existing email is not automatically linked; use its current sign-in method |
| Google **422** on new account | The provider email may not establish current ownership; use OTP |
| Profile **422** | Supply a nonempty trimmed name; omit unsupported fields |
| Welcome is missing | Inspect the local event/queue/ledger stages; sign-in success does not prove delivery |

Use the `X-Request-ID` response header with [observability](../../../docs/observability.md).
Never attach raw codes or tokens to logs while diagnosing.

## Test the behavior

Business service and repository tests live in `tests/identity`. They use public
constructors, isolated PostgreSQL/Redis and captured mail. They cover concurrent
proof consumption and refresh, durable denial state, profile policy, account
changes, expiry, revocation and push/session isolation. HTTP tests cover validation,
no-store behavior and rejection of spoofed forwarding headers.

The [integration testing guide](../../../docs/testing-database.md) supplies disposable
backend setup. `tests/modules/workflow_test.go` follows signup → profile → returning
sign-in → notifications → refresh → logout. Tests do not send external email;
real Google consent is a separate manual client exercise.
