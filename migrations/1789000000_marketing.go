package migrations

import (
	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		builders := []func(core.App) error{
			createMailTemplatesCollection,
			createMailSuppressionsCollection,
			createMailCampaignsCollection,
			createMailAutomationsCollection,
			createMailEnrollmentsCollection,
			createMailMessagesCollection,
		}

		for _, build := range builders {
			if err := build(txApp); err != nil {
				return err
			}
		}

		return nil
	}, func(txApp core.App) error {
		names := []string{
			core.CollectionNameMailMessages,
			core.CollectionNameMailEnrollments,
			core.CollectionNameMailAutomations,
			core.CollectionNameMailCampaigns,
			core.CollectionNameMailSuppressions,
			core.CollectionNameMailTemplates,
		}

		for _, name := range names {
			col, err := txApp.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			if err := txApp.Delete(col); err != nil {
				return err
			}
		}

		return nil
	})
}

func createMailTemplatesCollection(txApp core.App) error {
	col := core.NewBaseCollection(core.CollectionNameMailTemplates)
	col.System = true

	col.Fields.Add(&core.TextField{Name: "name", System: true, Required: true})
	col.Fields.Add(&core.TextField{Name: "subject", System: true, Required: true})
	col.Fields.Add(&core.EditorField{Name: "body", System: true, Required: true})
	addTimestamps(col)

	col.AddIndex("idx_mailTemplates_name", true, "name", "")

	return txApp.Save(col)
}

func createMailSuppressionsCollection(txApp core.App) error {
	col := core.NewBaseCollection(core.CollectionNameMailSuppressions)
	col.System = true

	col.Fields.Add(&core.EmailField{Name: "email", System: true, Required: true})
	col.Fields.Add(&core.SelectField{
		Name:      "reason",
		System:    true,
		Required:  true,
		MaxSelect: 1,
		Values: []string{
			core.MailSuppressionReasonUnsubscribe,
			core.MailSuppressionReasonBounce,
			core.MailSuppressionReasonComplaint,
			core.MailSuppressionReasonManual,
		},
	})
	col.Fields.Add(&core.TextField{Name: "source", System: true})
	addTimestamps(col)

	col.AddIndex("idx_mailSuppressions_email", true, "email", "")

	return txApp.Save(col)
}

func createMailCampaignsCollection(txApp core.App) error {
	templates, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailTemplates)
	if err != nil {
		return err
	}

	col := core.NewBaseCollection(core.CollectionNameMailCampaigns)
	col.System = true

	col.Fields.Add(&core.TextField{Name: "name", System: true, Required: true})
	col.Fields.Add(&core.TextField{Name: "subject", System: true})
	col.Fields.Add(&core.EditorField{Name: "body", System: true})
	col.Fields.Add(&core.RelationField{Name: "template", System: true, CollectionId: templates.Id, MaxSelect: 1})
	col.Fields.Add(&core.TextField{Name: "audienceCollection", System: true})
	col.Fields.Add(&core.TextField{Name: "audienceFilter", System: true, Max: 2000})
	col.Fields.Add(&core.SelectField{
		Name:      "status",
		System:    true,
		MaxSelect: 1,
		Values: []string{
			core.MailCampaignStatusDraft,
			core.MailCampaignStatusScheduled,
			core.MailCampaignStatusSending,
			core.MailCampaignStatusSent,
			core.MailCampaignStatusPaused,
			core.MailCampaignStatusFailed,
			core.MailCampaignStatusCanceled,
		},
	})
	col.Fields.Add(&core.DateField{Name: "scheduledAt", System: true})
	col.Fields.Add(&core.DateField{Name: "sentAt", System: true})
	for _, counter := range []string{"totalRecipients", "totalSent", "totalOpened", "totalClicked", "totalFailed", "totalUnsubscribed"} {
		col.Fields.Add(&core.NumberField{Name: counter, System: true})
	}
	addTimestamps(col)

	return txApp.Save(col)
}

func createMailAutomationsCollection(txApp core.App) error {
	col := core.NewBaseCollection(core.CollectionNameMailAutomations)
	col.System = true

	col.Fields.Add(&core.TextField{Name: "name", System: true, Required: true})
	col.Fields.Add(&core.BoolField{Name: "enabled", System: true})
	col.Fields.Add(&core.TextField{Name: "triggerCollection", System: true, Required: true})
	col.Fields.Add(&core.SelectField{
		Name:      "triggerEvent",
		System:    true,
		Required:  true,
		MaxSelect: 1,
		Values:    []string{core.MailTriggerEventCreate, core.MailTriggerEventUpdate},
	})
	col.Fields.Add(&core.TextField{Name: "triggerCondition", System: true, Max: 2000})
	col.Fields.Add(&core.TextField{Name: "exitCondition", System: true, Max: 2000})
	col.Fields.Add(&core.JSONField{Name: "steps", System: true, MaxSize: 100_000})
	addTimestamps(col)

	return txApp.Save(col)
}

