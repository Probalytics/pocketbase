package core

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/tools/types"
)

// SendCampaign expands the campaign audience into the queue and starts
// delivering on the next sender tick.
func (app *BaseApp) SendCampaign(campaign *Record) error {
	return app.enqueueCampaign(campaign)
}

// ScheduleCampaign marks a campaign to be sent at a later time. The sender
// picks it up automatically once the scheduled time arrives.
func (app *BaseApp) ScheduleCampaign(campaign *Record, at types.DateTime) error {
	campaign.Set("scheduledAt", at)
	campaign.Set("status", MailCampaignStatusScheduled)
	return app.Save(campaign)
}

// CancelCampaign stops a campaign and drops any of its messages still waiting
// in the queue.
func (app *BaseApp) CancelCampaign(campaign *Record) error {
	for {
		batch, err := app.FindRecordsByFilter(
			CollectionNameMailMessages,
			"campaign={:id} && status={:status}",
			"", marketingAudiencePageSize, 0,
			dbx.Params{"id": campaign.Id, "status": MailMessageStatusQueued},
		)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}

		for _, message := range batch {
			if err := app.finishMarketingMessage(message, MailMessageStatusCanceled, "campaign canceled"); err != nil {
				return err
			}
		}
	}

	campaign.Set("status", MailCampaignStatusCanceled)
	return app.Save(campaign)
}

// SendTestEmail delivers a one-off copy of the campaign to the given address,
// rendered against an optional sample record.
func (app *BaseApp) SendTestEmail(campaign *Record, to string, sample *Record) error {
	subject, body := app.campaignContent(campaign)

	message, err := app.queueMarketingMessage(marketingMessageParams{
		To:      to,
		Subject: subject,
		Body:    body,
		Record:  sample,
	})
	if err != nil {
		return err
	}

	return app.deliverMarketingMessage(message)
}

// RenderCampaign resolves a campaign's subject and body for previewing,
// substituting variables against an optional sample record.
func (app *BaseApp) RenderCampaign(campaign *Record, sample *Record) (subject string, html string) {
	rawSubject, rawBody := app.campaignContent(campaign)
	return resolveMailContent(app, rawSubject, sample), resolveMailContent(app, rawBody, sample)
}

// SendDirectEmail queues and immediately delivers a single ad-hoc email to a
// contact, rendering variables against the given source record.
func (app *BaseApp) SendDirectEmail(to, subject, body string, record *Record) error {
	message, err := app.queueMarketingMessage(marketingMessageParams{
		To:      to,
		Subject: subject,
		Body:    body,
		Record:  record,
	})
	if err != nil {
		return err
	}

	return app.deliverMarketingMessage(message)
}
