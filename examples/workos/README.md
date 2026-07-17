# WorkOS authentication demo

A minimal PocketBase executable + single page login UI showcasing the
fork's WorkOS User Management integration:

- **email-first sign in** — enterprise emails are routed automatically to
  their organization's SSO (SAML/OIDC via WorkOS), everyone else gets
  password and one-time code options
- **password sign in / sign up** — credentials are stored in WorkOS, not
  locally; supports TOTP two-step verification
- **one-time email codes** — WorkOS Magic Auth behind the standard
  PocketBase OTP endpoints
- **social sign in** — Google / Microsoft / GitHub via WorkOS
- **Directory Sync** — SCIM provisioning/deprovisioning through
  `POST /api/workos/webhooks`

## Run

```sh
go run ./examples/workos serve
```

Then:

1. Open `http://127.0.0.1:8090/_/` and create the first superuser
   (superuser login always stays native — it is never delegated to WorkOS).
2. Enable WorkOS in the app settings with the credentials from your
   [WorkOS dashboard](https://dashboard.workos.com) — either from the
   dashboard settings UI or via the API:

   ```sh
   curl -X PATCH http://127.0.0.1:8090/api/settings \
     -H "Authorization: <superuser token>" \
     -H "Content-Type: application/json" \
     -d '{"workos": {
           "enabled":  true,
           "clientId": "client_...",
           "apiKey":   "sk_test_...",
           "webhookSecret": "..."
         }}'
   ```

3. In the WorkOS dashboard add `http://127.0.0.1:8090/` as an allowed
   redirect URI.
4. Open `http://127.0.0.1:8090/` and sign in.

## Enterprise SSO routing

Create an `organizations` record with the customer's `workosOrgId` and
their verified email domains (e.g. `["acme.com"]`). Any user entering an
email at one of those domains is redirected to that organization's
identity provider instead of the password form.

To let the customer's IT admin configure their own SSO connection or
directory, generate a self-serve WorkOS Admin Portal link:

```sh
curl -X POST http://127.0.0.1:8090/api/workos/portal-link \
  -H "Authorization: <superuser token>" \
  -H "Content-Type: application/json" \
  -d '{"organization": "<organizations record id>", "intent": "sso"}'
```

## Directory Sync (SCIM)

Point a WorkOS webhook at `https://<your host>/api/workos/webhooks`.
`dsync.user.created/updated` provision or update shadow user records,
`dsync.user.deleted` suspends them (blocking sign in) without deleting
any data.

## Session hardening (token refresh)

By default a PocketBase auth token is independent of the WorkOS session
once issued, so revoking or suspending a user in WorkOS only takes effect
on the next Directory Sync event (or when the PocketBase token expires).

Enabling `workos.syncOnRefresh` closes that gap: on every PocketBase
`auth-refresh` of a delegated collection the stored WorkOS refresh token is
exchanged for a fresh one and the local record/organization state is
re-synced. If WorkOS rejects the exchange (revoked/expired session,
suspended or deleted user) the PocketBase refresh is rejected too, logging
the user out at their next refresh.

The refresh token is stored **encrypted** in a hidden `workosRefreshToken`
users field, so this requires starting the server with an encryption key:

```sh
./pocketbase serve --encryptionEnv=PB_ENCRYPTION_KEY
# with PB_ENCRYPTION_KEY set to a 32-character secret
```

Without an encryption key the token is not persisted and the re-validation
is skipped (the refresh proceeds as before). Transient WorkOS/network
errors during a refresh also fail open, so an outage doesn't log everyone
out.

## Email changes

The native `request-email-change` / `confirm-email-change` endpoints keep
working, and the confirmation still goes through PocketBase's own signed,
single-use token emailed to the new address. On confirmation the change is
additionally propagated to WorkOS (via `UpdateUser`) so the WorkOS user and
the local record stay in sync.

Because the PocketBase token already proves ownership of the new address,
the WorkOS email is updated with `email_verified: true` (otherwise WorkOS
resets it to unverified and blocks the next password login). If the WorkOS
update fails, the confirmation is aborted and the local email is left
unchanged, so the two never drift apart.

For delegated collections the confirm step does not require the account
password (delegated records only hold a random local password, and SSO /
Magic Auth users have none) — possession of the emailed token is the proof.

## Calling WorkOS directly from the client

Enable `workos.exposeTokens` to include the raw WorkOS `accessToken` and
`refreshToken` in the `workos` object of the auth response `meta`, for
clients that need to call the WorkOS APIs directly. It is off by default
since it surfaces a long-lived credential to the client.
