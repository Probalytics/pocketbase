package core

import (
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"
)

const marketingAudiencePageSize = 500

// marketingMessageParams describes a single email to queue for delivery.
type marketingMessageParams struct {
	To           string
	Subject      string
	Body         string
	Record       *Record // source record for {RECORD:field} substitution (optional)
	CampaignId   string
	AutomationId string
	ScheduledAt  types.DateTime
}

// queueMarketingMessage renders the content against the source record and
// stores a ready-to-send message in the queue.
func (app *BaseApp) queueMarketingMessage(params marketingMessageParams) (*Record, error) {
	collection, err := app.FindCachedCollectionByNameOrId(CollectionNameMailMessages)
	if err != nil {
		return nil, err
	}

	message := NewRecord(collection)
	message.Set("to", params.To)
	message.Set("subject", resolveMailContent(app, params.Subject, params.Record))
	message.Set("body", resolveMailContent(app, params.Body, params.Record))
	message.Set("campaign", params.CampaignId)
	message.Set("automation", params.AutomationId)
	message.Set("status", MailMessageStatusQueued)
	message.Set("token", security.RandomString(32))
	message.Set("scheduledAt", params.ScheduledAt)

	if params.Record != nil {
		message.Set("collectionRef", params.Record.Collection().Name)
		message.Set("recordRef", params.Record.Id)
	}

	return message, app.Save(message)
}

// enqueueCampaign expands a campaign's audience into individual queued
// messages and flips the campaign into the "sending" state.
func (app *BaseApp) enqueueCampaign(campaign *Record) error {
	subject, body := app.campaignContent(campaign)

	audience := campaign.GetString("audienceCollection")
	filter := campaign.GetString("audienceFilter")

	recipients := 0
	for offset := 0; ; offset += marketingAudiencePageSize {
		batch, err := app.FindRecordsByFilter(audience, filter, "", marketingAudiencePageSize, offset)
		if err != nil {
			return err
		}

		for _, record := range batch {
			to := recordEmail(record)
			if to == "" || app.isSuppressed(to) {
				continue
			}

			_, err := app.queueMarketingMessage(marketingMessageParams{
				To:         to,
				Subject:    subject,
				Body:       body,
				Record:     record,
				CampaignId: campaign.Id,
			})
			if err != nil {
				return err
			}
			recipients++
		}

		if len(batch) < marketingAudiencePageSize {
			break
		}
	}

	campaign.Set("totalRecipients", recipients)
	campaign.Set("status", MailCampaignStatusSending)

	return app.Save(campaign)
}

// campaignContent returns the subject and body to send, falling back to the
// linked template for any field the campaign leaves empty.
func (app *BaseApp) campaignContent(campaign *Record) (subject string, body string) {
	subject = campaign.GetString("subject")
	body = campaign.GetString("body")

	templateId := campaign.GetString("template")
	if templateId == "" {
		return subject, body
	}

	template, err := app.FindRecordById(CollectionNameMailTemplates, templateId)
	if err != nil {
		return subject, body
	}

	if subject == "" {
		subject = template.GetString("subject")
	}
	if body == "" {
		body = template.GetString("body")
	}

	return subject, body
}
