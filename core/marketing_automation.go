package core

import (
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/tools/types"
)

// automationStep is one node of an automation pipeline: either an email to
// send or a delay before the next node.
type automationStep struct {
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Template string `json:"template"`
	Seconds  int    `json:"seconds"`
}

// dispatchAutomationTrigger enrolls a record into every enabled automation
// whose trigger matches the record's collection, event and condition.
func (app *BaseApp) dispatchAutomationTrigger(record *Record, event string) {
	automations, err := app.FindRecordsByFilter(
		CollectionNameMailAutomations,
		"enabled=true && triggerCollection={:collection} && triggerEvent={:event}",
		"", 200, 0,
		dbx.Params{"collection": record.Collection().Name, "event": event},
	)
	if err != nil {
		app.Logger().Error("Failed to load automations", "error", err.Error())
		return
	}

	for _, automation := range automations {
		if !app.recordMatchesFilter(record, automation.GetString("triggerCondition")) {
			continue
		}
		if err := app.enrollRecord(automation, record); err != nil {
			app.Logger().Error("Failed to enroll record", "automation", automation.Id, "record", record.Id, "error", err.Error())
		}
	}
}

func (app *BaseApp) enrollRecord(automation *Record, record *Record) error {
	to := recordEmail(record)
	if to == "" {
		return nil
	}

	existing, _ := app.FindFirstRecordByFilter(
		CollectionNameMailEnrollments,
		"automation={:a} && collectionRef={:c} && recordRef={:r}",
		dbx.Params{"a": automation.Id, "c": record.Collection().Name, "r": record.Id},
	)
	if existing != nil {
		return nil
	}

	collection, err := app.FindCachedCollectionByNameOrId(CollectionNameMailEnrollments)
	if err != nil {
		return err
	}

	enrollment := NewRecord(collection)
	enrollment.Set("automation", automation.Id)
	enrollment.Set("collectionRef", record.Collection().Name)
	enrollment.Set("recordRef", record.Id)
	enrollment.Set("email", to)
	enrollment.Set("step", 0)
	enrollment.Set("status", MailEnrollmentStatusActive)
	enrollment.Set("nextRunAt", types.NowDateTime())

	return app.Save(enrollment)
}

// processAutomationEnrollments advances every active enrollment whose next
// step is due. It runs once per cron tick.
func (app *BaseApp) processAutomationEnrollments() {
	if !app.Settings().Marketing.Enabled {
		return
	}

	enrollments, err := app.FindRecordsByFilter(
		CollectionNameMailEnrollments,
		"status={:status} && nextRunAt<={:now}",
		"nextRunAt", 200, 0,
		dbx.Params{"status": MailEnrollmentStatusActive, "now": types.NowDateTime()},
	)
	if err != nil {
		app.Logger().Error("Failed to load automation enrollments", "error", err.Error())
		return
	}

	for _, enrollment := range enrollments {
		if err := app.advanceEnrollment(enrollment); err != nil {
			app.Logger().Error("Failed to advance enrollment", "id", enrollment.Id, "error", err.Error())
		}
	}
}

func (app *BaseApp) advanceEnrollment(enrollment *Record) error {
	automation, err := app.FindRecordById(CollectionNameMailAutomations, enrollment.GetString("automation"))
	if err != nil {
		return app.exitEnrollment(enrollment, MailEnrollmentStatusFailed)
	}

	source, err := app.FindRecordById(enrollment.GetString("collectionRef"), enrollment.GetString("recordRef"))
	if err != nil {
		return app.exitEnrollment(enrollment, MailEnrollmentStatusExited)
	}

	if exit := automation.GetString("exitCondition"); exit != "" && app.recordMatchesFilter(source, exit) {
		return app.exitEnrollment(enrollment, MailEnrollmentStatusExited)
	}

	var steps []automationStep
	if err := automation.UnmarshalJSONField("steps", &steps); err != nil {
		return app.exitEnrollment(enrollment, MailEnrollmentStatusFailed)
	}

	index := enrollment.GetInt("step")
	if index >= len(steps) {
		return app.exitEnrollment(enrollment, MailEnrollmentStatusCompleted)
	}

	step := steps[index]
	nextRun := types.NowDateTime()

	switch step.Kind {
	case MailStepKindEmail:
		if err := app.enqueueAutomationEmail(automation, enrollment, source, step); err != nil {
			return err
		}
	case MailStepKindDelay:
		nextRun = nextRun.Add(time.Duration(step.Seconds) * time.Second)
	}

	enrollment.Set("step", index+1)
	enrollment.Set("nextRunAt", nextRun)

	return app.Save(enrollment)
}

func (app *BaseApp) enqueueAutomationEmail(automation, enrollment, source *Record, step automationStep) error {
	subject, body := step.Subject, step.Body

	if step.Template != "" {
		if template, err := app.FindRecordById(CollectionNameMailTemplates, step.Template); err == nil {
			if subject == "" {
				subject = template.GetString("subject")
			}
			if body == "" {
				body = template.GetString("body")
			}
		}
	}

	to := enrollment.GetString("email")
	if app.isSuppressed(to) {
		return nil
	}

	_, err := app.queueMarketingMessage(marketingMessageParams{
		To:           to,
		Subject:      subject,
		Body:         body,
		Record:       source,
		AutomationId: automation.Id,
	})

	return err
}

func (app *BaseApp) exitEnrollment(enrollment *Record, status string) error {
	enrollment.Set("status", status)
	return app.Save(enrollment)
}

// recordMatchesFilter reports whether a record satisfies a PocketBase filter
// expression. An empty expression always matches.
func (app *BaseApp) recordMatchesFilter(record *Record, filter string) bool {
	if strings.TrimSpace(filter) == "" {
		return true
	}

	found, err := app.FindRecordsByFilter(
		record.Collection(),
		"id={:id} && ("+filter+")",
		"", 1, 0,
		dbx.Params{"id": record.Id},
	)

	return err == nil && len(found) > 0
}
