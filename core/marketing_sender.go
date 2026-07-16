package core

import (
	"fmt"
	"net/mail"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/pocketbase/pocketbase/tools/types"
)

const marketingSenderCron = "__pbMailSender__"

// processMailQueue delivers the due marketing messages, bounded by the
// configured per-minute rate. It runs once per cron tick.
func (app *BaseApp) processMailQueue() {
	cfg := app.Settings().Marketing
	if !cfg.Enabled {
		return
	}

	messages, err := app.FindRecordsByFilter(
		CollectionNameMailMessages,
		"status={:status} && scheduledAt<={:now} && (campaign='' || campaign.status!={:paused})",
		"scheduledAt",
		marketingRate(cfg),
		0,
		dbx.Params{
			"status": MailMessageStatusQueued,
			"now":    types.NowDateTime(),
			"paused": MailCampaignStatusPaused,
		},
	)
	if err != nil {
		app.Logger().Error("Failed to load the marketing mail queue", "error", err.Error())
		return
	}

	for _, message := range messages {
		if err := app.deliverMarketingMessage(message); err != nil {
			app.Logger().Error("Failed to deliver marketing message", "id", message.Id, "error", err.Error())
		}
	}
}

// processScheduledCampaigns enqueues campaigns whose scheduled time has arrived.
func (app *BaseApp) processScheduledCampaigns() {
	if !app.Settings().Marketing.Enabled {
		return
	}

	campaigns, err := app.FindRecordsByFilter(
		CollectionNameMailCampaigns,
		"status={:status} && scheduledAt<={:now}",
		"scheduledAt", 50, 0,
		dbx.Params{"status": MailCampaignStatusScheduled, "now": types.NowDateTime()},
	)
	if err != nil {
		app.Logger().Error("Failed to load the scheduled campaigns", "error", err.Error())
		return
	}

	for _, campaign := range campaigns {
		if err := app.enqueueCampaign(campaign); err != nil {
			app.Logger().Error("Failed to enqueue scheduled campaign", "id", campaign.Id, "error", err.Error())
		}
	}
}

func (app *BaseApp) deliverMarketingMessage(message *Record) error {
	message.Set("status", MailMessageStatusSending)
	if err := app.Save(message); err != nil {
		return err
	}

	campaignId := message.GetString("campaign")

	if app.isSuppressed(message.GetString("to")) {
		if err := app.finishMarketingMessage(message, MailMessageStatusCanceled, "recipient is suppressed"); err != nil {
			return err
		}
		app.finalizeCampaignMessage(campaignId, "")
		return nil
	}

	email, err := app.buildMarketingEmail(message)
	if err != nil {
		if err := app.finishMarketingMessage(message, MailMessageStatusFailed, err.Error()); err != nil {
			return err
		}
		app.finalizeCampaignMessage(campaignId, "totalFailed")
		return nil
	}

	client := app.NewMailClient()

	event := new(MailerMarketingEvent)
	event.App = app
	event.Mailer = client
	event.Message = email
	event.Record = message

	sendErr := app.OnMailerMarketingSend().Trigger(event, func(e *MailerMarketingEvent) error {
		return e.Mailer.Send(e.Message)
	})
	if sendErr != nil {
		return app.retryOrFailMarketingMessage(message, sendErr)
	}

	message.Set("sentAt", types.NowDateTime())
	if err := app.finishMarketingMessage(message, MailMessageStatusSent, ""); err != nil {
		return err
	}
	app.finalizeCampaignMessage(campaignId, "totalSent")

	return nil
}

func (app *BaseApp) buildMarketingEmail(message *Record) (*mailer.Message, error) {
	address, err := mail.ParseAddress(message.GetString("to"))
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}

	meta := app.Settings().Meta
	base := marketingBaseURL(app)
	token := message.GetString("token")

	return &mailer.Message{
		From:    mail.Address{Name: meta.SenderName, Address: meta.SenderAddress},
		To:      []mail.Address{*address},
		Subject: message.GetString("subject"),
		HTML:    buildMarketingBody(app, message),
		Headers: map[string]string{
			"List-Unsubscribe":      fmt.Sprintf("<%s/api/marketing/unsubscribe/%s>", base, token),
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}, nil
}

func (app *BaseApp) retryOrFailMarketingMessage(message *Record, cause error) error {
	attempts := message.GetInt("attempts") + 1
	message.Set("attempts", attempts)

	cfg := app.Settings().Marketing
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	if attempts >= maxAttempts {
		if err := app.finishMarketingMessage(message, MailMessageStatusFailed, cause.Error()); err != nil {
			return err
		}
		app.finalizeCampaignMessage(message.GetString("campaign"), "totalFailed")
		return nil
	}

	message.Set("status", MailMessageStatusQueued)
	message.Set("error", cause.Error())
	message.Set("scheduledAt", types.NowDateTime().Add(time.Duration(attempts*attempts)*time.Minute))

	return app.Save(message)
}

func (app *BaseApp) finishMarketingMessage(message *Record, status, reason string) error {
	message.Set("status", status)
	message.Set("error", reason)
	return app.Save(message)
}

// finalizeCampaignMessage bumps an optional counter on the parent campaign
// and flips it to "sent" once no message is left pending.
func (app *BaseApp) finalizeCampaignMessage(campaignId, counterField string) {
	if campaignId == "" {
		return
	}

	campaign, err := app.FindRecordById(CollectionNameMailCampaigns, campaignId)
	if err != nil {
		return
	}

	if counterField != "" {
		campaign.Set(counterField, campaign.GetInt(counterField)+1)
	}

	pending, _ := app.CountRecords(
		CollectionNameMailMessages,
		dbx.HashExp{"campaign": campaignId},
		dbx.In("status", MailMessageStatusQueued, MailMessageStatusSending),
	)
	if pending == 0 && campaign.GetString("status") == MailCampaignStatusSending {
		campaign.Set("status", MailCampaignStatusSent)
		campaign.Set("sentAt", types.NowDateTime())
	}

	if err := app.Save(campaign); err != nil {
		app.Logger().Warn("Failed to update campaign progress", "campaign", campaignId, "error", err.Error())
	}
}

func (app *BaseApp) isSuppressed(email string) bool {
	_, err := app.FindFirstRecordByFilter(
		CollectionNameMailSuppressions,
		"email={:email}",
		dbx.Params{"email": email},
	)
	return err == nil
}

func marketingRate(cfg MarketingConfig) int {
	if cfg.RatePerMinute <= 0 {
		return 120
	}
	return cfg.RatePerMinute
}
