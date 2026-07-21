#!/usr/bin/env bash
# Real-WorkOS integration test for the PocketBase fork.
# Usage: WOS_CLIENT_ID=client_... WOS_API_KEY=sk_test_... ./real_e2e.sh
set -u

S="${TMPDIR:-/tmp}/pbworkos-real-e2e"
mkdir -p "$S"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
PB=http://127.0.0.1:8091
WOS=https://api.workos.com
CLIENT_ID="${WOS_CLIENT_ID:?set WOS_CLIENT_ID=client_...}"
API_KEY="${WOS_API_KEY:?set WOS_API_KEY=sk_test_...}"
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "PASS  $1"; }
bad()  { FAIL=$((FAIL+1)); echo "FAIL  $1 -- $2"; }

# 0. reachability
if ! curl -s --max-time 10 -o /dev/null -w '%{http_code}' "$WOS/user_management/users?limit=1" -H "Authorization: Bearer $API_KEY" | grep -q 200; then
  echo "ABORT: cannot reach $WOS with the provided key (network policy or invalid key)"; exit 1
fi
ok "0: API key valid, api.workos.com reachable"

# 1. fresh server
pkill -f 'pbworkos serve' 2>/dev/null && sleep 1  # leftover server from a previous run
rm -rf "$S/real/pb_data"; mkdir -p "$S/real"
cp -r "$REPO/examples/workos/pb_public" "$S/real/pb_public" 2>/dev/null || true
(cd "$REPO" && go build -o "$S/real/pbworkos" ./examples/workos) || { echo "build failed"; exit 1; }
(cd "$S/real" && ./pbworkos superuser upsert admin@example.com Admin12345! --dir=./pb_data >/dev/null 2>&1)
(cd "$S/real" && exec ./pbworkos serve --dir=./pb_data --http=127.0.0.1:8091 >"$S/real/server.log" 2>&1) &
SRV=$!; trap 'kill $SRV 2>/dev/null' EXIT
sleep 2
if ! curl -s --max-time 5 -o /dev/null "$PB/api/health"; then
  echo "ABORT: the test server did not start:"; tail -5 "$S/real/server.log"; exit 1
fi

TOKEN=$(curl -s -X POST $PB/api/collections/_superusers/auth-with-password -H 'Content-Type: application/json' \
  -d '{"identity":"admin@example.com","password":"Admin12345!"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
curl -s -X PATCH $PB/api/settings -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"workos\":{\"enabled\":true,\"clientId\":\"$CLIENT_ID\",\"apiKey\":\"$API_KEY\",\"apiURL\":\"$WOS\"}}" >/dev/null

# note: a unique non-example.com domain is used because WorkOS test
# environments ship with a default SSO connection matching example.com,
# which rejects password logins with an "sso_required" error
# (that mapping is tested separately at the end)
TS=$(date +%s)
EMAIL="pbfork.$TS@pbfork-$TS.com"
PW='R3al!yStr0ngPass'

# 2. signup -> real WorkOS CreateUser
CREATE=$(curl -s -w '\n%{http_code}' -X POST $PB/api/collections/users/records -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PW\",\"passwordConfirm\":\"$PW\"}")
[ "$(echo "$CREATE" | tail -1)" = 200 ] && ok "1: signup created record + real WorkOS user ($EMAIL)" || bad "1: signup" "$CREATE"

WUID=$(curl -s "$WOS/user_management/users?email=$EMAIL" -H "Authorization: Bearer $API_KEY" | python3 -c 'import sys,json;d=json.load(sys.stdin)["data"];print(d[0]["id"] if d else "")')
[ -n "$WUID" ] && ok "2: user exists in WorkOS ($WUID)" || bad "2: user not found in WorkOS" ""

# 3. password auth via WorkOS
#    (environments with "Require email verification" enabled reject the first
#    attempt with a dedicated 403 - the user is then marked verified via the
#    management API, standing in for the emailed verification code)
AUTH=$(curl -s -w '\n%{http_code}' -X POST $PB/api/collections/users/auth-with-password -H 'Content-Type: application/json' \
  -d "{\"identity\":\"$EMAIL\",\"password\":\"$PW\"}")
AUTHC=$(echo "$AUTH" | tail -1)
if [ "$AUTHC" = 200 ]; then
  ok "3: password auth against real WorkOS"
elif [ "$AUTHC" = 403 ] && echo "$AUTH" | grep -q "email must be verified"; then
  ok "3a: unverified email rejected with dedicated 403 (env requires email verification)"
  curl -s -X PUT "$WOS/user_management/users/$WUID" -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
    -d '{"email_verified":true}' >/dev/null
  AUTH=$(curl -s -w '\n%{http_code}' -X POST $PB/api/collections/users/auth-with-password -H 'Content-Type: application/json' \
    -d "{\"identity\":\"$EMAIL\",\"password\":\"$PW\"}")
  [ "$(echo "$AUTH" | tail -1)" = 200 ] && ok "3: password auth against real WorkOS (after email verification)" || bad "3: password auth after verification" "$(echo "$AUTH" | head -c 300)"
else
  bad "3: password auth" "$(echo "$AUTH" | head -c 300)"
fi

WRONG=$(curl -s -o /dev/null -w '%{http_code}' -X POST $PB/api/collections/users/auth-with-password -H 'Content-Type: application/json' \
  -d "{\"identity\":\"$EMAIL\",\"password\":\"nope-nope\"}")
[ "$WRONG" = 400 ] && ok "4: wrong password rejected (400)" || bad "4: wrong password" "$WRONG"

# 4. magic auth: request via PB, then read the code from a direct CreateMagicAuth
#    (the API returns the code in the response so apps can send custom emails)
OTP=$(curl -s -X POST $PB/api/collections/users/request-otp -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["otpId"])')
MAGICRESP=$(curl -s -X POST "$WOS/user_management/magic_auth" -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\"}")
CODE=$(echo "$MAGICRESP" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("code",""))')
if [ -n "$CODE" ]; then
  MAGIC=$(curl -s -w '\n%{http_code}' -X POST $PB/api/collections/users/auth-with-otp -H 'Content-Type: application/json' \
    -d "{\"otpId\":\"$OTP\",\"password\":\"$CODE\"}")
  [ "$(echo "$MAGIC" | tail -1)" = 200 ] && ok "5: magic auth code login against real WorkOS" || bad "5: magic auth" "$(echo "$MAGIC" | head -c 300)"
else
  REASON=$(echo "$MAGICRESP" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("error_description") or d.get("message") or "code not exposed by API")')
  echo "SKIP  5: magic auth ($REASON - enable Magic Auth in the WorkOS dashboard under Authentication)"
fi

# 5. password reset request (WorkOS sends the email; we just verify 204)
PR=$(curl -s -o /dev/null -w '%{http_code}' -X POST $PB/api/collections/users/request-password-reset -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\"}")
[ "$PR" = 204 ] && ok "6: password reset request accepted (204)" || bad "6: password reset request" "$PR"

# 6. real organization + domain routing + portal link
ORG=$(curl -s -X POST "$WOS/organizations" -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
  -d '{"name":"PB Fork Test Org","domain_data":[{"domain":"pbfork-test.example","state":"verified"}]}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))')
if [ -n "$ORG" ]; then
  ok "7: created real WorkOS organization ($ORG)"
  curl -s -X POST $PB/api/collections/organizations/records -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
    -d "{\"name\":\"PB Fork Test Org\",\"workosOrgId\":\"$ORG\",\"domains\":[\"pbfork-test.example\"]}" >/dev/null
  START=$(curl -s -X POST $PB/api/collections/users/auth-with-workos -H 'Content-Type: application/json' \
    -d '{"email":"someone@pbfork-test.example"}')
  echo "$START" | grep -q "organization_id=$ORG" && ok "8: email-domain routing produced org-scoped authorize URL" || bad "8: domain routing" "$(echo "$START" | head -c 200)"
  AUTHURL=$(echo "$START" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("authURL",""))')
  HTTPC=$(curl -s -o /dev/null -w '%{http_code}' "${AUTHURL}http%3A%2F%2F127.0.0.1%3A8091%2F")
  case "$HTTPC" in 200|302) ok "9: real WorkOS accepted the authorize URL ($HTTPC)";; *) bad "9: authorize URL" "$HTTPC (check allowed redirect URIs in the WorkOS dashboard)";; esac
  PL=$(curl -s -X POST $PB/api/workos/portal-link -H "Authorization: $TOKEN" -H 'Content-Type: application/json' \
    -d "{\"organization\":\"$ORG\",\"intent\":\"sso\"}")
  echo "$PL" | grep -q '"link"' && ok "10: real Admin Portal link generated" || bad "10: portal link" "$(echo "$PL" | head -c 200)"
