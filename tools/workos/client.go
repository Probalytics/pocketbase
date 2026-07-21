// Package workos implements a minimal WorkOS User Management API client
// (https://workos.com/docs/reference/user-management).
//
// It intentionally covers only the small API subset needed by PocketBase
// and is written without external dependencies.
//
// The endpoint paths, request/response fields, auth headers and grant_type
// strings below were extracted from the official Go SDK
// (github.com/workos/workos-go/v4@v4.46.1, pkg/usermanagement, pkg/portal
// and pkg/webhooks packages).
package workos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the default WorkOS API base url.
const DefaultBaseURL = "https://api.workos.com"

// Client is a thin WorkOS User Management API client.
type Client struct {
	// HTTPClient is the http client to use for the API requests
	// (if not set, defaults to a client with 30s timeout).
	HTTPClient *http.Client

	// ClientId is the WorkOS environment Client ID
	// (sent as "client_id" in the authenticate requests body).
	ClientId string

	// APIKey is the WorkOS environment secret API key.
	//
	// It is sent as "Authorization: Bearer <APIKey>" header for the
	// management endpoints and as "client_secret" body field for the
	// /user_management/authenticate endpoint.
	APIKey string

	// BaseURL is the WorkOS API base url
	// (if not set, defaults to DefaultBaseURL).
	BaseURL string
}

// -------------------------------------------------------------------
// Types (JSON tags mirror the official SDK / API response fields)
// -------------------------------------------------------------------

