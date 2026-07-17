package apis

import (
	"html"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/types"
)

// trackingPixel is a 1x1 transparent GIF returned by the open-tracking endpoint.
var trackingPixel = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00,
	0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02,
	0x44, 0x01, 0x00, 0x3b,
}

// bindMarketingApi registers the marketing campaign actions and the public
// tracking, click and unsubscribe endpoints.
func bindMarketingApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/marketing")

	sub.GET("/o/{token}", trackOpen)
	sub.GET("/c/{token}", trackClick)
	sub.GET("/unsubscribe/{token}", unsubscribe)
	sub.POST("/unsubscribe/{token}", unsubscribe)

	sub.POST("/campaigns/{id}/send", campaignSend).Bind(RequireSuperuserAuth())
	sub.POST("/campaigns/{id}/schedule", campaignSchedule).Bind(RequireSuperuserAuth())
	sub.POST("/campaigns/{id}/cancel", campaignCancel).Bind(RequireSuperuserAuth())
	sub.POST("/campaigns/{id}/pause", campaignPause).Bind(RequireSuperuserAuth())
	sub.POST("/campaigns/{id}/resume", campaignResume).Bind(RequireSuperuserAuth())
	sub.POST("/campaigns/{id}/test", campaignTest).Bind(RequireSuperuserAuth())
	sub.GET("/campaigns/{id}/preview", campaignPreview).Bind(RequireSuperuserAuth())
	sub.GET("/audience", audienceCount).Bind(RequireSuperuserAuth())

	sub.POST("/contacts/{collection}/{id}/email", contactEmail).Bind(RequireSuperuserAuth())
}

func contactEmail(e *core.RequestEvent) error {
	record, err := e.App.FindRecordById(e.Request.PathValue("collection"), e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("Contact not found.", err)
	}

	to := record.GetString("email")
	if to == "" {
		to = record.Email()
	}
	if to == "" {
		return e.BadRequestError("The contact has no email address.", nil)
	}

	data := struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}{}
	if err := e.BindBody(&data); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	if err := e.App.SendDirectEmail(to, data.Subject, data.Body, record); err != nil {
		return e.BadRequestError("Failed to send the email.", err)
	}

	return e.NoContent(http.StatusOK)
}

func campaignSend(e *core.RequestEvent) error {
	campaign, err := loadCampaign(e)
	if err != nil {
		return err
	}
	if err := e.App.SendCampaign(campaign); err != nil {
		return e.BadRequestError("Failed to send the campaign.", err)
	}
	return e.JSON(http.StatusOK, campaign)
}

func campaignSchedule(e *core.RequestEvent) error {
	campaign, err := loadCampaign(e)
	if err != nil {
		return err
	}

	data := struct {
		ScheduledAt string `json:"scheduledAt"`
	}{}
	if err := e.BindBody(&data); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	at, err := types.ParseDateTime(data.ScheduledAt)
	if err != nil {
		return e.BadRequestError("Invalid scheduledAt value.", err)
	}

	if err := e.App.ScheduleCampaign(campaign, at); err != nil {
		return e.BadRequestError("Failed to schedule the campaign.", err)
	}
	return e.JSON(http.StatusOK, campaign)
}

func campaignCancel(e *core.RequestEvent) error {
	campaign, err := loadCampaign(e)
	if err != nil {
		return err
	}
	if err := e.App.CancelCampaign(campaign); err != nil {
		return e.BadRequestError("Failed to cancel the campaign.", err)
	}
	return e.JSON(http.StatusOK, campaign)
}

func campaignPause(e *core.RequestEvent) error {
	return setCampaignStatus(e, core.MailCampaignStatusPaused)
}

func campaignResume(e *core.RequestEvent) error {
	return setCampaignStatus(e, core.MailCampaignStatusSending)
}

func setCampaignStatus(e *core.RequestEvent, status string) error {
	campaign, err := loadCampaign(e)
	if err != nil {
		return err
	}
	campaign.Set("status", status)
	if err := e.App.Save(campaign); err != nil {
		return e.BadRequestError("Failed to update the campaign.", err)
	}
	return e.JSON(http.StatusOK, campaign)
}

func campaignTest(e *core.RequestEvent) error {
	campaign, err := loadCampaign(e)
	if err != nil {
		return err
	}

	data := struct {
		Email string `json:"email"`
	}{}
	if err := e.BindBody(&data); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if data.Email == "" {
		return e.BadRequestError("Missing recipient email.", nil)
	}

	if err := e.App.SendTestEmail(campaign, data.Email, firstAudienceRecord(e.App, campaign)); err != nil {
		return e.BadRequestError("Failed to send the test email.", err)
	}
	return e.NoContent(http.StatusOK)
}

func campaignPreview(e *core.RequestEvent) error {
	campaign, err := loadCampaign(e)
	if err != nil {
		return err
	}

	sample := firstAudienceRecord(e.App, campaign)
	if recordId := e.Request.URL.Query().Get("recordId"); recordId != "" {
		if found, err := e.App.FindRecordById(campaign.GetString("audienceCollection"), recordId); err == nil {
			sample = found
		}
	}

	subject, html := e.App.RenderCampaign(campaign, sample)

	return e.JSON(http.StatusOK, map[string]string{"subject": subject, "html": html})
}

