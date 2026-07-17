package workos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const (
	testClientId = "client_test_id"
	testAPIKey   = "sk_test_key"
)

// testServer starts a httptest server that asserts the expected request
// method, path (incl. query), auth header and body and responds with the
// provided status and response body.
func testServer(t *testing.T, expectedMethod string, expectedPath string, expectedAuth string, expectedBody string, status int, response string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != expectedMethod {
			t.Errorf("Expected method %q, got %q", expectedMethod, r.Method)
		}

		if r.URL.RequestURI() != expectedPath {
			t.Errorf("Expected path %q, got %q", expectedPath, r.URL.RequestURI())
		}

		if auth := r.Header.Get("Authorization"); auth != expectedAuth {
			t.Errorf("Expected Authorization header %q, got %q", expectedAuth, auth)
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Expected Content-Type header %q, got %q", "application/json", ct)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
		}

		if expectedBody == "" {
			if len(body) != 0 {
				t.Errorf("Expected empty request body, got %s", body)
			}
		} else {
			var expected, actual any
			if err := json.Unmarshal([]byte(expectedBody), &expected); err != nil {
				t.Fatalf("Invalid expectedBody test fixture: %v", err)
			}
			if err := json.Unmarshal(body, &actual); err != nil {
				t.Fatalf("Failed to unmarshal request body %s: %v", body, err)
			}

			expectedRaw, _ := json.Marshal(expected)
			actualRaw, _ := json.Marshal(actual)
			if string(expectedRaw) != string(actualRaw) {
				t.Errorf("Expected request body\n%s\ngot\n%s", expectedRaw, actualRaw)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, response)
	}))
}

func testClient(baseURL string) *Client {
	return &Client{
		ClientId: testClientId,
		APIKey:   testAPIKey,
		BaseURL:  baseURL,
	}
}

const testUserJSON = `{
	"id": "user_123",
	"email": "test@example.com",
	"email_verified": true,
	"first_name": "John",
	"last_name": "Doe",
	"profile_picture_url": "https://example.com/pic.png",
	"created_at": "2024-01-01T00:00:00.000Z",
	"updated_at": "2024-01-02T00:00:00.000Z"
}`

func checkTestUser(t *testing.T, user *User) {
	t.Helper()

	if user == nil {
		t.Fatal("Expected user, got nil")
	}

	if user.Id != "user_123" {
		t.Errorf("Expected user id %q, got %q", "user_123", user.Id)
	}
	if user.Email != "test@example.com" {
		t.Errorf("Expected user email %q, got %q", "test@example.com", user.Email)
	}
	if !user.EmailVerified {
		t.Error("Expected user email_verified to be true")
	}
	if user.FirstName != "John" {
		t.Errorf("Expected user first_name %q, got %q", "John", user.FirstName)
	}
	if user.LastName != "Doe" {
		t.Errorf("Expected user last_name %q, got %q", "Doe", user.LastName)
	}
	if user.ProfilePictureURL != "https://example.com/pic.png" {
		t.Errorf("Expected user profile_picture_url %q, got %q", "https://example.com/pic.png", user.ProfilePictureURL)
	}
	if user.CreatedAt != "2024-01-01T00:00:00.000Z" {
		t.Errorf("Expected user created_at %q, got %q", "2024-01-01T00:00:00.000Z", user.CreatedAt)
	}
	if user.UpdatedAt != "2024-01-02T00:00:00.000Z" {
		t.Errorf("Expected user updated_at %q, got %q", "2024-01-02T00:00:00.000Z", user.UpdatedAt)
	}
}

var testAuthResponseJSON = `{
	"user": ` + testUserJSON + `,
	"organization_id": "org_123",
	"access_token": "access_test",
	"refresh_token": "refresh_test",
	"authentication_method": "Password"
}`

func checkTestAuthResponse(t *testing.T, result *AuthResponse) {
	t.Helper()

	if result == nil {
		t.Fatal("Expected auth response, got nil")
	}

	checkTestUser(t, &result.User)

	if result.OrganizationId != "org_123" {
		t.Errorf("Expected organization_id %q, got %q", "org_123", result.OrganizationId)
	}
	if result.AccessToken != "access_test" {
		t.Errorf("Expected access_token %q, got %q", "access_test", result.AccessToken)
	}
	if result.RefreshToken != "refresh_test" {
		t.Errorf("Expected refresh_token %q, got %q", "refresh_test", result.RefreshToken)
	}
	if result.AuthenticationMethod != "Password" {
		t.Errorf("Expected authentication_method %q, got %q", "Password", result.AuthenticationMethod)
	}
}

