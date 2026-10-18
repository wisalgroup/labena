# Expo push delivery

`pkg/push` sends mobile alerts through Expo using the standard HTTP client. It can
run without app assembly, PostgreSQL, Redis or a recipient model. The framework
[notifier](../../internal/platform/notification/README.md) composes this same client
with stored installations, delivery retries and receipt polling.

Expo hands messages to the platform's FCM/APNs infrastructure. This adapter sends
OS notifications, not realtime application events, chat messages or browser Web Push.
There is no Firebase SDK or native FCM/APNs sender in the Go backend.

| Start here | Use |
| --- | --- |
| Verify Expo credentials and one destination | [Standalone example](#try-one-alert) |
| Integrate a mobile client | [Token registration](#expo-mobile-client) |
| Send to all eligible devices for a user/admin | [Durable notifications](#durable-notifications) |
| Diagnose accepted-but-unconfirmed delivery | [Receipts](#inspect-a-receipt) |

## Prepare Expo and the mobile app

Use an Expo development/production build with `expo-notifications` and configured
platform push credentials. Set the EAS project ID explicitly when obtaining an Expo
push token. Use a supported physical device or supported emulator/simulator; current
requirements and credential setup are in [Expo's setup guide](https://docs.expo.dev/push-notifications/push-notifications-setup/).

This framework's remote-push walkthrough uses your built application, not Expo Go.
Install/configure `expo-notifications`, rebuild after native configuration changes,
and request notification permission before registering a token. The Go backend
uses an Expo access token only when your project enables enhanced push security.
Never ship that server credential inside the mobile application.

## Application configuration

```dotenv
PUSH_ENABLED=true
PUSH_EXPO_PROJECT_ID=your-eas-project-id
PUSH_EXPO_ACCESS_TOKEN=your-expo-enhanced-push-security-token
```

Project ID scopes the persisted registration namespace; Expo routes using the token.
Get client tokens using that same EAS project ID. The server cannot establish token
ownership or its project from syntax alone. Use authenticated registration endpoints.
Enable enhanced push security in Expo and supply its access token in production.
The access token is optional for projects which have not enabled that feature.
Do not place it in the mobile bundle or commit it to configuration files.



These settings are loaded by normal app/worker entrypoints. They construct the
shared client but send nothing during startup. The project ID identifies the
notification subscription namespace; it is not a recipient, installation or ticket ID.

## Try one alert

First obtain an Expo push token from your configured app. The standalone example
reads **process environment variables directly**; it does not load `.env` or use
`APP_ENV_FILE`. Supply the values to the shell/process without committing them.

```sh
PUSH_REGISTRATION='ExpoPushToken[replace-with-your-registration]' \
  go run ./examples/push
```

If enhanced push security is enabled, also export `PUSH_EXPO_ACCESS_TOKEN` to this
process. The example sends a **real alert** and prints an accepted ticket ID. It
requires neither SQL nor Redis. A ticket means Expo accepted work; it does not
prove that the device displayed it.

After approximately 15 minutes, check that ticket without sending another alert:

```sh
PUSH_TICKET_ID='replace-with-the-ticket-id' go run ./examples/push
```

The output distinguishes a pending receipt, a handoff confirmation and an error.
When `PUSH_TICKET_ID` is set, the example takes the receipt path even if a registration
is also present. A receipt lookup failure is a reason to retry the lookup, not to
send another notification.

## Send independently

This function-body fragment imports `os`, `time` and `github.com/wisalgroup/labena/pkg/push`.
It assumes `ctx`, an authorized `expoPushToken`, and a string `exportID`:

```go
sender, err := push.NewExpo(push.ExpoConfig{
    AccessToken: os.Getenv("PUSH_EXPO_ACCESS_TOKEN"),
})
if err != nil { return err }
expires := time.Now().Add(time.Hour)
ticket, err := sender.Send(ctx, expoPushToken, push.Message{
    Title: "Your export is ready",
    Body: "Open the application to download it.",
    Data: map[string]string{"export_id": exportID},
    ExpiresAt: &expires,
})
if err != nil { return err }
// Persist ticket.ID. Acceptance only means Expo queued this message.
```



Construct once per process and reuse the sender. Calls use their caller's context;
do not replace it with a detached background context inside business operations.

## Inspect a receipt

Later, in another request or background job (typically after 15 minutes):

```go
receipt, err := sender.Receipt(ctx, ticket.ID)
if err != nil {
    return err // Retry this lookup; do not resend the notification.
}
if !receipt.Found {
    return nil // Still pending: schedule another lookup.
}
if receipt.Error != nil {
    return receipt.Error // Definitive handoff failure; inspect its classification.
}
// Expo confirmed handoff to the platform provider, not display or user receipt.
```



Persist the ticket ID if the process can restart before lookup. No background poller
runs inside `pkg/push`; your caller owns that lifecycle. See
[Expo's send/receipt lifecycle](https://docs.expo.dev/push-notifications/sending-notifications/)
for provider-side behavior.

| Outcome | Meaning | Next step |
| --- | --- | --- |
| Send returns a ticket | Expo accepted the request | Save the ticket and check later |
| Receipt returns a top-level error | Lookup did not complete reliably | Retry receipt lookup |
| `Found == false` | Receipt is not available yet | Schedule another lookup |
| `Found == true`, `Error == nil` | Expo confirmed provider handoff | Record success; this is not user receipt/read state |
| `Found == true`, `Error != nil` | Definitive provider handoff failure | Inspect classification before retry or disabling the destination |

The framework notifier supplies this persisted polling lifecycle if you do not
want to implement it separately.

## Control limits and failures

The shared `push.Sender` contract defines both operations once. `Ticket` acknowledges
queue acceptance. `Receipt.Found` distinguishes pending from success, and
`Receipt.Error` distinguishes a definitive failure from a lookup/network error.
`*push.Error` carries safe codes, permanence, invalid-destination status, and retry timing.
Provider bodies and registration values are never included in error messages.

`NewExpo` borrows an optional `HTTPClient` transport and rejects redirects to avoid
forwarding credentials. Each operation has a 30-second deadline, with at most six
concurrent requests per shared client. It performs no automatic retries and starts
no background workers. Respect Expo's project-level limits across replicas; HTTP 429
and Retry-After are surfaced for backoff. Reuse clients rather than constructing one
per message. Configure worker throughput to match your project budget.

Only `ExpoPushToken[...]` and `ExponentPushToken[...]` registrations are accepted;
native FCM/APNs tokens must be replaced by Expo registrations. Content is validated
and bounded before I/O. `ExpiresAt` maps to Expo's expiration timestamp. Never send
passwords, login codes, credentials or sensitive content in OS notifications.

| Field or limit | Contract |
| --- | --- |
| `Title` | Nonblank, at most 200 bytes |
| `Body` | At most 2000 bytes |
| `Data` | String keys and values; resource references, not authoritative state |
| Encoded `Message` | At most 3500 bytes before provider serialization |
| `ExpiresAt` | Optional expiry; elapsed content is rejected/skipped |
| Concurrency | At most six HTTP operations per sender instance |
| Operation deadline | At most 30 seconds, or a shorter caller deadline |

Data keys `from`, `message_type`, and prefixes `google.` / `gcm.` are reserved.
The adapter chooses the default notification sound; it currently exposes no
sound, badge, Android-channel selector or bulk-send API. Add such options only
alongside their provider validation and behavior.

Classify errors using `errors.As`, not provider strings:

```go
var failure *push.Error
if errors.As(err, &failure) {
    // failure.InvalidDestination: disable this registration version.
    // failure.Permanent: correct content/credentials before retrying.
    // failure.RetryAfter: minimum delay before another eligible attempt.
}
```

Import `errors`. Common safe codes are `unregistered`, `rate_limited`, `credentials`,
`invalid_argument` and `unavailable`. A network failure during Send can leave
acceptance uncertain. The standalone sender deliberately does not choose a retry
policy for that operation. See the notifier's [delivery lifecycle](../../internal/platform/notification/README.md#delivery-lifecycle)
for the framework's policy.

## Durable notifications

The framework notifier uses the same client. Each installation gets its own delivery
row and ticket. After acceptance, status is `awaiting_receipt`. Workers check after
15 minutes, then every five minutes while a receipt is missing or lookup fails
(respecting longer provider Retry-After). Polling does not consume the send-attempt
budget. A worker restart resumes lookup of the same persisted ticket.

An `ok` receipt marks the row sent. `DeviceNotRegistered` disables only the matching
registration version. A definitive retryable receipt failure schedules a new send;
a receipt API outage does not. After 24 hours without confirmation, the row fails
without automatically resending. Operator replay clears the old ticket and may cause
a duplicate. A crash after Expo acceptance but before SQL stores its ticket can also
cause a duplicate; exactly-once delivery is not claimed.

Synchronous `Notifier.Send` returns `Result.PushTicketID`; it does not persist or
poll that ticket. The caller must check receipts. Prefer `Enqueue` when this should
happen automatically through the existing worker lifecycle.

Use `notifier.Enqueue` with `Message.Push` after the authenticated installation
has been registered. PostgreSQL snapshots each eligible installation and content;
only delivery IDs enter Asynq. The dispatcher runs with workers and needs no cron
scheduler. Subscription inactivity pruning is a separate centralized schedule.

The shipped Identity welcome builder selects database and email; it does not
implicitly send a push. Your module selects push explicitly when that message is
useful as an OS alert. See [selecting channels](../../internal/platform/notification/README.md#select-channels).

## Expo mobile client

After permission and token acquisition, send the Expo registration to the
framework's authenticated subscription route. The following helper assumes your
application supplies its EAS `projectId`, a locally persisted installation UUID,
API origin and the user's Identity access token:

```javascript
import * as Notifications from 'expo-notifications';
import { Platform } from 'react-native';

export async function registerPush({ projectId, installationID, apiURL, accessToken }) {
  if (Platform.OS === 'android') {
    await Notifications.setNotificationChannelAsync('default', {
      name: 'Default', importance: Notifications.AndroidImportance.DEFAULT,
    });
  }
  const permission = await Notifications.requestPermissionsAsync();
  if (permission.status !== 'granted') return null;

  const { data: registration } = await Notifications.getExpoPushTokenAsync({ projectId });
  const response = await fetch(`${apiURL}/api/v1/notifications/subscriptions`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ installation_id: installationID, registration }),
  });
  if (!response.ok) {
    throw new Error(`Push registration failed (${response.status})`);
  }
  const { data: subscription } = await response.json();
  return subscription.id;
}
```

`apiURL` must be reachable from the device; its `localhost` is not your development
computer. Save the returned subscription ID. Retry failed registration through your
normal client request policy and refresh it on startup/token rotation. Do not log
the registration or access token.

Before switching accounts, detach the old subscription:

```javascript
const response = await fetch(`${apiURL}/api/v1/notifications/subscriptions/${subscriptionID}`, {
  method: 'DELETE',
  headers: { Authorization: `Bearer ${accessToken}` },
});
if (!response.ok) throw new Error(`Push detachment failed (${response.status})`);
```

Use the previous account's valid token. Identity logout also detaches registrations
bound to that session. Detachment cannot retract a provider request already in
flight. A registration token is a delivery address, not an authentication credential.

For foreground presentation, configure `Notifications.setNotificationHandler` at
application startup according to your UX. Subscribe to received/tap events and
remove listeners when the owning component unmounts. On a tap, authenticate and
fetch current authorized state using the payload's resource ID; the push body must
not decide authorization. See [Expo's incoming-notification guide](https://docs.expo.dev/push-notifications/receiving-notifications/).

Framework notifications add `notification_key` and `notification_type` string data.
Use the key to deduplicate client handling. These reserved values override values
supplied by a module. Direct `pkg/push` calls do not add them automatically.

## Test and troubleshoot push

```sh
go test ./pkg/push/... ./internal/platform/notification/...
```

Provider tests use a local fake HTTP transport to exercise deadlines, safe errors
and receipt parsing. PostgreSQL-dependent notification tests skip unless
`DATABASE_TEST_POSTGRES` is configured; use the [integration gate](../../docs/testing-database.md)
to verify persistence. Automated tests never contact Expo or send a real alert.

| Symptom | Check |
| --- | --- |
| Token acquisition fails | Build credentials, EAS project ID, permission and supported device environment |
| Subscription returns 401 | Identity token/session and authentication configuration |
| Subscription returns conflict | Installation/registration already active under another recipient; detach it first |
| Send returns `credentials` | Server Expo access token and platform credentials in the Expo project |
| Accepted ticket, no alert | Receipt, device notification settings, foreground presentation and app environment |
| Receipt is not found yet | Poll later; do not resend solely because it is absent |
| `unregistered` | Disable the matching registration; refresh from the client when it registers again |
| HTTP 429 / `rate_limited` | Respect Retry-After and budget throughput across all replicas |
| Notification delivery is skipped | Expired content, no frozen target, detached session or changed registration version |

Expo/mobile behavior can change independently of this repository. Use the official
[setup](https://docs.expo.dev/push-notifications/push-notifications-setup/) and
[send/receipt](https://docs.expo.dev/push-notifications/sending-notifications/) guides
for current platform prerequisites. The Go APIs and limits above describe this
repository's actual adapter.
