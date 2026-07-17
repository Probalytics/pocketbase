package apis_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	mockWorkOSClientId      = "client_test"
	mockWorkOSAPIKey        = "sk_test"
	mockWorkOSWebhookSecret = "whsec_test"
	mockWorkOSPassword      = "workos-pass-123"
	mockWorkOSMagicAuthCode = "151515"
	mockWorkOSTOTPCode      = "654321"
	mockWorkOSVerifyCode    = "999000"

	superuserAuthToken = "eyJhbGciOiJIUzI1NiJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoicGJjXzMxNDI2MzU4MjMiLCJleHAiOjI1MjQ2MDQ0NjEsInJlZnJlc2hhYmxlIjp0cnVlfQ.UXgO3j-0BumcugrFjbd7j0M4MQvbrLggLlcu_YNGjoY"
)

type mockWorkOSUser struct {
	id       string
	first    string
	last     string
	orgId    string
	verified bool
	mfa      bool
	ssoOnly  bool // the email domain matches an active SSO connection
}

var mockWorkOSUsers = map[string]mockWorkOSUser{
	"test@example.com":       {id: "user_wos_test", first: "Test", last: "User", verified: true},
	"test2@example.com":      {id: "user_wos_test2", verified: true},
	"new@example.com":        {id: "user_wos_new", first: "New", last: "User", verified: true},
	"org@example.com":        {id: "user_wos_org", orgId: "org_wos_123", verified: true},
	"mfa@example.com":        {id: "user_wos_mfa", verified: true, mfa: true},
	"sso@example.com":        {id: "user_wos_sso", orgId: "org_wos_123", verified: true},
	"ssoonly@example.com":    {id: "user_wos_ssoonly", verified: true, ssoOnly: true},
	"unverified@example.com": {id: "user_wos_unverified", verified: false},
}

func mockWorkOSUserPayload(email string, u mockWorkOSUser) map[string]any {
	return map[string]any{
		"id":             u.id,
		"email":          email,
		"email_verified": u.verified,
		"first_name":     u.first,
		"last_name":      u.last,
	}
}

