package apis

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
	"github.com/pocketbase/pocketbase/tools/list"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/workos"
)

// bindWorkOSApi registers the WorkOS delegation api endpoints.
func bindWorkOSApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/collections/{collection}")

	sub.POST("/auth-with-workos", recordAuthWithWorkOSStart).Bind(
		collectionPathRateLimit("", "authWithWorkOS", "auth"),
	)

	sub.POST("/auth-with-mfa", recordAuthWithWorkOSMFA).Bind(
		collectionPathRateLimit("", "authWithMFA", "auth"),
	)

	workosGroup := rg.Group("/workos")

	workosGroup.POST("/webhooks", workosWebhooks)

	workosGroup.POST("/portal-link", workosPortalLink).Bind(RequireSuperuserAuth())
}

// workosDelegated reports whether the auth flows of the specified
// collection are delegated to WorkOS User Management.
//
// The superusers collection always remains fully native.
func workosDelegated(app core.App, collection *core.Collection) bool {
	return app.Settings().WorkOS.Enabled &&
		collection != nil &&
		collection.IsAuth() &&
		collection.Name != core.CollectionNameSuperusers
}

// workosClientFromApp initializes a WorkOS API client from the app WorkOS settings.
func workosClientFromApp(app core.App) *workos.Client {
	settings := app.Settings().WorkOS

	return &workos.Client{
		ClientId: settings.ClientId,
		APIKey:   settings.APIKey,
		BaseURL:  settings.APIURL,
	}
}

// workosProviderConfig returns an OAuth2 provider config for the "workos"
// provider synthesized from the app WorkOS settings.
//
// It is used as fallback when a WorkOS delegated collection doesn't have
// an explicit "workos" entry in its OAuth2 providers list.
func workosProviderConfig(app core.App) core.OAuth2ProviderConfig {
	settings := app.Settings().WorkOS

	baseURL := strings.TrimRight(settings.APIURL, "/")
	if baseURL == "" {
		baseURL = workos.DefaultBaseURL
	}

	return core.OAuth2ProviderConfig{
		Name:         auth.NameWorkOS,
		ClientId:     settings.ClientId,
		ClientSecret: settings.APIKey,
		AuthURL:      baseURL + "/user_management/authorize",
		TokenURL:     baseURL + "/user_management/authenticate",
	}
}

// workosAuthMeta builds a small meta payload for the RecordAuthResponse
// of the WorkOS delegated auth flows.
func workosAuthMeta(authResp *workos.AuthResponse) map[string]any {
	return map[string]any{
		"workos": map[string]any{
			"organizationId":       authResp.OrganizationId,
			"authenticationMethod": authResp.AuthenticationMethod,
		},
	}
}

