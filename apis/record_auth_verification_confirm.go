package apis

import (
	"net/http"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/ozzo-validation/v4/is"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/spf13/cast"
)

func recordConfirmVerification(e *core.RequestEvent) error {
	collection, err := findAuthCollection(e)
	if err != nil {
		return err
	}

	if collection.Name == core.CollectionNameSuperusers {
		return e.BadRequestError("All superusers are verified by default.", nil)
	}

	// delegate to WorkOS when the request carries the optional
	// {email, code} WorkOS verification fields
	// (token-only requests fall through to the native flow since
	// previously issued PB verification tokens may still exist)
	if workosDelegated(e.App, collection) {
		wForm := new(workosConfirmVerificationForm)
		if err = e.BindBody(wForm); err != nil {
			return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
		}

		if wForm.Email != "" || wForm.Code != "" {
			return workosConfirmVerification(e, collection, wForm)
		}
	}

	form := new(recordConfirmVerificationForm)
	form.app = e.App
	form.collection = collection
	if err = e.BindBody(form); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
	}
	if err = form.validate(); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	record, err := form.app.FindAuthRecordByToken(form.Token, core.TokenTypeVerification)
	if err != nil {
		return e.BadRequestError("Invalid or expired verification token.", err)
	}

	wasVerified := record.Verified()

	event := new(core.RecordConfirmVerificationRequestEvent)
	event.RequestEvent = e
	event.Collection = collection
	event.Record = record

	return e.App.OnRecordConfirmVerificationRequest().Trigger(event, func(e *core.RecordConfirmVerificationRequestEvent) error {
		if !wasVerified {
			e.Record.SetVerified(true)

			// similar to the OTP auth, we enforce an extra password reset
			// guard as this way is less prone to pre-hijacking attacks
			// in case the password auth is eventually enabled later
			if !e.Record.Collection().PasswordAuth.Enabled {
				e.Record.SetRandomPassword()
			}

			if err := e.App.Save(e.Record); err != nil {
				return firstApiError(err, e.BadRequestError("An error occurred while saving the verified state.", err))
			}
		}

		e.App.Store().Remove(getVerificationResendKey(e.Record))

		return execAfterSuccessTx(true, e.App, func() error {
			return e.NoContent(http.StatusNoContent)
		})
	})
}

// workosConfirmVerification verifies a user email address with a WorkOS
// emailed verification code and syncs the local record verified state.
func workosConfirmVerification(e *core.RequestEvent, collection *core.Collection, form *workosConfirmVerificationForm) error {
	if err := form.validate(); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	client := workosClientFromApp(e.App)

	wUser, err := client.GetUserByEmail(e.Request.Context(), form.Email)
	if err != nil {
		return e.BadRequestError("Invalid or expired verification code.", err)
	}

	wUser, err = client.VerifyEmail(e.Request.Context(), wUser.Id, form.Code)
	if err != nil {
		return e.BadRequestError("Invalid or expired verification code.", err)
	}

	// sync the local record verified state (if such record exists)
	record, err := e.App.FindAuthRecordByEmail(collection, form.Email)
	if err == nil && !record.Verified() {
		record.SetVerified(true)
		record.SetIfFieldExists("workosUserId", wUser.Id)
		if err = e.App.Save(record); err != nil {
			return firstApiError(err, e.BadRequestError("An error occurred while saving the verified state.", err))
		}

		e.App.Store().Remove(getVerificationResendKey(record))
	}

	return e.NoContent(http.StatusNoContent)
}

type workosConfirmVerificationForm struct {
	Token string `form:"token" json:"token"`
	Email string `form:"email" json:"email"`
	Code  string `form:"code" json:"code"`
}

func (form *workosConfirmVerificationForm) validate() error {
	return validation.ValidateStruct(form,
		validation.Field(&form.Email, validation.Required, validation.Length(1, 255), is.EmailFormat),
		validation.Field(&form.Code, validation.Required, validation.Length(1, 71)),
	)
}

// -------------------------------------------------------------------

type recordConfirmVerificationForm struct {
	app        core.App
	collection *core.Collection

	Token string `form:"token" json:"token"`
}

func (form *recordConfirmVerificationForm) validate() error {
	return validation.ValidateStruct(form,
		validation.Field(&form.Token, validation.Required, validation.By(form.checkToken)),
	)
}

func (form *recordConfirmVerificationForm) checkToken(value any) error {
	v, _ := value.(string)
	if v == "" {
		return nil // nothing to check
	}

	claims, _ := security.ParseUnverifiedJWT(v)
	email := cast.ToString(claims["email"])
	if email == "" {
		return validation.NewError("validation_invalid_token_claims", "Missing email token claim.")
	}

	record, err := form.app.FindAuthRecordByToken(v, core.TokenTypeVerification)
	if err != nil || record == nil {
		return validation.NewError("validation_invalid_token", "Invalid or expired token.")
	}

	if record.Collection().Id != form.collection.Id {
		return validation.NewError("validation_token_collection_mismatch", "The provided token is for different auth collection.")
	}

	if record.Email() != email {
		return validation.NewError("validation_token_email_mismatch", "The record email doesn't match with the requested token claims.")
	}

	return nil
}