func audienceCount(e *core.RequestEvent) error {
	collection := e.Request.URL.Query().Get("collection")
	if collection == "" {
		return e.BadRequestError("Missing collection.", nil)
	}

	total, err := e.App.CountRecordsByFilter(collection, e.Request.URL.Query().Get("filter"))
	if err != nil {
		return e.BadRequestError("Failed to count the audience.", err)
	}

	return e.JSON(http.StatusOK, map[string]int64{"total": total})
}

func trackOpen(e *core.RequestEvent) error {
	if message, err := findMessageByToken(e.App, e.Request.PathValue("token")); err == nil {
		recordEngagement(e.App, message, "openedAt", "openCount", "totalOpened")
	}
	return e.Blob(http.StatusOK, "image/gif", trackingPixel)
}

func trackClick(e *core.RequestEvent) error {
	target := e.Request.URL.Query().Get("u")
	if !isHTTPURL(target) {
		return e.NotFoundError("Invalid link.", nil)
	}

	if message, err := findMessageByToken(e.App, e.Request.PathValue("token")); err == nil {
		recordEngagement(e.App, message, "clickedAt", "clickCount", "totalClicked")
	}

	return e.Redirect(http.StatusFound, target)
}

func unsubscribe(e *core.RequestEvent) error {
	message, err := findMessageByToken(e.App, e.Request.PathValue("token"))
	if err != nil {
		return e.NotFoundError("Link not found.", err)
	}

	if err := suppressRecipient(e.App, message); err != nil {
		return e.InternalServerError("Failed to unsubscribe.", err)
	}

	if e.Request.Method == http.MethodPost {
		return e.NoContent(http.StatusOK)
	}

	return e.HTML(http.StatusOK, unsubscribeConfirmationHTML(e.App))
}

func loadCampaign(e *core.RequestEvent) (*core.Record, error) {
	campaign, err := e.App.FindRecordById(core.CollectionNameMailCampaigns, e.Request.PathValue("id"))
	if err != nil {
		return nil, e.NotFoundError("Campaign not found.", err)
	}
	return campaign, nil
}

func firstAudienceRecord(app core.App, campaign *core.Record) *core.Record {
	audience := campaign.GetString("audienceCollection")
	if audience == "" {
		return nil
	}

	records, err := app.FindRecordsByFilter(audience, campaign.GetString("audienceFilter"), "", 1, 0)
	if err != nil || len(records) == 0 {
		return nil
	}
	return records[0]
}

func findMessageByToken(app core.App, token string) (*core.Record, error) {
	return app.FindFirstRecordByFilter(core.CollectionNameMailMessages, "token={:token}", dbx.Params{"token": token})
}

// recordEngagement records an open or click on a message and bumps the parent
// campaign's aggregate counter the first time it happens.
func recordEngagement(app core.App, message *core.Record, tsField, countField, campaignField string) {
	first := message.GetDateTime(tsField).IsZero()

	message.Set(countField, message.GetInt(countField)+1)
	if first {
		message.Set(tsField, types.NowDateTime())
	}
	if err := app.Save(message); err != nil {
		return
	}

	if first {
		bumpCampaign(app, message.GetString("campaign"), campaignField)
	}
}

func suppressRecipient(app core.App, message *core.Record) error {
	email := message.GetString("to")

	if existing, _ := app.FindFirstRecordByFilter(
		core.CollectionNameMailSuppressions,
		"email={:email}",
		dbx.Params{"email": email},
	); existing != nil {
		return nil
	}

	collection, err := app.FindCachedCollectionByNameOrId(core.CollectionNameMailSuppressions)
	if err != nil {
		return err
	}

	suppression := core.NewRecord(collection)
	suppression.Set("email", email)
	suppression.Set("reason", core.MailSuppressionReasonUnsubscribe)
	suppression.Set("source", message.GetString("campaign"))
	if err := app.Save(suppression); err != nil {
		return err
	}

	bumpCampaign(app, message.GetString("campaign"), "totalUnsubscribed")
	return nil
}

func bumpCampaign(app core.App, campaignId, field string) {
	if campaignId == "" {
		return
	}
	campaign, err := app.FindRecordById(core.CollectionNameMailCampaigns, campaignId)
	if err != nil {
		return
	}
	campaign.Set(field, campaign.GetInt(field)+1)
	_ = app.Save(campaign)
}

func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

func unsubscribeConfirmationHTML(app core.App) string {
	return `<!doctype html><html><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>Unsubscribed</title></head>` +
		`<body style="font-family:system-ui,sans-serif;display:flex;min-height:100vh;margin:0;` +
		`align-items:center;justify-content:center;background:#f8fafc;color:#0f172a">` +
		`<div style="text-align:center;padding:32px"><h1 style="font-size:20px;margin:0 0 8px">You're unsubscribed</h1>` +
		`<p style="color:#64748b;margin:0">You will no longer receive marketing emails from ` +
		html.EscapeString(app.Settings().Meta.AppName) + `.</p></div></body></html>`
}