// authWorkOSRecord converges a WorkOS User Management user to a single
// local auth record of the specified collection:
//
//  1. Existing "workos" _externalAuths link -> its record.
//  2. Fallback to a lookup by the (verified) WorkOS email.
//  3. Otherwise creates a new local "shadow" record (the credentials remain in WorkOS).
//
// It also ensures that the _externalAuths link exists, that the local
// verified/workosUserId state is in sync and that the WorkOS organization
// (if any) is linked to the record.
func authWorkOSRecord(e *core.RequestEvent, collection *core.Collection, wUser *workos.User, orgId string) (*core.Record, error) {
	if wUser == nil || wUser.Id == "" {
		return nil, errors.New("missing WorkOS user data")
	}

	var record *core.Record
	var matchedByEmail bool

	// check for an existing relation with the auth collection
	externalAuthRel, err := e.App.FindFirstExternalAuthByExpr(dbx.HashExp{
		"collectionRef": collection.Id,
		"provider":      auth.NameWorkOS,
		"providerId":    wUser.Id,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed WorkOS relation check: %w", err)
	}

	if externalAuthRel != nil {
		record, err = e.App.FindRecordById(collection, externalAuthRel.RecordRef())
		if err != nil {
			return nil, err
		}
	}

	// fallback match by the trusted hidden workosUserId field
	// (e.g. records created via the signup interception before their first
	// login; the field is hidden and cannot be set by regular clients)
	if record == nil && collection.Fields.GetByName("workosUserId") != nil {
		record, err = e.App.FindFirstRecordByData(collection, "workosUserId", wUser.Id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("failed WorkOS user id record check: %w", err)
		}
	}

	if record == nil && wUser.Email != "" && wUser.EmailVerified {
		// look for an existing auth record with the verified WorkOS email
		record, err = e.App.FindAuthRecordByEmail(collection, wUser.Email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("failed WorkOS auth record check: %w", err)
		}
		matchedByEmail = record != nil
	}

	txErr := e.App.RunInTransaction(func(txApp core.App) error {
		if record == nil {
			// create a local "shadow" record
			record = core.NewRecord(collection)
			record.SetEmail(wUser.Email)
			record.SetVerified(wUser.EmailVerified)
			record.SetIfFieldExists("name", strings.TrimSpace(wUser.FirstName+" "+wUser.LastName))
			record.SetIfFieldExists("workosUserId", wUser.Id)
			record.SetRandomPassword() // the credentials live in WorkOS

			if err := txApp.Save(record); err != nil {
				return fmt.Errorf("failed to create WorkOS shadow record: %w", err)
			}
		} else {
			var needUpdate bool

			// prevent pre-hijacking in case the record was precreated by
			// a malicious actor (similar to the native OAuth2 flow)
			if matchedByEmail && !record.Verified() {
				needUpdate = true
				record.SetRandomPassword()

				if err := txApp.DeleteAllExternalAuthsByRecord(record); err != nil {
					return err
				}
			}

			// sync the verified state (only upgrades)
			if wUser.EmailVerified && !record.Verified() {
				needUpdate = true
				record.SetVerified(true)
			}

			// sync the workosUserId drift
			if collection.Fields.GetByName("workosUserId") != nil && record.GetString("workosUserId") != wUser.Id {
				needUpdate = true
				record.Set("workosUserId", wUser.Id)
			}

			if needUpdate {
				if err := txApp.Save(record); err != nil {
					return err
				}
			}
		}

		// create the ExternalAuth relation if missing
		rel, err := txApp.FindFirstExternalAuthByExpr(dbx.HashExp{
			"collectionRef": collection.Id,
			"provider":      auth.NameWorkOS,
			"providerId":    wUser.Id,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if rel == nil {
			rel = core.NewExternalAuth(txApp)
			rel.SetCollectionRef(collection.Id)
			rel.SetRecordRef(record.Id)
			rel.SetProvider(auth.NameWorkOS)
			rel.SetProviderId(wUser.Id)

			if err := txApp.Save(rel); err != nil {
				return fmt.Errorf("failed to save WorkOS linked rel: %w", err)
			}
		}

		// organization linking
		if orgId != "" {
			if err := core.WorkOSLinkOrganization(txApp, record, orgId); err != nil {
				return err
			}
		}

		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	return record, nil
}

// -------------------------------------------------------------------
// Webhooks
// -------------------------------------------------------------------

func workosWebhooks(e *core.RequestEvent) error {
	settings := e.App.Settings().WorkOS
	if !settings.Enabled || settings.WebhookSecret == "" {
		return e.NotFoundError("", nil)
	}

	raw, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.BadRequestError("Failed to read request body.", err)
	}

	err = workos.VerifyWebhookSignature(
		e.Request.Header.Get("WorkOS-Signature"),
		raw,
		settings.WebhookSecret,
		3*time.Minute,
		time.Now(),
	)
	if err != nil {
		return e.UnauthorizedError("Invalid webhook signature.", err)
	}

	payload := struct {
		Id    string          `json:"id"`
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}{}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return e.BadRequestError("Failed to parse webhook payload.", err)
	}

	switch payload.Event {
	case "dsync.user.created", "dsync.user.updated", "dsync.user.deleted":
		if err = workosHandleDsyncUserEvent(e.App, payload.Event, payload.Data); err != nil {
			return firstApiError(err, e.InternalServerError("Failed to process webhook event.", err))
		}
	default:
		e.App.Logger().Debug("Unhandled WorkOS webhook event", "event", payload.Event, "webhookId", payload.Id)
	}

	return e.JSON(http.StatusOK, map[string]bool{"received": true})
}

// workosDirectoryUser is a partial representation of a WorkOS Directory
// Sync user webhook payload (SDK: pkg/directorysync.User).
type workosDirectoryUser struct {
	Id        string `json:"id"`
	IdpId     string `json:"idp_id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	State     string `json:"state"` // "active" or "inactive"

	// OrganizationId is the id of the organization in which the directory resides.
	OrganizationId string `json:"organization_id"`

	Emails []struct {
		Primary bool   `json:"primary"`
		Value   string `json:"value"`
	} `json:"emails"`
}

func (du workosDirectoryUser) primaryEmail() string {
	if du.Email != "" {
		return du.Email
	}

	for _, e := range du.Emails {
		if e.Primary && e.Value != "" {
			return e.Value
		}
	}

	for _, e := range du.Emails {
		if e.Value != "" {
			return e.Value
		}
	}

	return ""
}

// workosHandleDsyncUserEvent upserts a local shadow auth record for the
// specified "dsync.user.*" webhook event.
//
// Deprovisioned or deleted directory users are never hard deleted -
// they are only marked as suspended (blocking them from authenticating
// via the collection "suspended = false" auth rule).
func workosHandleDsyncUserEvent(app core.App, eventName string, data []byte) error {
	collection := firstWorkOSDelegatedAuthCollection(app)
	if collection == nil {
		app.Logger().Debug("Skipped WorkOS dsync event - no delegated auth collection", "event", eventName)
		return nil
	}

	du := workosDirectoryUser{}
	if err := json.Unmarshal(data, &du); err != nil {
		return err
	}
	if du.Id == "" {
		return errors.New("missing dsync user id")
	}

	email := du.primaryEmail()
	isDeleted := eventName == "dsync.user.deleted"

	return app.RunInTransaction(func(txApp core.App) error {
		var record *core.Record
		var err error

		// match by the WorkOS directory user id
		if collection.Fields.GetByName("workosUserId") != nil {
			record, err = txApp.FindFirstRecordByData(collection, "workosUserId", du.Id)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		// fallback to the primary email
		if record == nil && email != "" {
			record, err = txApp.FindAuthRecordByEmail(collection, email)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		if record == nil {
			if isDeleted {
				return nil // nothing to suspend
			}

			// create a new shadow record
			record = core.NewRecord(collection)
			record.SetEmail(email)
			record.SetVerified(email != "") // the email comes from the directory provider
			record.SetRandomPassword()      // the credentials live in WorkOS
		}

		if name := strings.TrimSpace(du.FirstName + " " + du.LastName); name != "" {
			record.SetIfFieldExists("name", name)
		}

		// don't overwrite an eventual existing User Management id link
		if record.GetString("workosUserId") == "" {
			record.SetIfFieldExists("workosUserId", du.Id)
		}

		record.SetIfFieldExists("suspended", isDeleted || (du.State != "" && du.State != "active"))

		if err = txApp.Save(record); err != nil {
			return fmt.Errorf("failed to upsert dsync shadow record: %w", err)
		}

		if du.OrganizationId != "" {
			if err = core.WorkOSLinkOrganization(txApp, record, du.OrganizationId); err != nil {
				return err
			}
		}

		return nil
	})
}

// firstWorkOSDelegatedAuthCollection returns the first WorkOS delegated
// auth collection (preferring the default "users" one).
func firstWorkOSDelegatedAuthCollection(app core.App) *core.Collection {
	if collection, err := app.FindCachedCollectionByNameOrId("users"); err == nil && workosDelegated(app, collection) {
		return collection
	}

	collections, err := app.FindAllCollections(core.CollectionTypeAuth)
	if err != nil {
		return nil
	}

	for _, collection := range collections {
		if workosDelegated(app, collection) {
			return collection
		}
	}

	return nil
}

// -------------------------------------------------------------------
// Admin Portal
// -------------------------------------------------------------------

var workosPortalIntents = []string{"sso", "dsync"}

func workosPortalLink(e *core.RequestEvent) error {
	if !e.App.Settings().WorkOS.Enabled {
		return e.NotFoundError("", nil)
	}

	form := struct {
		// Organization is either an "organizations" collection record id
		// or directly a WorkOS organization id.
		Organization string `form:"organization" json:"organization"`
		Intent       string `form:"intent" json:"intent"`
	}{}
	if err := e.BindBody(&form); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
	}

	err := validation.Errors{
		"organization": validation.Validate(form.Organization, validation.Required, validation.Length(1, 255)),
		"intent":       validation.Validate(form.Intent, validation.Required, validation.In(list.ToInterfaceSlice(workosPortalIntents)...)),
	}.Filter()
	if err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	workosOrgId := form.Organization

	// resolve the WorkOS organization id from an organizations record (if such exists)
	if org, err := e.App.FindRecordById(core.WorkOSOrganizationsCollectionName, form.Organization); err == nil {
		if v := org.GetString("workosOrgId"); v != "" {
			workosOrgId = v
		}
	}

	link, err := workosClientFromApp(e.App).GeneratePortalLink(e.Request.Context(), workosOrgId, form.Intent)
	if err != nil {
		return firstApiError(err, e.BadRequestError("Failed to generate portal link.", err))
	}

	return e.JSON(http.StatusOK, map[string]string{"link": link})
}