// newMockWorkOSServer starts a stateless mock WorkOS API server with
// canned users covering the subset of endpoints used by the delegation layer.
func newMockWorkOSServer() *httptest.Server {
	writeJSON := func(w http.ResponseWriter, status int, data any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(data)
	}

	writeErr := func(w http.ResponseWriter, status int, code string, message string) {
		writeJSON(w, status, map[string]any{"code": code, "message": message})
	}

	requireBearer := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+mockWorkOSAPIKey {
			writeErr(w, 401, "unauthorized", "Invalid API key.")
			return false
		}
		return true
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /user_management/authenticate", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		// the params could be submitted either as JSON (the PB WorkOS client)
		// or as form-encoded body/basic auth (the golang.org/x/oauth2 exchange)
		params := map[string]string{}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			anyParams := map[string]any{}
			_ = json.Unmarshal(raw, &anyParams)
			for k, v := range anyParams {
				params[k] = fmt.Sprintf("%v", v)
			}
		} else {
			values, _ := url.ParseQuery(string(raw))
			for k := range values {
				params[k] = values.Get(k)
			}
			if id, secret, ok := r.BasicAuth(); ok {
				params["client_id"], _ = url.QueryUnescape(id)
				params["client_secret"], _ = url.QueryUnescape(secret)
			}
		}

		if params["client_id"] != mockWorkOSClientId || params["client_secret"] != mockWorkOSAPIKey {
			writeErr(w, 401, "invalid_client", "Invalid client credentials.")
			return
		}

		success := func(email string, u mockWorkOSUser, method string) {
			resp := map[string]any{
				"access_token":          "wos_access_" + u.id,
				"refresh_token":         "wos_refresh_" + u.id,
				"token_type":            "Bearer",
				"user":                  mockWorkOSUserPayload(email, u),
				"authentication_method": method,
			}
			if u.orgId != "" {
				resp["organization_id"] = u.orgId
			}
			writeJSON(w, 200, resp)
		}

		switch params["grant_type"] {
		case "password":
			email := params["email"]
			u, ok := mockWorkOSUsers[email]
			if ok && u.ssoOnly {
				// matches the real API behavior - the SSO requirement is
				// reported (in the OAuth error shape) before any password check
				writeJSON(w, 400, map[string]any{
					"error":             "sso_required",
					"error_description": "User must authenticate using one of the matching connections.",
					"email":             email,
					"connection_ids":    []string{"conn_wos_1"},
				})
				return
			}
			if !ok || params["password"] != mockWorkOSPassword {
				writeErr(w, 400, "invalid_credentials", "Invalid email or password.")
				return
			}
			if !u.verified {
				writeJSON(w, 400, map[string]any{
					"code":                         "email_verification_required",
					"message":                      "Email ownership must be verified before authentication.",
					"email":                        email,
					"pending_authentication_token": "pending_token_verify",
					"email_verification_id":        "email_verification_1",
				})
				return
			}
			if u.mfa {
				writeJSON(w, 422, map[string]any{
					"code":                         "mfa_challenge",
					"message":                      "MFA verification is required.",
					"pending_authentication_token": "pending_token_mfa",
					"authentication_factors":       []map[string]string{{"id": "auth_factor_1", "type": "totp"}},
				})
				return
			}
			success(email, u, "Password")
		case "urn:workos:oauth:grant-type:magic-auth:code":
			email := params["email"]
			u, ok := mockWorkOSUsers[email]
			if !ok || params["code"] != mockWorkOSMagicAuthCode {
				writeErr(w, 400, "invalid_one_time_code", "Invalid one-time code.")
				return
			}
			success(email, u, "MagicAuth")
		case "urn:workos:oauth:grant-type:mfa-totp":
			if params["pending_authentication_token"] != "pending_token_mfa" ||
				params["authentication_challenge_id"] != "auth_challenge_1" ||
				params["code"] != mockWorkOSTOTPCode {
				writeErr(w, 400, "invalid_totp_code", "Invalid TOTP code.")
				return
			}
			success("mfa@example.com", mockWorkOSUsers["mfa@example.com"], "Password")
		case "authorization_code":
			if params["code"] != "oauth_code_ok" {
				writeErr(w, 400, "invalid_grant", "Invalid authorization code.")
				return
			}
			success("sso@example.com", mockWorkOSUsers["sso@example.com"], "SSO")
		case "refresh_token":
			rt := params["refresh_token"]
			// a revoked/expired WorkOS session
			if rt == "wos_refresh_revoked" {
				writeErr(w, 400, "invalid_grant", "The refresh token is invalid.")
				return
			}
			// resolve the user by the "wos_refresh_<id>[_rN]" token issued by
			// success()/prior rotations
			var email string
			var u mockWorkOSUser
			for e, cand := range mockWorkOSUsers {
				if strings.HasPrefix(rt, "wos_refresh_"+cand.id) {
					email, u = e, cand
					break
				}
			}
			if email == "" {
				writeErr(w, 400, "invalid_grant", "The refresh token is invalid.")
				return
			}
			// mirror WorkOS refresh token rotation - return a NEW refresh token
			writeJSON(w, 200, map[string]any{
				"access_token":          "wos_access_" + u.id + "_r2",
				"refresh_token":         "wos_refresh_" + u.id + "_r2",
				"token_type":            "Bearer",
				"user":                  mockWorkOSUserPayload(email, u),
				"organization_id":       u.orgId,
				"authentication_method": "RefreshToken",
			})
		default:
			writeErr(w, 400, "unsupported_grant_type", "Unsupported grant type.")
		}
	})

	mux.HandleFunc("POST /auth/factors/{factorId}/challenge", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		if r.PathValue("factorId") != "auth_factor_1" {
			writeErr(w, 404, "entity_not_found", "Factor not found.")
			return
		}
		writeJSON(w, 201, map[string]any{
			"id":                       "auth_challenge_1",
			"authentication_factor_id": "auth_factor_1",
		})
	})

	mux.HandleFunc("POST /user_management/users", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		body := struct {
			Email     string `json:"email"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body.Email == "exists@example.com" {
			writeErr(w, 422, "email_not_available", "Email is already in use.")
			return
		}

		writeJSON(w, 201, map[string]any{
			"id":             "user_wos_created",
			"email":          body.Email,
			"email_verified": false,
			"first_name":     body.FirstName,
			"last_name":      body.LastName,
		})
	})

	mux.HandleFunc("GET /user_management/users", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		email := r.URL.Query().Get("email")
		data := []map[string]any{}
		if u, ok := mockWorkOSUsers[email]; ok {
			data = append(data, mockWorkOSUserPayload(email, u))
		}
		writeJSON(w, 200, map[string]any{"data": data})
	})

	mux.HandleFunc("POST /user_management/magic_auth", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		body := struct {
			Email string `json:"email"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)

		u, ok := mockWorkOSUsers[body.Email]
		if !ok {
			writeErr(w, 404, "entity_not_found", "User not found.")
			return
		}

		writeJSON(w, 201, map[string]any{
			"id":      "magic_auth_1",
			"user_id": u.id,
			"email":   body.Email,
		})
	})

	mux.HandleFunc("POST /user_management/password_reset", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		body := struct {
			Email string `json:"email"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)

		writeJSON(w, 201, map[string]any{
			"id":    "password_reset_1",
			"email": body.Email,
		})
	})

	mux.HandleFunc("POST /user_management/password_reset/confirm", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		body := struct {
			Token       string `json:"token"`
			NewPassword string `json:"new_password"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body.Token != "reset_tok_ok" {
			writeErr(w, 404, "entity_not_found", "Invalid password reset token.")
			return
		}

		writeJSON(w, 200, map[string]any{
			"user": mockWorkOSUserPayload("test@example.com", mockWorkOSUsers["test@example.com"]),
		})
	})

	mux.HandleFunc("POST /user_management/users/{userId}/email_verification/send", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		writeJSON(w, 200, map[string]any{"user": map[string]any{"id": r.PathValue("userId")}})
	})

	mux.HandleFunc("POST /user_management/users/{userId}/email_verification/confirm", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		body := struct {
			Code string `json:"code"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body.Code != mockWorkOSVerifyCode {
			writeErr(w, 400, "email_verification_code_incorrect", "Invalid verification code.")
			return
		}

		writeJSON(w, 200, map[string]any{
			"user": map[string]any{
				"id":             r.PathValue("userId"),
				"email_verified": true,
			},
		})
	})

	mux.HandleFunc("POST /portal/generate_link", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		body := struct {
			Intent       string `json:"intent"`
			Organization string `json:"organization"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)

		writeJSON(w, 201, map[string]any{
			"link": "https://portal.workos.test/" + body.Intent + "/" + body.Organization,
		})
	})

	return httptest.NewServer(mux)
}

// setupWorkOSApp enables and configures the WorkOS settings of the test app
// against the provided mock server url.
func setupWorkOSApp(t testing.TB, app *tests.TestApp, apiURL string) {
	app.Settings().WorkOS.Enabled = true
	app.Settings().WorkOS.ClientId = mockWorkOSClientId
	app.Settings().WorkOS.APIKey = mockWorkOSAPIKey
	app.Settings().WorkOS.WebhookSecret = mockWorkOSWebhookSecret
	app.Settings().WorkOS.APIURL = apiURL

	// disable the native MFA of the users collection to test the
	// WorkOS delegated flows in isolation
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	users.MFA.Enabled = false
	if err = app.Save(users); err != nil {
		t.Fatal(err)
	}
}

// workosWebhookHeaders returns request headers with a WorkOS-Signature
// computed for the provided body (see workos.VerifyWebhookSignature).
func workosWebhookHeaders(secret string, body string, ts time.Time) map[string]string {
	msTimestamp := strconv.FormatInt(ts.UnixMilli(), 10)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msTimestamp))
	mac.Write([]byte("."))
	mac.Write([]byte(body))

	return map[string]string{
		"WorkOS-Signature": "t=" + msTimestamp + ", v1=" + hex.EncodeToString(mac.Sum(nil)),
	}
}

