# A persisted HTTP and CLI operation

Follow one account from email proof to PostgreSQL, then read it through a console
command. This uses shipped Identity code. Start with the [installation guide](index.html),
then enable authentication and configure SMTP as described in [Identity](identity.html#email-otp-configuration-and-security).
The minimal quickstart has no public account-creation route while authentication is disabled.

## Make the request

Request a sign-in code for an address you control:

```sh
curl -i -X POST http://127.0.0.1:8080/api/v1/auth/otp/send \
  -H 'Content-Type: application/json' \
  -d '{"email":"workflow@example.com"}'
```

Expect 202 and save `data.challenge_id`. Use the code delivered by email:

```sh
curl -i -X POST http://127.0.0.1:8080/api/v1/auth/otp/verify \
  -H 'Content-Type: application/json' \
  -d '{"challenge_id":"CHALLENGE_UUID","code":"CODE_FROM_EMAIL","platform":"web"}'
```

Expect 201 for a new account or 200 for an existing one. Save `data.user.id` and the
access token. Both responses include `data.user.profile_complete`; when false,
submit the name to authenticated `PATCH /api/v1/users/me`. `GET /users/me` exposes
the same state on later app launches. Names are optional during sign-in.

## Complete the profile

Use the access token returned by sign-in:

```sh
curl -i -X PATCH http://127.0.0.1:8080/api/v1/users/me \
  -H 'Authorization: Bearer ACCESS_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada Lovelace"}'
```

Expect **200**, the updated public user, and `profile_complete: true`. Read it back:

```sh
curl -i http://127.0.0.1:8080/api/v1/users/me \
  -H 'Authorization: Bearer ACCESS_TOKEN'
```

The handler derives the account ID from the authenticated principal, rather than
trusting an ID in the request body. Request mapping supplies the service input;
the service/entity still validates the name for non-HTTP callers.

## Follow the operation

| Step | Owner | Behavior |
| --- | --- | --- |
| Assemble | `internal/modules/identity/register.go` | Construct services once and share them with transports |
| Route | `delivery/http/routes.go` | Connect both OTP operations to the auth handler |
| Decode | `delivery/http/requests.go` | Validate bounded JSON and reject unknown fields |
| Decide | `service/session.go` | Validate proof and device input, then request atomic sign-in |
| Persist | `repository/session.go` and `verification.go` | Consume proof, create or verify the account and insert its session in one transaction |
| Respond | `delivery/http/responses.go` | Return tokens and the public user with profile-completion state |

Paths after the first row are relative to `internal/modules/identity`. The shared user
entity lives in `internal/modules/shared/entities/user.go`. Sending a code creates only a
temporary challenge. Invalid proof creates no account. Only newly committed accounts
publish `UserRegistered`; returning sign-ins never replace profiles or publish it
again. Local publication is best-effort and cannot undo a successful signup.

## Read the same account through CLI

Use the same configured environment as the app and the ID returned by sign-in.
Export the same `APP_ENV_FILE` in this terminal before running:

```sh
go run ./cmd/cli identity show '<user-id>'
```

The command calls the same `UserService.Get` used by HTTP reads. It receives a
constructed service directly; it has no application callback or second repository.
Here is its complete source, `internal/modules/identity/delivery/cli/commands.go`:

```go
package cli

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/wisalgroup/labena/internal/modules/identity/service"
)

func New(users service.UserService) *cobra.Command {
	group := &cobra.Command{Use: "identity", Short: "Identity commands"}
	group.AddCommand(&cobra.Command{
		Use: "show USER_ID", Short: "Show an account", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, err := uuid.Parse(args[0])
			if err != nil {
				return err
			}
			user, err := users.Get(cmd.Context(), userID)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(user)
		},
	})
	return group
}
```

CLI execution constructs configured dependencies but does not start HTTP or queue
workers. Help also constructs the application. Console access is an operator
capability: this command does not borrow an HTTP user's permission checks.

An HTTP read requires a bearer session and ownership checks. With authentication
disabled, protected routes fail closed; do not remove that middleware to make the
quickstart read work. Use the CLI above, or enable [passwordless authentication](identity.html).

## Grow your own module

Begin with the [one-file module](modules.html), then add only the pieces required
by a persisted operation: a shared entity, a module migration, a repository and a
service when there are business rules. Interfaces belong to their owner; HTTP and
CLI consume the same service contract. Construct once in `New`, register native
transports in `Register`, and keep recurring timing in app assembly.

Do not copy Identity's authentication transactions into ordinary operations.
Authentication sometimes commits denial state deliberately. Most business changes
can join a caller-owned transaction, as shown in [SQL and notification intent](transactional-notifications.html).

## Verify the behavior

`TestModulesPostgresRedisWorkflow` exercises real HTTP OTP signup, PostgreSQL
persistence, CLI reads, authenticated access and the background reaction. Run the
required [integration gate](testing-database.html) with disposable PostgreSQL and
Redis; without their settings the integration test skips.

Continue with [transactional notifications](transactional-notifications.html) for
atomic intent, or [account onboarding](onboarding.html) for optional background work.
