package apis

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/ozzo-validation/v4/is"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/dbutils"
	"github.com/pocketbase/pocketbase/tools/list"
	"github.com/pocketbase/pocketbase/tools/workos"
)

func recordAuthWithPassword(e *core.RequestEvent) error {
	collection, err := findAuthCollection(e)
	if err != nil {
		return err
	}

	// password auth is implicitly available for WorkOS delegated collections
	isWorkOSDelegated := workosDelegated(e.App, collection)

	if !collection.PasswordAuth.Enabled && !isWorkOSDelegated {
		return e.ForbiddenError("The collection is not configured to allow password authentication.", nil)
	}

	form := &authWithPasswordForm{}
	if err = e.BindBody(form); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
	}
	if err = form.validate(collection); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	e.Set(core.RequestEventKeyInfoContext, core.RequestInfoContextPasswordAuth)

	// delegate the email+password authentication to WorkOS
	// (non-email identities fall through to the native flow, e.g. legacy username logins)
	if isWorkOSDelegated &&
		(form.IdentityField == "" || form.IdentityField == core.FieldNameEmail) &&
		is.EmailFormat.Validate(form.Identity) == nil {
		return workosAuthWithPassword(e, collection, form)
	}

	if !collection.PasswordAuth.Enabled {
		return e.ForbiddenError("The collection is not configured to allow password authentication.", nil)
	}

	var foundRecord *core.Record
	var foundErr error

	if form.IdentityField != "" {
		foundRecord, foundErr = findRecordByIdentityField(e.App, collection, form.IdentityField, form.Identity)
	} else {
		identityFields := collection.PasswordAuth.IdentityFields

		// @todo consider removing with the stable release or moving it in the collection save
		//
		// prioritize email lookup to minimize breaking changes with earlier versions
		if len(identityFields) > 1 && identityFields[0] != core.FieldNameEmail {
			identityFields = slices.Clone(identityFields)
			slices.SortStableFunc(identityFields, func(a, b string) int {
				if a == "email" {
					return -1
				}
				if b == "email" {
					return 1
				}
				return 0
			})
		}

		for _, name := range identityFields {
			if name == core.FieldNameEmail && is.EmailFormat.Validate(form.Identity) != nil {
				continue // no need to query the database if we know that the submitted value is not an email
			}

			foundRecord, foundErr = findRecordByIdentityField(e.App, collection, name, form.Identity)
			if foundErr == nil {
				break
			}
		}
	}

	// ignore not found errors to allow custom record find implementations
	if foundErr != nil && !errors.Is(foundErr, sql.ErrNoRows) {
		return e.InternalServerError("", foundErr)
	}

	event := new(core.RecordAuthWithPasswordRequestEvent)
	event.RequestEvent = e
	event.Collection = collection
	event.Record = foundRecord
	event.Identity = form.Identity
	event.Password = form.Password
	event.IdentityField = form.IdentityField

	return e.App.OnRecordAuthWithPasswordRequest().Trigger(event, func(e *core.RecordAuthWithPasswordRequestEvent) error {
		if e.Record == nil || !e.Record.ValidatePassword(e.Password) {
			// dummy password check to minimize enumeration side-channel attacks
			if e.Record == nil {
				dummyPasswordCheck(e.App, e.Collection)
			}

			return e.BadRequestError("Failed to authenticate.", errors.New("invalid login credentials"))
		}

		return RecordAuthResponse(e.RequestEvent, e.Record, core.MFAMethodPassword, nil)
	})
}

// workosAuthWithPassword performs an email+password authentication
// against WorkOS User Management on behalf of the specified collection.
func workosAuthWithPassword(e *core.RequestEvent, collection *core.Collection, form *authWithPasswordForm) error {
	authResp, err := workosClientFromApp(e.App).AuthenticateWithPassword(
		e.Request.Context(),
		form.Identity,
		form.Password,
		e.RealIP(),
		e.Request.UserAgent(),
	)
	if err != nil {
		var apiErr *workos.APIError
		if errors.As(err, &apiErr) && apiErr.IsMFARequired() {
			// mirror the native MFA response shape (see checkMFA in record_helpers.go)
			// with the enrolled WorkOS factors listed for the follow-up auth-with-mfa call
			factors := make([]map[string]string, 0, len(apiErr.AuthenticationFactors))
			for _, factor := range apiErr.AuthenticationFactors {
				factors = append(factors, map[string]string{
					"id":   factor.Id,
					"type": factor.Type,
				})
			}

			e.JSON(http.StatusUnauthorized, map[string]any{
				"mfaId":   apiErr.PendingAuthenticationToken,
				"factors": factors,
			})

			return ErrMFA
		}

		return e.BadRequestError("Failed to authenticate.", err)
	}

	record, err := authWorkOSRecord(e, collection, &authResp.User, authResp.OrganizationId)
	if err != nil {
		return firstApiError(err, e.BadRequestError("Failed to authenticate.", err))
	}

	return RecordAuthResponse(e, record, core.MFAMethodPassword, workosAuthMeta(authResp))
}

// -------------------------------------------------------------------

type authWithPasswordForm struct {
	Identity string `form:"identity" json:"identity"`
	Password string `form:"password" json:"password"`

	// IdentityField specifies the field to use to search for the identity
	// (leave it empty for "auto" detection).
	IdentityField string `form:"identityField" json:"identityField"`
}

func (form *authWithPasswordForm) validate(collection *core.Collection) error {
	return validation.ValidateStruct(form,
		validation.Field(&form.Identity, validation.Required, validation.Length(1, 255)),
		validation.Field(&form.Password, validation.Required, validation.Length(1, 255)),
		validation.Field(
			&form.IdentityField,
			validation.Length(1, 255),
			validation.In(list.ToInterfaceSlice(collection.PasswordAuth.IdentityFields)...),
		),
	)
}

// dummy password check to minimize side-channel attacks
// (performed with the collection configured field cost)
func dummyPasswordCheck(app core.App, collection *core.Collection) {
	record := &core.Record{}

	// find any random existing record
	err := app.RecordQuery(collection).Limit(1).One(record)
	if err != nil {
		return
	}

	// the value and result doesn't matter, we just need a constant-time check
	_ = record.ValidatePassword("")
}

func findRecordByIdentityField(app core.App, collection *core.Collection, field string, value any) (*core.Record, error) {
	if !slices.Contains(collection.PasswordAuth.IdentityFields, field) {
		return nil, errors.New("invalid identity field " + field)
	}

	index, ok := dbutils.FindSingleColumnUniqueIndex(collection.Indexes, field)
	if !ok {
		return nil, errors.New("missing " + field + " unique index constraint")
	}

	var expr dbx.Expression
	if strings.EqualFold(index.Columns[0].Collate, "nocase") {
		// case-insensitive search
		expr = dbx.NewExp("[["+field+"]] = {:identity} COLLATE NOCASE", dbx.Params{"identity": value})
	} else {
		expr = dbx.HashExp{field: value}
	}

	record := &core.Record{}

	err := app.RecordQuery(collection).AndWhere(expr).Limit(1).One(record)
	if err != nil {
		return nil, err
	}

	return record, nil
}
