package auth_test

import (
	"testing"

	"github.com/pocketbase/pocketbase/tools/auth"
	"golang.org/x/oauth2"
)

func TestNewWorkOSProvider(t *testing.T) {
	p := auth.NewWorkOSProvider()

	scenarios := []struct {
		name     string
		expected string
		value    string
	}{
		{"authURL", "https://api.workos.com/user_management/authorize", p.AuthURL()},
		{"tokenURL", "https://api.workos.com/user_management/authenticate", p.TokenURL()},
		{"userInfoURL", "", p.UserInfoURL()},
		{"displayName", "WorkOS", p.DisplayName()},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			if s.value != s.expected {
				t.Fatalf("Expected %s %q, got %q", s.name, s.expected, s.value)
			}
		})
	}

	if !p.PKCE() {
		t.Fatal("Expected PKCE to be enabled")
	}

	if len(p.Scopes()) != 0 {
		t.Fatalf("Expected empty scopes, got %v", p.Scopes())
	}
}

func TestWorkOSFetchAuthUser(t *testing.T) {
	p := auth.NewWorkOSProvider()

	baseUser := func(emailVerified any) map[string]any {
		return map[string]any{
			"id":                  "user_123",
			"email":               "test@example.com",
			"email_verified":      emailVerified,
			"first_name":          "John",
			"last_name":           "Doe",
			"profile_picture_url": "https://example.com/avatar.png",
		}
	}

	scenarios := []struct {
		name           string
		extra          map[string]any
		expectError    bool
		expectedId     string
		expectedName   string
		expectedEmail  string
		expectedAvatar string
		expectedOrgId  string
	}{
		{
			name:        "missing user data",
			extra:       map[string]any{},
			expectError: true,
		},
		{
			name:        "user data with invalid type",
			extra:       map[string]any{"user": "invalid"},
			expectError: true,
		},
		{
			name:           "verified email",
			extra:          map[string]any{"user": baseUser(true)},
			expectedId:     "user_123",
			expectedName:   "John Doe",
			expectedEmail:  "test@example.com",
			expectedAvatar: "https://example.com/avatar.png",
		},
		{
			name:           "unverified email",
			extra:          map[string]any{"user": baseUser(false)},
			expectedId:     "user_123",
			expectedName:   "John Doe",
			expectedEmail:  "",
			expectedAvatar: "https://example.com/avatar.png",
		},
		{
			name: "with organization_id",
			extra: map[string]any{
				"user":            baseUser(true),
				"organization_id": "org_456",
			},
			expectedId:     "user_123",
			expectedName:   "John Doe",
			expectedEmail:  "test@example.com",
			expectedAvatar: "https://example.com/avatar.png",
			expectedOrgId:  "org_456",
		},
		{
			name: "with empty organization_id",
			extra: map[string]any{
				"user":            baseUser(true),
				"organization_id": "",
			},
			expectedId:     "user_123",
			expectedName:   "John Doe",
			expectedEmail:  "test@example.com",
			expectedAvatar: "https://example.com/avatar.png",
			expectedOrgId:  "",
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			token := (&oauth2.Token{
				AccessToken:  "test_access_token",
				RefreshToken: "test_refresh_token",
			}).WithExtra(s.extra)

			user, err := p.FetchAuthUser(token)

			hasErr := err != nil
			if hasErr != s.expectError {
				t.Fatalf("Expected hasErr %v, got %v (%v)", s.expectError, hasErr, err)
			}

			if s.expectError {
				if user != nil {
					t.Fatalf("Expected nil user, got %v", user)
				}
				return
			}

			if user.Id != s.expectedId {
				t.Fatalf("Expected id %q, got %q", s.expectedId, user.Id)
			}

			if user.Name != s.expectedName {
				t.Fatalf("Expected name %q, got %q", s.expectedName, user.Name)
			}

			if user.Email != s.expectedEmail {
				t.Fatalf("Expected email %q, got %q", s.expectedEmail, user.Email)
			}

			if user.AvatarURL != s.expectedAvatar {
				t.Fatalf("Expected avatar %q, got %q", s.expectedAvatar, user.AvatarURL)
			}

			if user.AccessToken != "test_access_token" {
				t.Fatalf("Expected access token %q, got %q", "test_access_token", user.AccessToken)
			}

			if user.RefreshToken != "test_refresh_token" {
				t.Fatalf("Expected refresh token %q, got %q", "test_refresh_token", user.RefreshToken)
			}

			rawOrgId, _ := user.RawUser["organization_id"].(string)
			if rawOrgId != s.expectedOrgId {
				t.Fatalf("Expected RawUser organization_id %q, got %q", s.expectedOrgId, rawOrgId)
			}
		})
	}
}
