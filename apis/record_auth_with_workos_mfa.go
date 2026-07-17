package apis

import (
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
)

// recordAuthWithWorkOSMFA completes a pending WorkOS MFA authentication
// (started with an auth-with-password request that returned 401 + mfaId).
func recordAuthWithWorkOSMFA(e *core.RequestEvent) error {
	collection, err := findAuthCollection(e)
	if err != nil {
		return err
	}

	if !workosDelegated(e.App, collection) {
		return e.NotFoundError("The collection is not configured for WorkOS authentication.", nil)
	}

	form := &authWithWorkOSMFAForm{}
	if err = e.BindBody(form); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
	}
	if err = form.validate(); err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	e.Set(core.RequestEventKeyInfoContext, core.RequestInfoContextPasswordAuth)

	client := workosClientFromApp(e.App)

	// WorkOS requires challenging the factor first to obtain a challenge id
	challenge, err := client.ChallengeFactor(e.Request.Context(), form.FactorId)
	if err != nil {
		return e.BadRequestError("Failed to authenticate.", err)
	}

	authResp, err := client.AuthenticateWithTOTP(e.Request.Context(), form.Code, form.MfaId, challenge.Id)
	if err != nil {
		return e.BadRequestError("Failed to authenticate.", err)
	}

	record, err := authWorkOSRecord(e, collection, &authResp.User, authResp.OrganizationId)
	if err != nil {
		return firstApiError(err, e.BadRequestError("Failed to authenticate.", err))
	}

	workosPersistRefreshToken(e.App, record, authResp.RefreshToken)

	return RecordAuthResponse(e, record, core.MFAMethodPassword, workosAuthMeta(e.App, authResp))
}

// -------------------------------------------------------------------

type authWithWorkOSMFAForm struct {
	// MfaId is the pending authentication token returned with the
	// preceding WorkOS MFA challenge response.
	MfaId string `form:"mfaId" json:"mfaId"`

	// FactorId is the id of the enrolled WorkOS MFA factor to challenge.
	FactorId string `form:"factorId" json:"factorId"`

	// Code is the user submitted TOTP code.
	Code string `form:"code" json:"code"`
}

func (form *authWithWorkOSMFAForm) validate() error {
	return validation.ValidateStruct(form,
		validation.Field(&form.MfaId, validation.Required, validation.Length(1, 255)),
		validation.Field(&form.FactorId, validation.Required, validation.Length(1, 255)),
		validation.Field(&form.Code, validation.Required, validation.Length(1, 71)),
	)
}