func TestAuthenticateMethods(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		name         string
		expectedBody string
		call         func(c *Client) (*AuthResponse, error)
	}{
		{
			"AuthenticateWithPassword",
			`{
				"client_id": "` + testClientId + `",
				"client_secret": "` + testAPIKey + `",
				"grant_type": "password",
				"email": "test@example.com",
				"password": "secret",
				"ip_address": "127.0.0.1",
				"user_agent": "test_ua"
			}`,
			func(c *Client) (*AuthResponse, error) {
				return c.AuthenticateWithPassword(context.Background(), "test@example.com", "secret", "127.0.0.1", "test_ua")
			},
		},
		{
			"AuthenticateWithPassword (without optional fields)",
			`{
				"client_id": "` + testClientId + `",
				"client_secret": "` + testAPIKey + `",
				"grant_type": "password",
				"email": "test@example.com",
				"password": "secret"
			}`,
			func(c *Client) (*AuthResponse, error) {
				return c.AuthenticateWithPassword(context.Background(), "test@example.com", "secret", "", "")
			},
		},
		{
			"AuthenticateWithCode",
			`{
				"client_id": "` + testClientId + `",
				"client_secret": "` + testAPIKey + `",
				"grant_type": "authorization_code",
				"code": "code_123",
				"code_verifier": "verifier_123"
			}`,
			func(c *Client) (*AuthResponse, error) {
				return c.AuthenticateWithCode(context.Background(), "code_123", "verifier_123")
			},
		},
		{
			"AuthenticateWithCode (without code verifier)",
			`{
				"client_id": "` + testClientId + `",
				"client_secret": "` + testAPIKey + `",
				"grant_type": "authorization_code",
				"code": "code_123"
			}`,
			func(c *Client) (*AuthResponse, error) {
				return c.AuthenticateWithCode(context.Background(), "code_123", "")
			},
		},
		{
			"AuthenticateWithMagicAuth",
			`{
				"client_id": "` + testClientId + `",
				"client_secret": "` + testAPIKey + `",
				"grant_type": "urn:workos:oauth:grant-type:magic-auth:code",
				"code": "123456",
				"email": "test@example.com"
			}`,
			func(c *Client) (*AuthResponse, error) {
				return c.AuthenticateWithMagicAuth(context.Background(), "123456", "test@example.com")
			},
		},
		{
			"AuthenticateWithTOTP",
			`{
				"client_id": "` + testClientId + `",
				"client_secret": "` + testAPIKey + `",
				"grant_type": "urn:workos:oauth:grant-type:mfa-totp",
				"code": "123456",
				"pending_authentication_token": "pending_123",
				"authentication_challenge_id": "auth_challenge_123"
			}`,
			func(c *Client) (*AuthResponse, error) {
				return c.AuthenticateWithTOTP(context.Background(), "123456", "pending_123", "auth_challenge_123")
			},
		},
		{
			"AuthenticateWithRefreshToken",
			`{
				"client_id": "` + testClientId + `",
				"client_secret": "` + testAPIKey + `",
				"grant_type": "refresh_token",
				"refresh_token": "refresh_123"
			}`,
			func(c *Client) (*AuthResponse, error) {
				return c.AuthenticateWithRefreshToken(context.Background(), "refresh_123")
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			// note: the authenticate endpoint must NOT send an Authorization header
			srv := testServer(t, http.MethodPost, "/user_management/authenticate", "", s.expectedBody, http.StatusOK, testAuthResponseJSON)
			defer srv.Close()

			result, err := s.call(testClient(srv.URL))
			if err != nil {
				t.Fatalf("Expected nil error, got %v", err)
			}

			checkTestAuthResponse(t, result)
		})
	}
}