func createMailEnrollmentsCollection(txApp core.App) error {
	automations, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailAutomations)
	if err != nil {
		return err
	}

	col := core.NewBaseCollection(core.CollectionNameMailEnrollments)
	col.System = true

	col.Fields.Add(&core.RelationField{Name: "automation", System: true, Required: true, CollectionId: automations.Id, MaxSelect: 1, CascadeDelete: true})
	col.Fields.Add(&core.TextField{Name: "collectionRef", System: true, Required: true})
	col.Fields.Add(&core.TextField{Name: "recordRef", System: true, Required: true})
	col.Fields.Add(&core.EmailField{Name: "email", System: true, Required: true})
	col.Fields.Add(&core.NumberField{Name: "step", System: true})
	col.Fields.Add(&core.SelectField{
		Name:      "status",
		System:    true,
		MaxSelect: 1,
		Values: []string{
			core.MailEnrollmentStatusActive,
			core.MailEnrollmentStatusCompleted,
			core.MailEnrollmentStatusExited,
			core.MailEnrollmentStatusFailed,
		},
	})
	col.Fields.Add(&core.DateField{Name: "nextRunAt", System: true})
	addTimestamps(col)

	col.AddIndex("idx_mailEnrollments_dispatch", false, "status, nextRunAt", "")
	col.AddIndex("idx_mailEnrollments_unique", true, "automation, collectionRef, recordRef", "")

	return txApp.Save(col)
}

func createMailMessagesCollection(txApp core.App) error {
	campaigns, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailCampaigns)
	if err != nil {
		return err
	}
	automations, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailAutomations)
	if err != nil {
		return err
	}

	col := core.NewBaseCollection(core.CollectionNameMailMessages)
	col.System = true

	col.Fields.Add(&core.EmailField{Name: "to", System: true, Required: true})
	col.Fields.Add(&core.RelationField{Name: "campaign", System: true, CollectionId: campaigns.Id, MaxSelect: 1, CascadeDelete: true})
	col.Fields.Add(&core.RelationField{Name: "automation", System: true, CollectionId: automations.Id, MaxSelect: 1, CascadeDelete: true})
	col.Fields.Add(&core.TextField{Name: "collectionRef", System: true})
	col.Fields.Add(&core.TextField{Name: "recordRef", System: true})
	col.Fields.Add(&core.TextField{Name: "subject", System: true})
	col.Fields.Add(&core.EditorField{Name: "body", System: true, MaxSize: 5_000_000})
	col.Fields.Add(&core.SelectField{
		Name:      "status",
		System:    true,
		MaxSelect: 1,
		Values: []string{
			core.MailMessageStatusQueued,
			core.MailMessageStatusSending,
			core.MailMessageStatusSent,
			core.MailMessageStatusFailed,
			core.MailMessageStatusBounced,
			core.MailMessageStatusCanceled,
		},
	})
	col.Fields.Add(&core.DateField{Name: "scheduledAt", System: true})
	col.Fields.Add(&core.DateField{Name: "sentAt", System: true})
	col.Fields.Add(&core.NumberField{Name: "attempts", System: true})
	col.Fields.Add(&core.TextField{Name: "error", System: true})
	col.Fields.Add(&core.DateField{Name: "openedAt", System: true})
	col.Fields.Add(&core.NumberField{Name: "openCount", System: true})
	col.Fields.Add(&core.DateField{Name: "clickedAt", System: true})
	col.Fields.Add(&core.NumberField{Name: "clickCount", System: true})
	col.Fields.Add(&core.TextField{Name: "token", System: true, Required: true})
	addTimestamps(col)

	col.AddIndex("idx_mailMessages_dispatch", false, "status, scheduledAt", "")
	col.AddIndex("idx_mailMessages_token", true, "token", "")

	return txApp.Save(col)
}

func addTimestamps(col *core.Collection) {
	col.Fields.Add(&core.AutodateField{Name: "created", System: true, OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", System: true, OnCreate: true, OnUpdate: true})
}
