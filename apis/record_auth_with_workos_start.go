package apis

import (
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/ozzo-validation/v4/is"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
	"github.com/pocketbase/pocketbase/tools/list"
	"github.com/pocketbase/pocketbase/tools/security"
	"golang.org/x/oauth2"
)

// workosOAuthProviders lists the supported WorkOS "provider" authorization
// url selector values (https://workos.com/docs/reference/user-management/authentication/get-authorization-url).
var workosOAuthProviders = []string{
	"GoogleOAuth",
	"MicrosoftOAuth",
	"AppleOAuth",
	"GitHubOAuth",
}

// recordAuthWithWorkOSStart generates a WorkOS AuthKit/SSO authorization
// url for the specified collection.
//
// The request must contain exactly one of:
//   - "provider" - a social login selector (e.g. "GoogleOAuth");
//   - "email"    - routes to the enterprise SSO connection of the organization
//     matching the email domain (responds with {"fallback": true}
//     when there is no matching organization so that clients can
//     fallback to a password/code UI).
//
// The authorization flow is completed with a regular
// auth-with-oauth2 request ("provider": "workos").
func recordAuthWithWorkOSStart(e *core.RequestEvent) error {
	collection, err := findAuthCollection(e)
	if err != nil {
		return err
	}

	if !workosDelegated(e.App, collection) {
		return e.NotFoundError("The collection is not configured for WorkOS authentication.", nil)
	}

	form := &authWithWorkOSStartForm{}
	if err = e.BindBody(form); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
	}
	if err = form.validate(); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	var selectorParam, selectorValue string

	if form.Provider != "" {
		selectorParam = "provider"
		selectorValue = form.Provider
	} else {
		domain := form.Email[strings.LastIndex(form.Email, "@")+1:]

		org, err := findWorkOSOrganizationByDomain(e.App, domain)
		if err != nil {
			return e.InternalServerError("Failed organization lookup.", err)
		}

		if org == nil {
			// no SSO connection to route to -> the client should
			// fallback to password/code based authentication
			return e.JSON(http.StatusOK, map[string]any{"fallback": true})
		}

		selectorParam = "organization_id"
		selectorValue = org.GetString("workosOrgId")
	}

	// load the "workos" provider (with the collection config taking precedence)
	providerConfig, ok := collection.OAuth2.GetProviderConfig(auth.NameWorkOS)
	if !ok {
		providerConfig = workosProviderConfig(e.App)
	}

	provider, err := providerConfig.InitProvider()
	if err != nil {
		return firstApiError(err, e.InternalServerError("Failed to init the WorkOS provider.", err))
	}

	state := security.RandomString(30)
	codeVerifier := security.RandomString(43)
	codeChallenge := security.S256Challenge(codeVerifier)

	urlOpts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam(selectorParam, selectorValue),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}

	authURL := provider.BuildAuthURL(
		state,
		urlOpts...,
	) + "&redirect_uri=" // empty redirect_uri so that users can append their redirect url

	return e.JSON(http.StatusOK, map[string]string{
		"authURL":             authURL,
		"state":               state,
		"codeVerifier":        codeVerifier,
		"codeChallengeMethod": "S256",
	})
}

// findWorkOSOrganizationByDomain returns the first "organizations"
// collection record whose "domains" json array contains the specified
// email domain (or nil if no organization matches).
func findWorkOSOrganizationByDomain(app core.App, domain string) (*core.Record, error) {
	if domain == "" {
		return nil, nil
	}

	if _, err := app.FindCachedCollectionByNameOrId(core.WorkOSOrganizationsCollectionName); err != nil {
		return nil, nil // missing organizations collection -> no SSO routing
	}

	// note: the LIKE prefilter is refined below with an exact match
	// on the parsed json array elements
	candidates, err := app.FindRecordsByFilter(
		core.WorkOSOrganizationsCollectionName,
		"domains ~ {:domain}",
		"-created",
		0,
		0,
		dbx.Params{"domain": domain},
	)
	if err != nil {
		return nil, err
	}

	for _, candidate := range candidates {
		var domains []string
		if err = candidate.UnmarshalJSONField("domains", &domains); err != nil {
			continue
		}

		if slices.ContainsFunc(domains, func(d string) bool {
			return strings.EqualFold(d, domain)
		}) {
			return candidate, nil
		}
	}

	return nil, nil
}

// -------------------------------------------------------------------

type authWithWorkOSStartForm struct {
	Email    string `form:"email" json:"email"`
	Provider string `form:"provider" json:"provider"`
}

func (form *authWithWorkOSStartForm) validate() error {
	return validation.ValidateStruct(form,
		validation.Field(
			&form.Email,
			validation.When(form.Provider == "", validation.Required.Error("either email or provider is required")),
			validation.When(form.Provider != "", validation.Empty.Error("email and provider cannot be used together")),
			validation.Length(1, 255),
			is.EmailFormat,
		),
		validation.Field(
			&form.Provider,
			validation.In(list.ToInterfaceSlice(workosOAuthProviders)...),
		),
	)
}