// User represents a WorkOS User Management user
// (SDK: pkg/common/user.go).
type User struct {
	Id                string `json:"id"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	ProfilePictureURL string `json:"profile_picture_url"`
	LastSignInAt      string `json:"last_sign_in_at"`
	ExternalId        string `json:"external_id"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// AuthResponse represents a successful /user_management/authenticate response
// (SDK: usermanagement.AuthenticateResponse).
//
// Note that for the "refresh_token" grant WorkOS returns only the token
// pair, aka. User/OrganizationId/AuthenticationMethod could be zero-values.
type AuthResponse struct {
	User                 User   `json:"user"`
	OrganizationId       string `json:"organization_id"`
	AccessToken          string `json:"access_token"`
	RefreshToken         string `json:"refresh_token"`
	AuthenticationMethod string `json:"authentication_method"`
}

// MagicAuth represents a WorkOS Magic Auth code object
// (SDK: usermanagement.MagicAuth).
type MagicAuth struct {
	Id        string `json:"id"`
	UserId    string `json:"user_id"`
	Email     string `json:"email"`
	ExpiresAt string `json:"expires_at"`
	Code      string `json:"code"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// PasswordReset represents a WorkOS password reset token object
// (SDK: usermanagement.PasswordReset).
type PasswordReset struct {
	Id                 string `json:"id"`
	UserId             string `json:"user_id"`
	Email              string `json:"email"`
	PasswordResetToken string `json:"password_reset_token"`
	PasswordResetUrl   string `json:"password_reset_url"`
	ExpiresAt          string `json:"expires_at"`
	CreatedAt          string `json:"created_at"`
}

// CreateUserOpts defines the POST /user_management/users body fields
// (SDK: usermanagement.CreateUserOpts).
type CreateUserOpts struct {
	Email         string `json:"email"`
	Password      string `json:"password,omitempty"`
	FirstName     string `json:"first_name,omitempty"`
	LastName      string `json:"last_name,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
}

// UpdateUserOpts defines the PUT /user_management/users/{id} body fields
// (SDK: usermanagement.UpdateUserOpts).
//
// Only the non-empty fields are sent.
type UpdateUserOpts struct {
	Email         string `json:"email,omitempty"`
	FirstName     string `json:"first_name,omitempty"`
	LastName      string `json:"last_name,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
	Password      string `json:"password,omitempty"`
}

// AuthenticationFactor represents a WorkOS MFA factor reference returned
// as part of the "mfa_challenge" error response
// (SDK: workos_errors.AuthenticationFactor).
type AuthenticationFactor struct {
	Id   string `json:"id"`
	Type string `json:"type"` // "totp" or "sms"
}

// Challenge represents a WorkOS MFA authentication factor challenge
// (SDK: pkg/mfa.Challenge).
type Challenge struct {
	Id        string `json:"id"`
	FactorId  string `json:"authentication_factor_id"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// -------------------------------------------------------------------
// Errors
// -------------------------------------------------------------------

// ErrUserNotFound is returned by GetUserByEmail when there is
// no user with the specified email.
var ErrUserNotFound = errors.New("workos: user not found")

// APIError represents a WorkOS API error response.
//
// WorkOS returns errors in 2 JSON shapes (SDK: workos_errors/http.go):
//
//	{"code": "...", "message": "...", ...}
//	{"error": "...", "error_description": "...", ...}
//
// The authentication endpoint additionally may return "step-up" errors, e.g.
// the MFA challenge one (code="mfa_challenge", HTTP 422):
//
//	{
//	  "code": "mfa_challenge",
//	  "message": "...",
//	  "user": {...},
//	  "authentication_factors": [{"id": "auth_factor_...", "type": "totp"}],
//	  "pending_authentication_token": "..."
//	}
type APIError struct {
	// Status is the HTTP response status code.
	Status int

	// Code is the WorkOS error code
	// (the "code" field or, when missing, the "error" field).
	Code string

	// Message is the human readable error message
	// (the "message" field or, when missing, the "error_description" field).
	Message string

	// RawBody is the raw response body.
	RawBody string

	// PendingAuthenticationToken is set on authentication "step-up"
	// errors (e.g. "mfa_challenge") and must be echoed back in the
	// follow-up authenticate request.
	PendingAuthenticationToken string

	// AuthenticationFactors is set on "mfa_challenge" errors and lists
	// the enrolled MFA factors the user can be challenged with.
	//
	// Note: WorkOS returns factor ids, not challenge ids - a challenge id
	// is obtained by challenging one of the factors and is then passed to
	// Client.AuthenticateWithTOTP.
	AuthenticationFactors []AuthenticationFactor

	// EmailVerificationId is set on "email_verification_required" errors
	// and identifies the pending email verification of the user.
	EmailVerificationId string
}

// Error implements the [error] interface.
func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.RawBody
	}

	if e.Code != "" {
		return fmt.Sprintf("workos: %d (%s) %s", e.Status, e.Code, msg)
	}

	return fmt.Sprintf("workos: %d %s", e.Status, msg)
}

// IsMFARequired reports whether the error indicates that the user must
// complete an MFA challenge to finish authenticating
// (SDK: workos_errors.MFAChallengeCode).
func (e *APIError) IsMFARequired() bool {
	return e.Code == "mfa_challenge"
}

// IsSSORequired reports whether the error indicates that the user must
// authenticate via SSO because their email domain matches an active
// SSO connection (password and other direct grants are rejected).
//
// The real API returns it in the OAuth error shape:
//
//	{"error": "sso_required", "error_description": "...", "connection_ids": [...]}
func (e *APIError) IsSSORequired() bool {
	return e.Code == "sso_required" || e.Code == "organization_selection_required"
}

// IsEmailVerificationRequired reports whether the error indicates that
// the user's email ownership must be verified before authenticating
// (returned by environments with the "Require email verification"
// dashboard setting enabled):
//
//	{
//	  "code": "email_verification_required",
//	  "message": "...",
//	  "pending_authentication_token": "...",
//	  "email_verification_id": "email_verification_..."
//	}
func (e *APIError) IsEmailVerificationRequired() bool {
	return e.Code == "email_verification_required"
}