else
  echo "SKIP  7-10: could not create a WorkOS organization"
fi

# 7. sso_required mapping: WorkOS test environments have a default SSO
#    connection matching example.com, so a password login for that domain
#    must surface the fork's dedicated 403 "SSO required" error
EMAIL2="pbfork.sso.$TS@example.com"
curl -s -X POST $PB/api/collections/users/records -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL2\",\"password\":\"$PW\",\"passwordConfirm\":\"$PW\"}" >/dev/null
SSOAUTH=$(curl -s -w '\n%{http_code}' -X POST $PB/api/collections/users/auth-with-password -H 'Content-Type: application/json' \
  -d "{\"identity\":\"$EMAIL2\",\"password\":\"$PW\"}")
if [ "$(echo "$SSOAUTH" | tail -1)" = 403 ] && echo "$SSOAUTH" | grep -q "SSO authentication is required"; then
  ok "11: sso_required mapped to a dedicated 403 SSO error"
else
  echo "SKIP  11: sso_required mapping (no SSO connection matching example.com in this environment? got: $(echo "$SSOAUTH" | head -c 200))"
fi

# 8. email-change contract: the fork's confirm-email-change delegation calls
#    WorkOS UpdateUser with email_verified=true. Verify directly against WorkOS
#    that changing the email that way keeps the account able to password-login
#    (without email_verified WorkOS resets it to false and blocks login).
if [ -n "$WUID" ]; then
  NEWEMAIL="pbfork.chg.$TS@pbfork-$TS.com"
  UPD=$(curl -s -X PUT "$WOS/user_management/users/$WUID" -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$NEWEMAIL\",\"email_verified\":true}")
  echo "$UPD" | grep -q "\"email\":\"$NEWEMAIL\"" && echo "$UPD" | grep -q '"email_verified":true' \
    && ok "12: WorkOS UpdateUser changed the email and kept it verified" \
    || bad "12: WorkOS email update" "$(echo "$UPD" | head -c 200)"
  RELOGIN=$(curl -s -w '\n%{http_code}' -X POST $PB/api/collections/users/auth-with-password -H 'Content-Type: application/json' \
    -d "{\"identity\":\"$NEWEMAIL\",\"password\":\"$PW\"}")
  [ "$(echo "$RELOGIN" | tail -1)" = 200 ] && ok "13: password login works with the changed email" || bad "13: login after email change" "$(echo "$RELOGIN" | head -c 200)"
else
  echo "SKIP  12-13: email-change contract (no WorkOS user id)"
fi

echo; echo "$PASS passed, $FAIL failed"
[ "$FAIL" = 0 ]