func findWorkOSExternalAuth(t testing.TB, app *tests.TestApp, record *core.Record, providerId string) *core.ExternalAuth {
	t.Helper()

	rels, err := app.FindAllExternalAuthsByRecord(record)
	if err != nil {
		t.Fatalf("Failed to fetch external auths: %v", err)
	}

	for _, rel := range rels {
		if rel.Provider() == "workos" && rel.ProviderId() == providerId {
			return rel
		}
	}

	t.Fatalf("Missing workos external auth with providerId %q (found %d rels)", providerId, len(rels))

	return nil
}

func TestRecordAuthWithPasswordWorkOS(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:           "delegated collection with valid WorkOS credentials and new user (shadow record creation)",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-password",
			Body:           strings.NewReader(`{"identity":"new@example.com","password":"` + mockWorkOSPassword + `"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"email":"new@example.com"`,
				`"verified":true`,
				`"name":"New User"`,
				`"authenticationMethod":"Password"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail("users", "new@example.com")
				if err != nil {
					t.Fatalf("Missing created shadow record: %v", err)
				}

				if v := record.GetString("workosUserId"); v != "user_wos_new" {
					t.Fatalf("Expected workosUserId %q, got %q", "user_wos_new", v)
				}

				findWorkOSExternalAuth(t, app, record, "user_wos_new")
			},
		},
		{
			Name:           "delegated collection with valid WorkOS credentials and existing local record (linking)",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-password",
			Body:           strings.NewReader(`{"identity":"test@example.com","password":"` + mockWorkOSPassword + `"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"email":"test@example.com"`,
				`"id":"4q1xlclmfloku33"`, // the existing record and not a new one
				`"verified":true`,        // upgraded from the WorkOS verified email
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindRecordById("users", "4q1xlclmfloku33")
				if err != nil {
					t.Fatal(err)
				}

				if v := record.GetString("workosUserId"); v != "user_wos_test" {
					t.Fatalf("Expected workosUserId %q, got %q", "user_wos_test", v)
				}

				findWorkOSExternalAuth(t, app, record, "user_wos_test")
			},
		},
		{
			Name:            "delegated collection with wrong WorkOS credentials",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-password",
			Body:            strings.NewReader(`{"identity":"test@example.com","password":"invalid-pass"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`, `Failed to authenticate`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindRecordById("users", "4q1xlclmfloku33")
				if err != nil {
					t.Fatal(err)
				}
				if record.GetString("workosUserId") != "" {
					t.Fatal("Expected no workosUserId sync on failed auth")
				}
			},
		},
		{
			Name:           "delegated collection with WorkOS MFA challenge",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-password",
			Body:           strings.NewReader(`{"identity":"mfa@example.com","password":"` + mockWorkOSPassword + `"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 401,
			ExpectedContent: []string{
				`"mfaId":"pending_token_mfa"`,
				`"id":"auth_factor_1"`,
				`"type":"totp"`,
			},
		},
		{
			Name:            "delegated collection with SSO-only email domain (sso_required)",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-password",
			Body:            strings.NewReader(`{"identity":"ssoonly@example.com","password":"` + mockWorkOSPassword + `"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`, `SSO authentication is required`},
		},
		{
			Name:            "delegated collection with unverified email (email_verification_required)",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-password",
			Body:            strings.NewReader(`{"identity":"unverified@example.com","password":"` + mockWorkOSPassword + `"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`, `email must be verified`},
		},
		{
			Name:           "delegated collection with organization linking",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-password",
			Body:           strings.NewReader(`{"identity":"org@example.com","password":"` + mockWorkOSPassword + `"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"organizationId":"org_wos_123"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				org, err := app.FindFirstRecordByData("organizations", "workosOrgId", "org_wos_123")
				if err != nil {
					t.Fatalf("Missing upserted organizations record: %v", err)
				}

				record, err := app.FindAuthRecordByEmail("users", "org@example.com")
				if err != nil {
					t.Fatal(err)
				}

				if v := record.GetString("organization"); v != org.Id {
					t.Fatalf("Expected record organization %q, got %q", org.Id, v)
				}
			},
		},
		{
			Name:   "delegated collection with suspended record",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-with-password",
			Body:   strings.NewReader(`{"identity":"test2@example.com","password":"` + mockWorkOSPassword + `"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				setupWorkOSApp(t, app, srv.URL)

				record, err := app.FindAuthRecordByEmail("users", "test2@example.com")
				if err != nil {
					t.Fatal(err)
				}
				record.Set("suspended", true)
				if err = app.Save(record); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:           "superusers collection remains native with WorkOS enabled",
			Method:         http.MethodPost,
			URL:            "/api/collections/_superusers/auth-with-password",
			Body:           strings.NewReader(`{"identity":"test@example.com","password":"1234567890"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"email":"test@example.com"`,
			},
		},
		{
			Name:           "delegated collection with non-email identity falls through to the native flow",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-password",
			Body:           strings.NewReader(`{"identity":"users75657","password":"1234567890"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"username":"users75657"`,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordAuthWithWorkOSMFA(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:            "not delegated collection",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-mfa",
			Body:            strings.NewReader(`{"mfaId":"pending_token_mfa","factorId":"auth_factor_1","code":"` + mockWorkOSTOTPCode + `"}`),
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "superusers collection",
			Method:          http.MethodPost,
			URL:             "/api/collections/_superusers/auth-with-mfa",
			Body:            strings.NewReader(`{"mfaId":"pending_token_mfa","factorId":"auth_factor_1","code":"` + mockWorkOSTOTPCode + `"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:           "missing body fields",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-mfa",
			Body:           strings.NewReader(`{}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"mfaId":{`,
				`"factorId":{`,
				`"code":{`,
			},
		},
		{
			Name:            "invalid TOTP code",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-mfa",
			Body:            strings.NewReader(`{"mfaId":"pending_token_mfa","factorId":"auth_factor_1","code":"000000"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`, `Failed to authenticate`},
		},
		{
			Name:           "valid TOTP code",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-mfa",
			Body:           strings.NewReader(`{"mfaId":"pending_token_mfa","factorId":"auth_factor_1","code":"` + mockWorkOSTOTPCode + `"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"email":"mfa@example.com"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail("users", "mfa@example.com")
				if err != nil {
					t.Fatalf("Missing created shadow record: %v", err)
				}

				findWorkOSExternalAuth(t, app, record, "user_wos_mfa")
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordAuthWithOTPWorkOS(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:           "request OTP for existing WorkOS user",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/request-otp",
			Body:           strings.NewReader(`{"email":"test@example.com"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"otpId":"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				payload := struct {
					OtpId string `json:"otpId"`
				}{}
				if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}

				email, _ := app.Store().Get("@workosMagicAuth_" + payload.OtpId).(string)
				if email != "test@example.com" {
					t.Fatalf("Expected stored magic auth email %q, got %q", "test@example.com", email)
				}
			},
		},
		{
			Name:           "request OTP for missing WorkOS user (dummy response)",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/request-otp",
			Body:           strings.NewReader(`{"email":"missing@example.com"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"otpId":"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				payload := struct {
					OtpId string `json:"otpId"`
				}{}
				if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}

				if app.Store().Has("@workosMagicAuth_" + payload.OtpId) {
					t.Fatal("Expected no stored magic auth session for missing WorkOS user")
				}
			},
		},
		{
			Name:            "auth with unknown otpId",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-otp",
			Body:            strings.NewReader(`{"otpId":"missing_otp_id","password":"` + mockWorkOSMagicAuthCode + `"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`, `Invalid or expired OTP`},
		},
		{
			Name:   "auth with invalid magic auth code",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-with-otp",
			Body:   strings.NewReader(`{"otpId":"otp_test_id_001","password":"000000"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				setupWorkOSApp(t, app, srv.URL)
				app.Store().Set("@workosMagicAuth_otp_test_id_001", "test@example.com")
			},
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{}`, `Invalid or expired OTP`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				// the pending session should be kept for retries
				if !app.Store().Has("@workosMagicAuth_otp_test_id_001") {
					t.Fatal("Expected the magic auth session to remain stored")
				}
			},
		},
		{
			Name:   "auth with valid magic auth code",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-with-otp",
			Body:   strings.NewReader(`{"otpId":"otp_test_id_002","password":"` + mockWorkOSMagicAuthCode + `"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				setupWorkOSApp(t, app, srv.URL)
				app.Store().Set("@workosMagicAuth_otp_test_id_002", "new@example.com")
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"email":"new@example.com"`,
				`"authenticationMethod":"MagicAuth"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if app.Store().Has("@workosMagicAuth_otp_test_id_002") {
					t.Fatal("Expected the magic auth session to be removed after successful auth")
				}

				record, err := app.FindAuthRecordByEmail("users", "new@example.com")
				if err != nil {
					t.Fatalf("Missing created shadow record: %v", err)
				}

				findWorkOSExternalAuth(t, app, record, "user_wos_new")
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordAuthWorkOSMethodsConvergence(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	setupWorkOSApp(t, app, srv.URL)

	pbRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}

	mux, err := pbRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	send := func(url string, body string) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
		req.Header.Set("content-type", "application/json")

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		return rec
	}

	// 1. password auth (creates the shadow record and the external auth link)
	res := send("/api/collections/users/auth-with-password", `{"identity":"new@example.com","password":"`+mockWorkOSPassword+`"}`)
	if res.Code != 200 {
		t.Fatalf("[password] expected status 200, got %d (%s)", res.Code, res.Body.String())
	}

	// 2. magic auth code exchange for the same WorkOS user
	app.Store().Set("@workosMagicAuth_otp_conv_id_001", "new@example.com")
	res = send("/api/collections/users/auth-with-otp", `{"otpId":"otp_conv_id_001","password":"`+mockWorkOSMagicAuthCode+`"}`)
	if res.Code != 200 {
		t.Fatalf("[magic auth] expected status 200, got %d (%s)", res.Code, res.Body.String())
	}

	// both auths must converge on a single record with a single external auth link
	total, err := app.CountRecords("users", dbx.HashExp{"email": "new@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("Expected exactly 1 record with the WorkOS user email, got %d", total)
	}

	record, err := app.FindAuthRecordByEmail("users", "new@example.com")
	if err != nil {
		t.Fatal(err)
	}

	rels, err := app.FindAllExternalAuthsByRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].Provider() != "workos" || rels[0].ProviderId() != "user_wos_new" {
		t.Fatalf("Expected exactly 1 workos external auth rel, got %v", rels)
	}
}

func TestRecordAuthWithWorkOSStart(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	seedOrg := func(t testing.TB, app *tests.TestApp, workosOrgId string, domains []string) {
		collection, err := app.FindCollectionByNameOrId("organizations")
		if err != nil {
			t.Fatal(err)
		}

		org := core.NewRecord(collection)
		org.Set("name", "Acme")
		org.Set("workosOrgId", workosOrgId)
		org.Set("domains", domains)
		if err = app.Save(org); err != nil {
			t.Fatal(err)
		}
	}

	scenarios := []tests.ApiScenario{
		{
			Name:            "WorkOS not enabled",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-workos",
			Body:            strings.NewReader(`{"provider":"GoogleOAuth"}`),
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "superusers collection",
			Method:          http.MethodPost,
			URL:             "/api/collections/_superusers/auth-with-workos",
			Body:            strings.NewReader(`{"provider":"GoogleOAuth"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "both email and provider",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-workos",
			Body:            strings.NewReader(`{"provider":"GoogleOAuth","email":"test@example.com"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"email":{`},
		},
		{
			Name:            "neither email nor provider",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-workos",
			Body:            strings.NewReader(`{}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"email":{`},
		},
		{
			Name:            "unsupported provider",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-workos",
			Body:            strings.NewReader(`{"provider":"FacebookOAuth"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"provider":{`},
		},
		{
			Name:           "social provider selector",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-workos",
			Body:           strings.NewReader(`{"provider":"GoogleOAuth"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"authURL":"`,
				`provider=GoogleOAuth`,
				`code_challenge=`,
				`code_challenge_method=S256`,
				`/user_management/authorize`,
				`"codeVerifier":"`,
				`"state":"`,
				`"codeChallengeMethod":"S256"`,
			},
		},
		{
			Name:   "email routed to a matching organization",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-with-workos",
			Body:   strings.NewReader(`{"email":"john@acme.com"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				setupWorkOSApp(t, app, srv.URL)
				seedOrg(t, app, "org_dom_1", []string{"acme.com", "acme.io"})
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"authURL":"`,
				`organization_id=org_dom_1`,
			},
		},
		{
			Name:   "email with a partially matching domain is not routed (exact json array match)",
			Method: http.MethodPost,
			URL:    "/api/collections/users/auth-with-workos",
			Body:   strings.NewReader(`{"email":"john@acme.company"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				setupWorkOSApp(t, app, srv.URL)
				seedOrg(t, app, "org_dom_1", []string{"acme.com"})
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"fallback":true`,
			},
		},
		{
			Name:           "email without a matching organization",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-workos",
			Body:           strings.NewReader(`{"email":"john@unknown.com"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"fallback":true`,
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordAuthPasswordResetWorkOS(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:           "delegated password reset request (existing user)",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/request-password-reset",
			Body:           strings.NewReader(`{"email":"test@example.com"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 204,
		},
		{
			Name:           "delegated password reset request (missing user is not revealed)",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/request-password-reset",
			Body:           strings.NewReader(`{"email":"missing@example.com"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 204,
		},
		{
			Name:            "delegated password reset confirm with invalid token",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/confirm-password-reset",
			Body:            strings.NewReader(`{"token":"invalid_token","password":"newpassword123","passwordConfirm":"newpassword123"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`Invalid or expired password reset token`},
		},
		{
			Name:            "delegated password reset confirm with mismatched passwords",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/confirm-password-reset",
			Body:            strings.NewReader(`{"token":"reset_tok_ok","password":"newpassword123","passwordConfirm":"different1234"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"passwordConfirm":{`},
		},
		{
			Name:            "delegated password reset confirm with too short password",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/confirm-password-reset",
			Body:            strings.NewReader(`{"token":"reset_tok_ok","password":"short","passwordConfirm":"short"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"password":{`},
		},
		{
			Name:           "delegated password reset confirm with valid token",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/confirm-password-reset",
			Body:           strings.NewReader(`{"token":"reset_tok_ok","password":"newpassword123","passwordConfirm":"newpassword123"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 204,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				// the mock returns the test@example.com WorkOS user with a verified email
				record, err := app.FindRecordById("users", "4q1xlclmfloku33")
				if err != nil {
					t.Fatal(err)
				}

				if !record.Verified() {
					t.Fatal("Expected the record to be marked as verified")
				}

				if v := record.GetString("workosUserId"); v != "user_wos_test" {
					t.Fatalf("Expected workosUserId %q, got %q", "user_wos_test", v)
				}
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordAuthVerificationWorkOS(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:           "delegated verification request",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/request-verification",
			Body:           strings.NewReader(`{"email":"test@example.com"}`),
			BeforeTestFunc: setup,
			Delay:          200 * time.Millisecond,
			ExpectedStatus: 204,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				// the resend throttle key is set only after the WorkOS
				// verification email request succeeded
				if !app.Store().Has("@limitVerificationEmail__pb_users_auth_4q1xlclmfloku33") {
					t.Fatal("Expected the verification resend throttle key to be set")
				}
			},
		},
		{
			Name:           "delegated verification request for missing user",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/request-verification",
			Body:           strings.NewReader(`{"email":"missing@example.com"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 204,
		},
		{
			Name:            "delegated verification confirm with invalid code",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/confirm-verification",
			Body:            strings.NewReader(`{"email":"test@example.com","code":"000000"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`Invalid or expired verification code`},
		},
		{
			Name:           "delegated verification confirm with valid code",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/confirm-verification",
			Body:           strings.NewReader(`{"email":"test@example.com","code":"` + mockWorkOSVerifyCode + `"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 204,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindRecordById("users", "4q1xlclmfloku33")
				if err != nil {
					t.Fatal(err)
				}

				if !record.Verified() {
					t.Fatal("Expected the record to be marked as verified")
				}
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordCreateWorkOSSignupInterception(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:           "signup with password creates the WorkOS user first",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/records",
			Body:           strings.NewReader(`{"email":"signup@example.com","password":"secret1234","passwordConfirm":"secret1234","name":"John Doe"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"id":"`,
				`"name":"John Doe"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail("users", "signup@example.com")
				if err != nil {
					t.Fatal(err)
				}

				if v := record.GetString("workosUserId"); v != "user_wos_created" {
					t.Fatalf("Expected workosUserId %q, got %q", "user_wos_created", v)
				}

				// the local record must have a random password since the
				// real credentials are stored in WorkOS
				if record.ValidatePassword("secret1234") {
					t.Fatal("Expected the submitted plain password to be replaced with a random one")
				}
			},
		},
		{
			Name:           "signup with an email already registered in WorkOS",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/records",
			Body:           strings.NewReader(`{"email":"exists@example.com","password":"secret1234","passwordConfirm":"secret1234"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 400,
			ExpectedContent: []string{
				`"email":{"code":"validation_workos_email_exists"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if _, err := app.FindAuthRecordByEmail("users", "exists@example.com"); err == nil {
					t.Fatal("Expected no local record to be created")
				}
			},
		},
		{
			Name:           "superusers signup interception is skipped",
			Method:         http.MethodPost,
			URL:            "/api/collections/_superusers/records",
			Body:           strings.NewReader(`{"email":"newsuper@example.com","password":"secret1234","passwordConfirm":"secret1234"}`),
			Headers:        map[string]string{"Authorization": superuserAuthToken},
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"email":"newsuper@example.com"`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "newsuper@example.com")
				if err != nil {
					t.Fatal(err)
				}

				// superusers remain fully native
				if !record.ValidatePassword("secret1234") {
					t.Fatal("Expected the submitted superuser password to be kept")
				}
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestRecordAuthWithOAuth2WorkOS(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:            "workos provider without WorkOS enabled",
			Method:          http.MethodPost,
			URL:             "/api/collections/users/auth-with-oauth2",
			Body:            strings.NewReader(`{"provider":"workos","code":"oauth_code_ok","codeVerifier":"ver"}`),
			ExpectedStatus:  400,
			ExpectedContent: []string{`"data":{`},
		},
		{
			Name:           "workos provider with synthesized provider config",
			Method:         http.MethodPost,
			URL:            "/api/collections/users/auth-with-oauth2",
			Body:           strings.NewReader(`{"provider":"workos","code":"oauth_code_ok","codeVerifier":"ver"}`),
			BeforeTestFunc: setup,
			ExpectedStatus: 200,
			ExpectedContent: []string{
				`"token":"`,
				`"email":"sso@example.com"`,
				`"isNew":true`,
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail("users", "sso@example.com")
				if err != nil {
					t.Fatal(err)
				}

				findWorkOSExternalAuth(t, app, record, "user_wos_sso")

				// the organization must be linked via the OnRecordAuthWithOAuth2Request hook
				org, err := app.FindFirstRecordByData("organizations", "workosOrgId", "org_wos_123")
				if err != nil {
					t.Fatalf("Missing upserted organizations record: %v", err)
				}

				if v := record.GetString("organization"); v != org.Id {
					t.Fatalf("Expected record organization %q, got %q", org.Id, v)
				}
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestWorkOSWebhooks(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	dsyncCreatedBody := `{"id":"event_1","event":"dsync.user.created","data":{"id":"directory_user_1","idp_id":"idp_1","organization_id":"org_dsync_1","first_name":"Dir","last_name":"User","state":"active","emails":[{"primary":true,"value":"dsync@example.com"}]}}`
	dsyncInactiveBody := `{"id":"event_2","event":"dsync.user.updated","data":{"id":"directory_user_2","state":"inactive","emails":[{"primary":true,"value":"dsync2@example.com"}]}}`
	dsyncDeletedBody := `{"id":"event_3","event":"dsync.user.deleted","data":{"id":"directory_user_3","state":"active","emails":[{"primary":true,"value":"test2@example.com"}]}}`
	unknownEventBody := `{"id":"event_4","event":"user.created","data":{"id":"user_123"}}`

	scenarios := []tests.ApiScenario{
		{
			Name:            "WorkOS not enabled",
			Method:          http.MethodPost,
			URL:             "/api/workos/webhooks",
			Body:            strings.NewReader(dsyncCreatedBody),
			Headers:         workosWebhookHeaders(mockWorkOSWebhookSecret, dsyncCreatedBody, time.Now()),
			ExpectedStatus:  404,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "missing signature header",
			Method:          http.MethodPost,
			URL:             "/api/workos/webhooks",
			Body:            strings.NewReader(dsyncCreatedBody),
			BeforeTestFunc:  setup,
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "invalid signature",
			Method:          http.MethodPost,
			URL:             "/api/workos/webhooks",
			Body:            strings.NewReader(dsyncCreatedBody),
			Headers:         workosWebhookHeaders("invalid_secret", dsyncCreatedBody, time.Now()),
			BeforeTestFunc:  setup,
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "stale signature timestamp",
			Method:          http.MethodPost,
			URL:             "/api/workos/webhooks",
			Body:            strings.NewReader(dsyncCreatedBody),
			Headers:         workosWebhookHeaders(mockWorkOSWebhookSecret, dsyncCreatedBody, time.Now().Add(-10*time.Minute)),
			BeforeTestFunc:  setup,
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "valid dsync.user.created",
			Method:          http.MethodPost,
			URL:             "/api/workos/webhooks",
			Body:            strings.NewReader(dsyncCreatedBody),
			Headers:         workosWebhookHeaders(mockWorkOSWebhookSecret, dsyncCreatedBody, time.Now()),
			BeforeTestFunc:  setup,
			ExpectedStatus:  200,
			ExpectedContent: []string{`"received":true`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail("users", "dsync@example.com")
				if err != nil {
					t.Fatalf("Missing created dsync shadow record: %v", err)
				}

				if !record.Verified() {
					t.Fatal("Expected the dsync shadow record to be verified")
				}

				if record.GetBool("suspended") {
					t.Fatal("Expected the active dsync shadow record to not be suspended")
				}

				if v := record.GetString("workosUserId"); v != "directory_user_1" {
					t.Fatalf("Expected workosUserId %q, got %q", "directory_user_1", v)
				}

				if v := record.GetString("name"); v != "Dir User" {
					t.Fatalf("Expected name %q, got %q", "Dir User", v)
				}

				org, err := app.FindFirstRecordByData("organizations", "workosOrgId", "org_dsync_1")
				if err != nil {
					t.Fatalf("Missing upserted organizations record: %v", err)
				}

				if v := record.GetString("organization"); v != org.Id {
					t.Fatalf("Expected record organization %q, got %q", org.Id, v)
				}
			},
		},
		{
			Name:    "valid dsync.user.updated with inactive state",
			Method:  http.MethodPost,
			URL:     "/api/workos/webhooks",
			Body:    strings.NewReader(dsyncInactiveBody),
			Headers: workosWebhookHeaders(mockWorkOSWebhookSecret, dsyncInactiveBody, time.Now()),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				setupWorkOSApp(t, app, srv.URL)

				// seed an existing provisioned record
				collection, err := app.FindCollectionByNameOrId("users")
				if err != nil {
					t.Fatal(err)
				}
				record := core.NewRecord(collection)
				record.SetEmail("dsync2@example.com")
				record.SetVerified(true)
				record.Set("workosUserId", "directory_user_2")
				record.SetRandomPassword()
				if err = app.Save(record); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"received":true`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail("users", "dsync2@example.com")
				if err != nil {
					t.Fatal(err)
				}

				if !record.GetBool("suspended") {
					t.Fatal("Expected the inactive dsync record to be suspended")
				}
			},
		},
		{
			Name:            "valid dsync.user.deleted suspends (and doesn't delete) the record",
			Method:          http.MethodPost,
			URL:             "/api/workos/webhooks",
			Body:            strings.NewReader(dsyncDeletedBody),
			Headers:         workosWebhookHeaders(mockWorkOSWebhookSecret, dsyncDeletedBody, time.Now()),
			BeforeTestFunc:  setup,
			ExpectedStatus:  200,
			ExpectedContent: []string{`"received":true`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				record, err := app.FindAuthRecordByEmail("users", "test2@example.com")
				if err != nil {
					t.Fatalf("Expected the record to still exist: %v", err)
				}

				if !record.GetBool("suspended") {
					t.Fatal("Expected the deleted dsync record to be suspended")
				}
			},
		},
		{
			Name:            "unknown event is acknowledged",
			Method:          http.MethodPost,
			URL:             "/api/workos/webhooks",
			Body:            strings.NewReader(unknownEventBody),
			Headers:         workosWebhookHeaders(mockWorkOSWebhookSecret, unknownEventBody, time.Now()),
			BeforeTestFunc:  setup,
			ExpectedStatus:  200,
			ExpectedContent: []string{`"received":true`},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestWorkOSPortalLink(t *testing.T) {
	t.Parallel()

	srv := newMockWorkOSServer()
	defer srv.Close()

	setup := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		setupWorkOSApp(t, app, srv.URL)
	}

	scenarios := []tests.ApiScenario{
		{
			Name:            "guest",
			Method:          http.MethodPost,
			URL:             "/api/workos/portal-link",
			Body:            strings.NewReader(`{"organization":"org_wos_123","intent":"sso"}`),
			BeforeTestFunc:  setup,
			ExpectedStatus:  401,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:   "regular auth record",
			Method: http.MethodPost,
			URL:    "/api/workos/portal-link",
			Body:   strings.NewReader(`{"organization":"org_wos_123","intent":"sso"}`),
			Headers: map[string]string{
				"Authorization": "eyJhbGciOiJIUzI1NiJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsInR5cGUiOiJhdXRoIiwiY29sbGVjdGlvbklkIjoiX3BiX3VzZXJzX2F1dGhfIiwiZXhwIjoyNTI0NjA0NDYxLCJyZWZyZXNoYWJsZSI6dHJ1ZX0.ZT3F0Z3iM-xbGgSG3LEKiEzHrPHr8t8IuHLZGGNuxLo",
			},
			BeforeTestFunc:  setup,
			ExpectedStatus:  403,
			ExpectedContent: []string{`"data":{}`},
		},
		{
			Name:            "superuser with invalid intent",
			Method:          http.MethodPost,
			URL:             "/api/workos/portal-link",
			Body:            strings.NewReader(`{"organization":"org_wos_123","intent":"audit_logs"}`),
			Headers:         map[string]string{"Authorization": superuserAuthToken},
			BeforeTestFunc:  setup,
			ExpectedStatus:  400,
			ExpectedContent: []string{`"intent":{`},
		},
		{
			Name:            "superuser with a direct WorkOS organization id",
			Method:          http.MethodPost,
			URL:             "/api/workos/portal-link",
			Body:            strings.NewReader(`{"organization":"org_wos_123","intent":"sso"}`),
			Headers:         map[string]string{"Authorization": superuserAuthToken},
			BeforeTestFunc:  setup,
			ExpectedStatus:  200,
			ExpectedContent: []string{`"link":"https://portal.workos.test/sso/org_wos_123"`},
		},
		{
			Name:    "superuser with an organizations record id",
			Method:  http.MethodPost,
			URL:     "/api/workos/portal-link",
			Body:    strings.NewReader(`{"organization":"orgrecid1234567","intent":"dsync"}`),
			Headers: map[string]string{"Authorization": superuserAuthToken},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				setupWorkOSApp(t, app, srv.URL)

				collection, err := app.FindCollectionByNameOrId("organizations")
				if err != nil {
					t.Fatal(err)
				}
				org := core.NewRecord(collection)
				org.Id = "orgrecid1234567"
				org.Set("name", "Acme")
				org.Set("workosOrgId", "org_wos_777")
				if err = app.Save(org); err != nil {
					t.Fatal(err)
				}
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"link":"https://portal.workos.test/dsync/org_wos_777"`},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

// workosRefreshTestKey is a valid 32-char AES key for the encrypted
// refresh-token storage in the refresh-sync tests.
const workosRefreshTestKey = "1234567890abcdef1234567890abcdef"

// newWorkOSRefreshTestMux builds a mux-backed test app configured for the
// WorkOS auth-refresh session syncing and returns a send() helper.
//
// It is intentionally NOT parallel-safe (the caller sets the app encryption
// key via t.Setenv), so callers must not mark themselves parallel.
func newWorkOSRefreshTestMux(t testing.TB, apiURL string, syncOnRefresh, exposeTokens bool) (*tests.TestApp, func(method, url, token, body string) *httptest.ResponseRecorder) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	setupWorkOSApp(t, app, apiURL)
	app.Settings().WorkOS.SyncOnRefresh = syncOnRefresh
	app.Settings().WorkOS.ExposeTokens = exposeTokens

	pbRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := pbRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	send := func(method, url, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("content-type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	return app, send
}

type workosAuthRespBody struct {
	Token string `json:"token"`
	Meta  struct {
		WorkOS map[string]any `json:"workos"`
	} `json:"meta"`
}

func parseWorkOSAuthResp(t testing.TB, rec *httptest.ResponseRecorder) workosAuthRespBody {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("expected status 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var out workosAuthRespBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse auth response: %v (%s)", err, rec.Body.String())
	}
	if out.Token == "" {
		t.Fatalf("missing token in auth response: %s", rec.Body.String())
	}
	return out
}

func TestRecordAuthWorkOSRefreshSync(t *testing.T) {
	// not parallel: mutates the app encryption key env var
	t.Setenv("pb_test_env", workosRefreshTestKey)

	srv := newMockWorkOSServer()
	defer srv.Close()

	app, send := newWorkOSRefreshTestMux(t, srv.URL, true, false)

	// 1. password login persists the encrypted refresh token
	login := parseWorkOSAuthResp(t, send(http.MethodPost, "/api/collections/users/auth-with-password", "", `{"identity":"org@example.com","password":"`+mockWorkOSPassword+`"}`))

	// the hidden field must not leak in the login response
	if strings.Contains(login.Meta.WorkOS["authenticationMethod"].(string), "wos_refresh") ||
		strings.Contains(string(mustJSON(t, login.Meta.WorkOS)), "wos_refresh") {
		t.Fatalf("refresh token leaked in the auth response meta: %v", login.Meta.WorkOS)
	}

	record, err := app.FindAuthRecordByEmail("users", "org@example.com")
	if err != nil {
		t.Fatal(err)
	}

	stored := record.GetString("workosRefreshToken")
	if stored == "" {
		t.Fatal("expected a stored workosRefreshToken after login")
	}
	if strings.Contains(stored, "wos_refresh_") {
		t.Fatalf("refresh token appears to be stored in plaintext: %q", stored)
	}
	dec, err := security.Decrypt(stored, workosRefreshTestKey)
	if err != nil {
		t.Fatalf("stored refresh token is not decryptable: %v", err)
	}
	if string(dec) != "wos_refresh_user_wos_org" {
		t.Fatalf("unexpected stored refresh token %q", string(dec))
	}

	// 2. auth-refresh re-validates the WorkOS session, rotates + re-persists
	refresh := parseWorkOSAuthResp(t, send(http.MethodPost, "/api/collections/users/auth-refresh", login.Token, ""))
	if m := refresh.Meta.WorkOS["authenticationMethod"]; m != "RefreshToken" {
		t.Fatalf("expected refreshed meta authenticationMethod=RefreshToken, got %v", m)
	}

	record, err = app.FindAuthRecordByEmail("users", "org@example.com")
	if err != nil {
		t.Fatal(err)
	}
	dec, err = security.Decrypt(record.GetString("workosRefreshToken"), workosRefreshTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != "wos_refresh_user_wos_org_r2" {
		t.Fatalf("expected the rotated refresh token to be persisted, got %q", string(dec))
	}
}

func TestRecordAuthWorkOSRefreshRejectedWhenSessionRevoked(t *testing.T) {
	t.Setenv("pb_test_env", workosRefreshTestKey)

	srv := newMockWorkOSServer()
	defer srv.Close()

	app, send := newWorkOSRefreshTestMux(t, srv.URL, true, false)

	login := parseWorkOSAuthResp(t, send(http.MethodPost, "/api/collections/users/auth-with-password", "", `{"identity":"org@example.com","password":"`+mockWorkOSPassword+`"}`))

	// simulate WorkOS having revoked the session by storing a token the
	// mock rejects with invalid_grant
	record, err := app.FindAuthRecordByEmail("users", "org@example.com")
	if err != nil {
		t.Fatal(err)
	}
	enc, err := security.Encrypt([]byte("wos_refresh_revoked"), workosRefreshTestKey)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("workosRefreshToken", enc)
	if err = app.Save(record); err != nil {
		t.Fatal(err)
	}

	rec := send(http.MethodPost, "/api/collections/users/auth-refresh", login.Token, "")
	if rec.Code != 401 {
		t.Fatalf("expected the refresh to be rejected with 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestRecordAuthWorkOSRefreshExposeTokens(t *testing.T) {
	t.Setenv("pb_test_env", workosRefreshTestKey)

	srv := newMockWorkOSServer()
	defer srv.Close()

	_, send := newWorkOSRefreshTestMux(t, srv.URL, true, true)

	login := parseWorkOSAuthResp(t, send(http.MethodPost, "/api/collections/users/auth-with-password", "", `{"identity":"org@example.com","password":"`+mockWorkOSPassword+`"}`))

	if login.Meta.WorkOS["accessToken"] != "wos_access_user_wos_org" {
		t.Fatalf("expected the access token to be exposed in the meta, got %v", login.Meta.WorkOS["accessToken"])
	}
	if login.Meta.WorkOS["refreshToken"] != "wos_refresh_user_wos_org" {
		t.Fatalf("expected the refresh token to be exposed in the meta, got %v", login.Meta.WorkOS["refreshToken"])
	}
}

func TestRecordAuthWorkOSRefreshNoEncryptionKey(t *testing.T) {
	// explicitly clear the encryption key -> the refresh token must not be
	// written in plaintext and the refresh must still succeed (fail open)
	t.Setenv("pb_test_env", "")

	srv := newMockWorkOSServer()
	defer srv.Close()

	app, send := newWorkOSRefreshTestMux(t, srv.URL, true, false)

	login := parseWorkOSAuthResp(t, send(http.MethodPost, "/api/collections/users/auth-with-password", "", `{"identity":"org@example.com","password":"`+mockWorkOSPassword+`"}`))

	record, err := app.FindAuthRecordByEmail("users", "org@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if v := record.GetString("workosRefreshToken"); v != "" {
		t.Fatalf("expected no stored refresh token without an encryption key, got %q", v)
	}

	// with nothing stored, the refresh should still proceed (fail open)
	rec := send(http.MethodPost, "/api/collections/users/auth-refresh", login.Token, "")
	if rec.Code != 200 {
		t.Fatalf("expected the refresh to proceed (200) with no stored token, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
