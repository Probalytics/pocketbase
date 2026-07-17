package core

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/tools/auth"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/workos"
)

// WorkOSOrganizationsCollectionName is the name of the collection used
// to mirror the WorkOS organizations of the delegated auth records.
const WorkOSOrganizationsCollectionName = "organizations"

// newWorkOSClient initializes a WorkOS API client from the app WorkOS settings.
func newWorkOSClient(app App) *workos.Client {
	settings := app.Settings().WorkOS

	return &workos.Client{
		ClientId: settings.ClientId,
		APIKey:   settings.APIKey,
		BaseURL:  settings.APIURL,
	}
}

// isWorkOSDelegatedAuthCollection reports whether the auth flows of the
// provided collection are delegated to WorkOS User Management.
//
// The superusers collection always remains fully native.
func isWorkOSDelegatedAuthCollection(app App, collection *Collection) bool {
	return app.Settings().WorkOS.Enabled &&
		collection != nil &&
		collection.IsAuth() &&
		collection.Name != CollectionNameSuperusers
}

// WorkOSLinkOrganization upserts an "organizations" collection record for
// the specified WorkOS organization id and assigns it to the auth record's
// "organization" relation field (if such field exists).
//
// It is a no-op if the organizations collection or the relation field is missing.
func WorkOSLinkOrganization(app App, record *Record, workosOrgId string) error {
	if workosOrgId == "" || record == nil {
		return nil
	}

	orgField, ok := record.Collection().Fields.GetByName("organization").(*RelationField)
	if !ok || orgField == nil {
		return nil // the auth collection doesn't support organization linking
	}

	orgCollection, err := app.FindCachedCollectionByNameOrId(orgField.CollectionId)
	if err != nil {
		return nil // missing organizations collection -> nothing to link
	}

	org, err := app.FindFirstRecordByData(orgCollection, "workosOrgId", workosOrgId)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		// create a new organizations record
		// (the name is defaulted to the WorkOS org id until synced otherwise)
		org = NewRecord(orgCollection)
		org.Set("workosOrgId", workosOrgId)
		org.Set("name", workosOrgId)
		if err = app.Save(org); err != nil {
			return fmt.Errorf("failed to upsert organization %q: %w", workosOrgId, err)
		}
	}

	if record.GetString("organization") != org.Id {
		record.Set("organization", org.Id)
		if err = app.Save(record); err != nil {
			return fmt.Errorf("failed to link record %q to organization %q: %w", record.Id, workosOrgId, err)
		}
	}

	return nil
}

func (app *BaseApp) registerWorkOSHooks() {
	// Intercept the direct auth record signups (email+password) and
	// create the corresponding WorkOS user BEFORE persisting the local record.
	//
	// On success the local record is saved with a random password since
	// the actual credentials are managed by WorkOS.
	app.OnRecordCreateRequest().Bind(&hook.Handler[*RecordRequestEvent]{
		Id: "__pbWorkOSCreate__",
		Func: func(e *RecordRequestEvent) error {
			if !isWorkOSDelegatedAuthCollection(e.App, e.Collection) {
				return e.Next()
			}

			email := e.Record.Email()
			if email == "" {
				return e.Next()
			}

			// the raw plain password submitted with the request
			// (loaded on the record by the upsert form before this hook fires;
			// note that the OAuth2 and other shadow record flows autogenerate
			// a hash-only random password, i.e. their Plain value is empty)
			var plainPassword string
			if v, ok := e.Record.GetRaw(FieldNamePassword).(*PasswordFieldValue); ok && v != nil {
				plainPassword = v.Plain
			}
			if plainPassword == "" {
				return e.Next()
			}

			// extra guard: skip the internal OAuth2 record create requests
			// (their WorkOS user already exists)
			if info, err := e.RequestInfo(); err == nil && info.Context == RequestInfoContextOAuth2 {
				return e.Next()
			}

			// best-effort first/last name split
			var firstName, lastName string
			if parts := strings.Fields(e.Record.GetString("name")); len(parts) > 0 {
				firstName = parts[0]
				lastName = strings.Join(parts[1:], " ")
			}

			user, err := newWorkOSClient(e.App).CreateUser(e.Request.Context(), workos.CreateUserOpts{
				Email:         email,
				Password:      plainPassword,
				FirstName:     firstName,
				LastName:      lastName,
				EmailVerified: false,
			})
			if err != nil {
				var apiErr *workos.APIError
				if errors.As(err, &apiErr) && (apiErr.Status == 409 || apiErr.Code == "email_not_available") {
					return e.BadRequestError("Failed to create record.", validation.Errors{
						FieldNameEmail: validation.NewError(
							"validation_workos_email_exists",
							"The email address is already in use.",
						),
					})
				}

				return e.BadRequestError("Failed to create record.", err)
			}

			e.Record.SetIfFieldExists("workosUserId", user.Id)

			// the credentials live in WorkOS
			// (this also disables the plain password validators of the upsert form)
			e.Record.SetRandomPassword()

			if err = e.Next(); err != nil {
				return err
			}

			// link the persisted record to its WorkOS user so that the
			// follow-up logins converge on it even before the email is verified
			ea := NewExternalAuth(e.App)
			ea.SetCollectionRef(e.Record.Collection().Id)
			ea.SetRecordRef(e.Record.Id)
			ea.SetProvider(auth.NameWorkOS)
			ea.SetProviderId(user.Id)
			if err = e.App.Save(ea); err != nil {
				// non-critical - the workosUserId field fallback still applies
				e.App.Logger().Error(
					"Failed to save the WorkOS external auth link after signup",
					"error", err,
					"recordId", e.Record.Id,
				)
			}

			return nil
		},
	})

	// Link the WorkOS organization (if any) on successful "workos"
	// provider OAuth2/SSO authentications.
	//
	// The linking is performed in OnRecordAuthRequest (fired by
	// RecordAuthResponse before the response body is written) instead of
	// OnRecordAuthWithOAuth2Request so that the returned record already
	// reflects the updated organization relation.
	app.OnRecordAuthRequest().Bind(&hook.Handler[*RecordAuthRequestEvent]{
		Id: "__pbWorkOSOrgLink__",
		Func: func(e *RecordAuthRequestEvent) error {
			if e.Record == nil || !isWorkOSDelegatedAuthCollection(e.App, e.Record.Collection()) {
				return e.Next()
			}

			// the OAuth2 auth meta is the marshalized auth.AuthUser and
			// carries the WorkOS organization id in its rawUser payload
			var orgId string
			if meta, ok := e.Meta.(map[string]any); ok {
				if rawUser, ok := meta["rawUser"].(map[string]any); ok {
					orgId, _ = rawUser["organization_id"].(string)
				}
			}

			if orgId != "" {
				if err := WorkOSLinkOrganization(e.App, e.Record, orgId); err != nil {
					// non-critical error - the user is still authenticated
					e.App.Logger().Error(
						"Failed to link WorkOS organization on auth",
						"error", err,
						"organizationId", orgId,
						"recordId", e.Record.Id,
					)
				}
			}

			return e.Next()
		},
	})
}
