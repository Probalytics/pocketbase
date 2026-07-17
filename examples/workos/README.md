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