func TestCreateUser(t *testing.T) {
	t.Parallel()

	expectedBody := `{
		"email": "test@example.com",
		"password": "secret",
		"first_name": "John",
		"last_name": "Doe",
		"email_verified": true
	}`

	srv := testServer(t, http.MethodPost, "/user_management/users", "Bearer "+testAPIKey, expectedBody, http.StatusCreated, testUserJSON)
	defer srv.Close()

	user, err := testClient(srv.URL).CreateUser(context.Background(), CreateUserOpts{
		Email:         "test@example.com",
		Password:      "secret",
		FirstName:     "John",
		LastName:      "Doe",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	checkTestUser(t, user)
}

func TestUpdateUser(t *testing.T) {
	t.Parallel()

	expectedBody := `{
		"email": "new@example.com",
		"email_verified": true
	}`

	srv := testServer(t, http.MethodPut, "/user_management/users/user_123", "Bearer "+testAPIKey, expectedBody, http.StatusOK, testUserJSON)
	defer srv.Close()

	user, err := testClient(srv.URL).UpdateUser(context.Background(), "user_123", UpdateUserOpts{
		Email:         "new@example.com",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	checkTestUser(t, user)
}

func TestGetUserByEmail(t *testing.T) {
	t.Parallel()

	t.Run("existing user", func(t *testing.T) {
		srv := testServer(t, http.MethodGet, "/user_management/users?email=test%40example.com&limit=1", "Bearer "+testAPIKey, "", http.StatusOK, `{"data":[`+testUserJSON+`],"list_metadata":{}}`)
		defer srv.Close()

		user, err := testClient(srv.URL).GetUserByEmail(context.Background(), "test@example.com")
		if err != nil {
			t.Fatalf("Expected nil error, got %v", err)
		}

		checkTestUser(t, user)
	})

	t.Run("missing user", func(t *testing.T) {
		srv := testServer(t, http.MethodGet, "/user_management/users?email=missing%40example.com&limit=1", "Bearer "+testAPIKey, "", http.StatusOK, `{"data":[],"list_metadata":{}}`)
		defer srv.Close()

		user, err := testClient(srv.URL).GetUserByEmail(context.Background(), "missing@example.com")
		if !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("Expected ErrUserNotFound, got %v", err)
		}
		if user != nil {
			t.Fatalf("Expected nil user, got %v", user)
		}
	})
}

func TestCreateMagicAuth(t *testing.T) {
	t.Parallel()

	response := `{
		"id": "magic_auth_123",
		"user_id": "user_123",
		"email": "test@example.com",
		"expires_at": "2024-01-01T00:10:00.000Z",
		"code": "123456",
		"created_at": "2024-01-01T00:00:00.000Z",
		"updated_at": "2024-01-01T00:00:00.000Z"
	}`

	srv := testServer(t, http.MethodPost, "/user_management/magic_auth", "Bearer "+testAPIKey, `{"email":"test@example.com"}`, http.StatusCreated, response)
	defer srv.Close()

	result, err := testClient(srv.URL).CreateMagicAuth(context.Background(), "test@example.com")
	if err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	if result.Id != "magic_auth_123" {
		t.Errorf("Expected id %q, got %q", "magic_auth_123", result.Id)
	}
	if result.UserId != "user_123" {
		t.Errorf("Expected user_id %q, got %q", "user_123", result.UserId)
	}
	if result.Email != "test@example.com" {
		t.Errorf("Expected email %q, got %q", "test@example.com", result.Email)
	}
	if result.Code != "123456" {
		t.Errorf("Expected code %q, got %q", "123456", result.Code)
	}
	if result.ExpiresAt != "2024-01-01T00:10:00.000Z" {
		t.Errorf("Expected expires_at %q, got %q", "2024-01-01T00:10:00.000Z", result.ExpiresAt)
	}
}

func TestCreatePasswordReset(t *testing.T) {
	t.Parallel()

	response := `{
		"id": "password_reset_123",
		"user_id": "user_123",
		"email": "test@example.com",
		"password_reset_token": "token_123",
		"password_reset_url": "https://example.com/reset?token=token_123",
		"expires_at": "2024-01-01T00:10:00.000Z",
		"created_at": "2024-01-01T00:00:00.000Z"
	}`

	srv := testServer(t, http.MethodPost, "/user_management/password_reset", "Bearer "+testAPIKey, `{"email":"test@example.com"}`, http.StatusCreated, response)
	defer srv.Close()

	result, err := testClient(srv.URL).CreatePasswordReset(context.Background(), "test@example.com")
	if err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	if result.Id != "password_reset_123" {
		t.Errorf("Expected id %q, got %q", "password_reset_123", result.Id)
	}
	if result.PasswordResetToken != "token_123" {
		t.Errorf("Expected password_reset_token %q, got %q", "token_123", result.PasswordResetToken)
	}
	if result.PasswordResetUrl != "https://example.com/reset?token=token_123" {
		t.Errorf("Expected password_reset_url %q, got %q", "https://example.com/reset?token=token_123", result.PasswordResetUrl)
	}
}

func TestConfirmPasswordReset(t *testing.T) {
	t.Parallel()

	expectedBody := `{"token":"token_123","new_password":"new_secret"}`

	srv := testServer(t, http.MethodPost, "/user_management/password_reset/confirm", "Bearer "+testAPIKey, expectedBody, http.StatusOK, `{"user":`+testUserJSON+`}`)
	defer srv.Close()

	user, err := testClient(srv.URL).ConfirmPasswordReset(context.Background(), "token_123", "new_secret")
	if err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	checkTestUser(t, user)
}

func TestSendVerificationEmail(t *testing.T) {
	t.Parallel()

	srv := testServer(t, http.MethodPost, "/user_management/users/user_123/email_verification/send", "Bearer "+testAPIKey, "", http.StatusOK, `{"user":`+testUserJSON+`}`)
	defer srv.Close()

	if err := testClient(srv.URL).SendVerificationEmail(context.Background(), "user_123"); err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}
}

func TestVerifyEmail(t *testing.T) {
	t.Parallel()

	srv := testServer(t, http.MethodPost, "/user_management/users/user_123/email_verification/confirm", "Bearer "+testAPIKey, `{"code":"123456"}`, http.StatusOK, `{"user":`+testUserJSON+`}`)
	defer srv.Close()

	user, err := testClient(srv.URL).VerifyEmail(context.Background(), "user_123", "123456")
	if err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	checkTestUser(t, user)
}

func TestGeneratePortalLink(t *testing.T) {
	t.Parallel()

	expectedBody := `{"intent":"sso","organization":"org_123"}`

	srv := testServer(t, http.MethodPost, "/portal/generate_link", "Bearer "+testAPIKey, expectedBody, http.StatusCreated, `{"link":"https://setup.workos.com/portal/launch?secret=test"}`)
	defer srv.Close()

	link, err := testClient(srv.URL).GeneratePortalLink(context.Background(), "org_123", "sso")
	if err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	if link != "https://setup.workos.com/portal/launch?secret=test" {
		t.Fatalf("Expected portal link %q, got %q", "https://setup.workos.com/portal/launch?secret=test", link)
	}
}

func TestAPIErrorParsing(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		name                 string
		status               int
		response             string
		expectedCode         string
		expectedMessage      string
		expectedPendingToken string
		expectedFactors      int
		expectedMFARequired  bool
	}{
		{
			"code+message error",
			http.StatusNotFound,
			`{"code":"entity_not_found","message":"User not found."}`,
			"entity_not_found",
			"User not found.",
			"",
			0,
			false,
		},
		{
			"error+error_description error",
			http.StatusBadRequest,
			`{"error":"invalid_grant","error_description":"The code has expired."}`,
			"invalid_grant",
			"The code has expired.",
			"",
			0,
			false,
		},
		{
			"message only error",
			http.StatusUnauthorized,
			`{"message":"Unauthorized"}`,
			"",
			"Unauthorized",
			"",
			0,
			false,
		},
		{
			"non-JSON error",
			http.StatusInternalServerError,
			`Internal Server Error`,
			"",
			"Internal Server Error",
			"",
			0,
			false,
		},
		{
			"mfa challenge error",
			http.StatusUnprocessableEntity,
			`{
				"code": "mfa_challenge",
				"message": "The user must complete an MFA challenge to finish authenticating.",
				"pending_authentication_token": "pending_123",
				"authentication_factors": [{"id": "auth_factor_123", "type": "totp"}],
				"user": {"id": "user_123", "email": "test@example.com"}
			}`,
			"mfa_challenge",
			"The user must complete an MFA challenge to finish authenticating.",
			"pending_123",
			1,
			true,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(s.status)
				fmt.Fprint(w, s.response)
			}))
			defer srv.Close()

			_, err := testClient(srv.URL).AuthenticateWithPassword(context.Background(), "test@example.com", "secret", "", "")
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Expected *APIError, got %T (%v)", err, err)
			}

			if apiErr.Status != s.status {
				t.Errorf("Expected status %d, got %d", s.status, apiErr.Status)
			}
			if apiErr.Code != s.expectedCode {
				t.Errorf("Expected code %q, got %q", s.expectedCode, apiErr.Code)
			}
			if apiErr.Message != s.expectedMessage {
				t.Errorf("Expected message %q, got %q", s.expectedMessage, apiErr.Message)
			}
			if apiErr.PendingAuthenticationToken != s.expectedPendingToken {
				t.Errorf("Expected pending token %q, got %q", s.expectedPendingToken, apiErr.PendingAuthenticationToken)
			}
			if len(apiErr.AuthenticationFactors) != s.expectedFactors {
				t.Errorf("Expected %d authentication factors, got %d", s.expectedFactors, len(apiErr.AuthenticationFactors))
			}
			if apiErr.IsMFARequired() != s.expectedMFARequired {
				t.Errorf("Expected IsMFARequired() %v, got %v", s.expectedMFARequired, apiErr.IsMFARequired())
			}
			if apiErr.RawBody != s.response {
				t.Errorf("Expected raw body\n%s\ngot\n%s", s.response, apiErr.RawBody)
			}
			if apiErr.Error() == "" {
				t.Error("Expected non-empty Error() message")
			}

			if s.expectedFactors > 0 {
				if apiErr.AuthenticationFactors[0].Id != "auth_factor_123" {
					t.Errorf("Expected factor id %q, got %q", "auth_factor_123", apiErr.AuthenticationFactors[0].Id)
				}
				if apiErr.AuthenticationFactors[0].Type != "totp" {
					t.Errorf("Expected factor type %q, got %q", "totp", apiErr.AuthenticationFactors[0].Type)
				}
			}
		})
	}
}