func parseAPIError(status int, body []byte) *APIError {
	apiErr := &APIError{
		Status:  status,
		RawBody: string(body),
	}

	var payload struct {
		Code                       string                 `json:"code"`
		Message                    string                 `json:"message"`
		Error                      string                 `json:"error"`
		ErrorDescription           string                 `json:"error_description"`
		PendingAuthenticationToken string                 `json:"pending_authentication_token"`
		AuthenticationFactors      []AuthenticationFactor `json:"authentication_factors"`
		EmailVerificationId        string                 `json:"email_verification_id"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		apiErr.Message = string(body)
		return apiErr
	}

	apiErr.PendingAuthenticationToken = payload.PendingAuthenticationToken
	apiErr.AuthenticationFactors = payload.AuthenticationFactors
	apiErr.EmailVerificationId = payload.EmailVerificationId

	switch {
	case payload.Code != "":
		apiErr.Code = payload.Code
		apiErr.Message = payload.Message
	case payload.Error != "":
		apiErr.Code = payload.Error
		apiErr.Message = payload.ErrorDescription
	default:
		apiErr.Message = payload.Message
	}

	return apiErr
}

// -------------------------------------------------------------------
// Authentication methods
//
// All of the below use POST /user_management/authenticate with a JSON
// body containing "client_id", "client_secret" (the API key) and
// "grant_type" (no Authorization header!).
// -------------------------------------------------------------------

// AuthenticateWithPassword authenticates a user with email and password
// (grant_type "password").
func (c *Client) AuthenticateWithPassword(ctx context.Context, email string, password string, ipAddress string, userAgent string) (*AuthResponse, error) {
	body := c.authBody("password")
	body["email"] = email
	body["password"] = password
	if ipAddress != "" {
		body["ip_address"] = ipAddress
	}
	if userAgent != "" {
		body["user_agent"] = userAgent
	}

	return c.authenticate(ctx, body)
}

// AuthenticateWithCode authenticates a user with an OAuth/SSO/AuthKit
// authorization code (grant_type "authorization_code").
//
// codeVerifier is optional and is used only for PKCE flows.
func (c *Client) AuthenticateWithCode(ctx context.Context, code string, codeVerifier string) (*AuthResponse, error) {
	body := c.authBody("authorization_code")
	body["code"] = code
	if codeVerifier != "" {
		body["code_verifier"] = codeVerifier
	}

	return c.authenticate(ctx, body)
}

// AuthenticateWithMagicAuth authenticates a user with a one-time Magic Auth
// code (grant_type "urn:workos:oauth:grant-type:magic-auth:code").
func (c *Client) AuthenticateWithMagicAuth(ctx context.Context, code string, email string) (*AuthResponse, error) {
	body := c.authBody("urn:workos:oauth:grant-type:magic-auth:code")
	body["code"] = code
	body["email"] = email

	return c.authenticate(ctx, body)
}

// AuthenticateWithTOTP completes an MFA challenge with a TOTP code
// (grant_type "urn:workos:oauth:grant-type:mfa-totp").
//
// pendingAuthToken is the "pending_authentication_token" from the
// preceding "mfa_challenge" [APIError] and challengeId is the id of the
// challenged authentication factor challenge.
func (c *Client) AuthenticateWithTOTP(ctx context.Context, code string, pendingAuthToken string, challengeId string) (*AuthResponse, error) {
	body := c.authBody("urn:workos:oauth:grant-type:mfa-totp")
	body["code"] = code
	body["pending_authentication_token"] = pendingAuthToken
	body["authentication_challenge_id"] = challengeId

	return c.authenticate(ctx, body)
}

// ChallengeFactor initiates an authentication challenge for the specified
// MFA factor (POST /auth/factors/{id}/challenge; SDK: pkg/mfa).
//
// The returned challenge id is then passed to [Client.AuthenticateWithTOTP]
// together with the pending authentication token from the preceding
// "mfa_challenge" [APIError].
func (c *Client) ChallengeFactor(ctx context.Context, factorId string) (*Challenge, error) {
	result := &Challenge{}

	err := c.send(ctx, http.MethodPost, "/auth/factors/"+url.PathEscape(factorId)+"/challenge", nil, map[string]any{}, result, true)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// AuthenticateWithRefreshToken exchanges a refresh token for a new
// access+refresh token pair (grant_type "refresh_token").
func (c *Client) AuthenticateWithRefreshToken(ctx context.Context, refreshToken string) (*AuthResponse, error) {
	body := c.authBody("refresh_token")
	body["refresh_token"] = refreshToken

	return c.authenticate(ctx, body)
}

func (c *Client) authBody(grantType string) map[string]any {
	return map[string]any{
		"client_id":     c.ClientId,
		"client_secret": c.APIKey,
		"grant_type":    grantType,
	}
}

func (c *Client) authenticate(ctx context.Context, body map[string]any) (*AuthResponse, error) {
	result := &AuthResponse{}

	// note: no Authorization header - the credentials are part of the body
	err := c.send(ctx, http.MethodPost, "/user_management/authenticate", nil, body, result, false)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// -------------------------------------------------------------------
// Users
// -------------------------------------------------------------------

// CreateUser creates a new user (POST /user_management/users).
func (c *Client) CreateUser(ctx context.Context, opts CreateUserOpts) (*User, error) {
	result := &User{}

	err := c.send(ctx, http.MethodPost, "/user_management/users", nil, opts, result, true)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// UpdateUser updates an existing user (PUT /user_management/users/{id}).
func (c *Client) UpdateUser(ctx context.Context, userId string, opts UpdateUserOpts) (*User, error) {
	result := &User{}

	err := c.send(ctx, http.MethodPut, "/user_management/users/"+url.PathEscape(userId), nil, opts, result, true)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetUserByEmail returns the user matching the provided email
// (GET /user_management/users?email=...).
//
// Returns [ErrUserNotFound] if there is no user with the specified email.
func (c *Client) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := url.Values{}
	query.Set("email", email)
	query.Set("limit", "1")

	result := struct {
		Data []User `json:"data"`
	}{}

	err := c.send(ctx, http.MethodGet, "/user_management/users", query, nil, &result, true)
	if err != nil {
		return nil, err
	}

	if len(result.Data) == 0 {
		return nil, ErrUserNotFound
	}

	return &result.Data[0], nil
}

// -------------------------------------------------------------------
// Magic Auth / password reset / email verification
// -------------------------------------------------------------------

// CreateMagicAuth creates (and emails) a one-time Magic Auth code
// (POST /user_management/magic_auth).
func (c *Client) CreateMagicAuth(ctx context.Context, email string) (*MagicAuth, error) {
	body := map[string]any{"email": email}

	result := &MagicAuth{}

	err := c.send(ctx, http.MethodPost, "/user_management/magic_auth", nil, body, result, true)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// CreatePasswordReset creates a password reset token
// (POST /user_management/password_reset).
func (c *Client) CreatePasswordReset(ctx context.Context, email string) (*PasswordReset, error) {
	body := map[string]any{"email": email}

	result := &PasswordReset{}

	err := c.send(ctx, http.MethodPost, "/user_management/password_reset", nil, body, result, true)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ConfirmPasswordReset sets a new user password using a password reset token
// (POST /user_management/password_reset/confirm).
func (c *Client) ConfirmPasswordReset(ctx context.Context, token string, newPassword string) (*User, error) {
	body := map[string]any{
		"token":        token,
		"new_password": newPassword,
	}

	result := struct {
		User User `json:"user"`
	}{}

	err := c.send(ctx, http.MethodPost, "/user_management/password_reset/confirm", nil, body, &result, true)
	if err != nil {
		return nil, err
	}

	return &result.User, nil
}

// SendVerificationEmail creates an email verification challenge and emails
// the verification code to the user
// (POST /user_management/users/{id}/email_verification/send).
func (c *Client) SendVerificationEmail(ctx context.Context, userId string) error {
	return c.send(ctx, http.MethodPost, "/user_management/users/"+url.PathEscape(userId)+"/email_verification/send", nil, nil, nil, true)
}

// VerifyEmail verifies a user email address with the emailed verification code
// (POST /user_management/users/{id}/email_verification/confirm).
func (c *Client) VerifyEmail(ctx context.Context, userId string, code string) (*User, error) {
	body := map[string]any{"code": code}

	result := struct {
		User User `json:"user"`
	}{}

	err := c.send(ctx, http.MethodPost, "/user_management/users/"+url.PathEscape(userId)+"/email_verification/confirm", nil, body, &result, true)
	if err != nil {
		return nil, err
	}

	return &result.User, nil
}

// -------------------------------------------------------------------
// Admin Portal
// -------------------------------------------------------------------

// GeneratePortalLink generates an Admin Portal link for the specified
// organization and intent, e.g. "sso", "dsync", "audit_logs", "log_streams",
// "certificate_renewal", "domain_verification"
// (POST /portal/generate_link; SDK: pkg/portal).
func (c *Client) GeneratePortalLink(ctx context.Context, organizationId string, intent string) (string, error) {
	body := map[string]any{
		"intent":       intent,
		"organization": organizationId,
	}

	result := struct {
		Link string `json:"link"`
	}{}

	err := c.send(ctx, http.MethodPost, "/portal/generate_link", nil, body, &result, true)
	if err != nil {
		return "", err
	}

	return result.Link, nil
}

// -------------------------------------------------------------------
// Internal request helper
// -------------------------------------------------------------------

func (c *Client) send(ctx context.Context, method string, path string, query url.Values, body any, result any, withBearer bool) error {
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	fullURL := strings.TrimRight(baseURL, "/") + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if withBearer {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	rawResponse, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return parseAPIError(res.StatusCode, rawResponse)
	}

	if result != nil {
		return json.Unmarshal(rawResponse, result)
	}

	return nil
}

// -------------------------------------------------------------------
// Webhooks
// -------------------------------------------------------------------

// Webhook signature verification errors.
var (
	ErrMissingSignatureHeader = errors.New("workos: missing signature header")
	ErrInvalidSignatureHeader = errors.New("workos: invalid signature header")
	ErrExpiredSignature       = errors.New("workos: signature timestamp is outside of the allowed tolerance")
	ErrInvalidSignature       = errors.New("workos: invalid signature")
)

// VerifyWebhookSignature validates a WorkOS webhook request signature.
//
// header is the raw "Workos-Signature" request header value in the format
//
//	t=<unix timestamp in milliseconds>, v1=<hex hmac digest>
//
// and the signature is the hex encoded HMAC-SHA256 of "<t>.<body>" keyed
// with the webhook signing secret (SDK: pkg/webhooks - note that the
// timestamp is in MILLIseconds).
//
// tolerance is the max allowed age of the signature timestamp compared to
// now (the SDK default is 180s). Pass a zero tolerance to skip the
// timestamp expiration check.
func VerifyWebhookSignature(header string, body []byte, secret string, tolerance time.Duration, now time.Time) error {
	if header == "" {
		return ErrMissingSignatureHeader
	}

	var rawTimestamp, signature string

	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)

		if v, ok := strings.CutPrefix(part, "t="); ok {
			rawTimestamp = v
		} else if v, ok := strings.CutPrefix(part, "v1="); ok {
			signature = v
		}
	}

	if rawTimestamp == "" || signature == "" {
		return ErrInvalidSignatureHeader
	}

	msTimestamp, err := strconv.ParseInt(rawTimestamp, 10, 64)
	if err != nil {
		return ErrInvalidSignatureHeader
	}

	if tolerance > 0 && now.Sub(time.UnixMilli(msTimestamp)) > tolerance {
		return ErrExpiredSignature
	}

	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(rawTimestamp))
	mac.Write([]byte("."))
	mac.Write(body)

	// hmac.Equal is a constant time comparison
	if !hmac.Equal(signatureBytes, mac.Sum(nil)) {
		return ErrInvalidSignature
	}

	return nil
}
