package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/spf13/cast"
	"golang.org/x/oauth2"
)

func init() {
	Providers[NameWorkOS] = wrapFactory(NewWorkOSProvider)
}

var _ Provider = (*WorkOS)(nil)

// NameWorkOS is the unique name of the WorkOS provider.
const NameWorkOS string = "workos"

// WorkOS allows authentication via the WorkOS User Management OAuth2 API
// (social login providers and enterprise SSO connections).
//
// Note that WorkOS doesn't expose a separate userinfo endpoint -
// the user data is returned inline with the token exchange response.
type WorkOS struct {
	BaseProvider
}

// NewWorkOSProvider creates a new WorkOS provider instance with some defaults.
func NewWorkOSProvider() *WorkOS {
	return &WorkOS{BaseProvider{
		ctx:         context.Background(),
		order:       30,
		logo:        `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32"><rect width="32" height="32" rx="6" fill="#ccc"/><path fill="#fff" d="M6.5 9h3l2.6 9.4L14.6 9h2.8l2.5 9.4L22.5 9h3l-4 14h-3l-2.5-9.1L13.5 23h-3z"/></svg>`,
		displayName: "WorkOS",
		pkce:        true,
		scopes:      []string{}, // WorkOS ignores the OAuth2 scopes for this endpoint
		authURL:     "https://api.workos.com/user_management/authorize",
		tokenURL:    "https://api.workos.com/user_management/authenticate",
	}}
}

// FetchAuthUser returns an AuthUser instance based on the WorkOS's token response user data.
//
// API reference: https://workos.com/docs/reference/user-management
func (p *WorkOS) FetchAuthUser(token *oauth2.Token) (*AuthUser, error) {
	data, err := p.FetchRawUserInfo(token)
	if err != nil {
		return nil, err
	}

	rawUser := map[string]any{}
	if err := json.Unmarshal(data, &rawUser); err != nil {
		return nil, err
	}

	extracted := struct {
		Id            string `json:"id"`
		FirstName     string `json:"first_name"`
		LastName      string `json:"last_name"`
		Picture       string `json:"profile_picture_url"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"` // see #6657
	}{}
	if err := json.Unmarshal(data, &extracted); err != nil {
		return nil, err
	}

	user := &AuthUser{
		Id:           extracted.Id,
		Name:         strings.TrimSpace(extracted.FirstName + " " + extracted.LastName),
		AvatarURL:    extracted.Picture,
		RawUser:      rawUser,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}

	user.Expiry, _ = types.ParseDateTime(token.Expiry)

	if cast.ToBool(extracted.EmailVerified) {
		user.Email = extracted.Email
	}

	// the top-level organization_id (present for enterprise SSO sign-ins)
	// is copied into the raw user data to allow tenant linking
	if organizationId, ok := token.Extra("organization_id").(string); ok && organizationId != "" {
		user.RawUser["organization_id"] = organizationId
	}

	return user, nil
}

// FetchRawUserInfo implements Provider.FetchRawUserInfo interface method.
//
// WorkOS doesn't have a userinfo endpoint and returns the user data
// inline with the token exchange response, so this method simply
// returns the marshaled "user" object from the token response.
func (p *WorkOS) FetchRawUserInfo(token *oauth2.Token) ([]byte, error) {
	user, ok := token.Extra("user").(map[string]any)
	if !ok {
		return nil, errors.New("missing user data in WorkOS token response")
	}

	return json.Marshal(user)
}