func TestDefaultBaseURL(t *testing.T) {
	t.Parallel()

	c := Client{}

	// ensure that requests without an explicit BaseURL target the default API url
	req, err := http.NewRequest(http.MethodGet, DefaultBaseURL, nil)
	if err != nil || req.URL.Host != "api.workos.com" {
		t.Fatalf("Expected valid default base url, got %q (%v)", DefaultBaseURL, err)
	}

	if c.BaseURL != "" {
		t.Fatalf("Expected empty BaseURL by default, got %q", c.BaseURL)
	}
}

// -------------------------------------------------------------------

func testWebhookSignatureHeader(secret string, body []byte, timestamp time.Time) string {
	rawTimestamp := strconv.FormatInt(timestamp.UnixMilli(), 10)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(rawTimestamp + "." + string(body)))

	return "t=" + rawTimestamp + ", v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature(t *testing.T) {
	t.Parallel()

	const secret = "whsec_test_secret"

	now := time.Now()
	body := []byte(`{"id":"wh_123","event":"user.created","data":{"id":"user_123"}}`)
	validHeader := testWebhookSignatureHeader(secret, body, now)

	scenarios := []struct {
		name        string
		header      string
		body        []byte
		secret      string
		tolerance   time.Duration
		now         time.Time
		expectedErr error
	}{
		{
			"valid signature",
			validHeader,
			body,
			secret,
			3 * time.Minute,
			now,
			nil,
		},
		{
			"valid signature (no spaces in header)",
			// some proxies may normalize the header spaces
			"t=" + strconv.FormatInt(now.UnixMilli(), 10) + ",v1=" + validHeader[len("t=1234567890123, v1="):],
			body,
			secret,
			3 * time.Minute,
			now,
			nil,
		},
		{
			"valid signature (zero tolerance skips the timestamp check)",
			testWebhookSignatureHeader(secret, body, now.Add(-24*time.Hour)),
			body,
			secret,
			0,
			now,
			nil,
		},
		{
			"empty header",
			"",
			body,
			secret,
			3 * time.Minute,
			now,
			ErrMissingSignatureHeader,
		},
		{
			"malformed header (no key-value pairs)",
			"invalid",
			body,
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignatureHeader,
		},
		{
			"malformed header (missing v1)",
			"t=" + strconv.FormatInt(now.UnixMilli(), 10),
			body,
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignatureHeader,
		},
		{
			"malformed header (missing t)",
			"v1=abcdef",
			body,
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignatureHeader,
		},
		{
			"malformed header (non-numeric timestamp)",
			"t=abc, v1=abcdef",
			body,
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignatureHeader,
		},
		{
			"stale timestamp",
			testWebhookSignatureHeader(secret, body, now.Add(-10*time.Minute)),
			body,
			secret,
			3 * time.Minute,
			now,
			ErrExpiredSignature,
		},
		{
			"wrong secret",
			testWebhookSignatureHeader("whsec_other", body, now),
			body,
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignature,
		},
		{
			"tampered body",
			validHeader,
			[]byte(`{"id":"wh_123","event":"user.created","data":{"id":"user_456"}}`),
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignature,
		},
		{
			"tampered timestamp",
			"t=" + strconv.FormatInt(now.Add(time.Minute).UnixMilli(), 10) + ", v1=" + validHeader[len("t=1234567890123, v1="):],
			body,
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignature,
		},
		{
			"non-hex signature",
			"t=" + strconv.FormatInt(now.UnixMilli(), 10) + ", v1=not_hex!",
			body,
			secret,
			3 * time.Minute,
			now,
			ErrInvalidSignature,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			err := VerifyWebhookSignature(s.header, s.body, s.secret, s.tolerance, s.now)

			if !errors.Is(err, s.expectedErr) {
				t.Fatalf("Expected error %v, got %v", s.expectedErr, err)
			}
		})
	}
}
