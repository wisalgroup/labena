# Identity schema

Identity starts with five creation migrations, applied in dependency order:

| Table | Responsibility |
| --- | --- |
| `users` | Account, contacts, optional password and lifecycle state |
| `auth_identities` | External provider subjects linked to accounts |
| `verification_challenges` | Expiring OTP proofs and per-proof attempts |
| `sessions` | Active and revoked device sessions |
| `session_refresh_tokens` | Refresh credentials and rotation history for replay detection |

Each file creates the complete table and its indexes. Go owns business enum values
and validation; PostgreSQL enforces primary keys, uniqueness, foreign keys and
required fields. Redis owns shared rate budgets. There is no custom social-login
challenge table.

`sessions.expires_at` and `session_refresh_tokens.expires_at` are nullable: `NULL`
means no time-based expiry. Access JWTs always expire. Logout and other revocations
still invalidate the session; cleanup removes revoked or absolutely expired
sessions and their refresh history in bounded batches.

For a development database already using the preceding five-table baseline, this
specific schema change can preserve its data with:

```sql
ALTER TABLE sessions ALTER COLUMN expires_at DROP NOT NULL;
ALTER TABLE session_refresh_tokens ALTER COLUMN expires_at DROP NOT NULL;
```

Existing rows keep their stored deadlines. Re-running already-applied baseline
migrations does not apply these column changes. The statements above were not
automatically run against your development database.

This is a **pre-release baseline reset**, replacing the earlier development history.
It is intended for a fresh database. Existing development databases must be reset
before applying it; this deletes their data. Do not deploy it onto a database that
must retain the earlier history or data.

Once the baseline is released or used by databases that must be preserved, leave
these files unchanged and add forward migrations for schema changes. ALTER migrations
are normal production evolution, not architectural debt. Down migrations drop data;
full rollback must first remove dependent tables owned by other modules.

App assembly registers these embedded files with the shared Goose history. Only the
migration command applies them; module registration never performs schema changes.

The old Watermill SQL event/offset tables are absent from this baseline. There
are no retained event-creation or retirement migrations; use the fresh schema
described above. GoChannel events have no persistent transport tables or event
pruning command, and old events are not replayed or backfilled.

The current baseline is passwordless. Users and verification challenges have no
password hash, password-change timestamp or password credential-version columns.
OTP and social sign-in share the existing sessions and refresh-token tables.
