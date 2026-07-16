package core

import (
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/routine"
)

const marketingAutomationsCron = "__pbMailAutomations__"

// registerMarketingHooks wires the background sender, the automation runner
// and the record triggers that enroll records into automations.
func (app *BaseApp) registerMarketingHooks() {
	app.Cron().Add(marketingSenderCron, "* * * * *", func() {
		app.processScheduledCampaigns()
		app.processMailQueue()
	})
	app.Cron().Add(marketingAutomationsCron, "* * * * *", app.processAutomationEnrollments)

	app.OnRecordAfterCreateSuccess().Bind(&hook.Handler[*RecordEvent]{
		Id: "__pbMarketingTriggerCreate__",
		Func: func(e *RecordEvent) error {
			app.triggerAutomations(e.Record, MailTriggerEventCreate)
			return e.Next()
		},
	})

	app.OnRecordAfterUpdateSuccess().Bind(&hook.Handler[*RecordEvent]{
		Id: "__pbMarketingTriggerUpdate__",
		Func: func(e *RecordEvent) error {
			app.triggerAutomations(e.Record, MailTriggerEventUpdate)
			return e.Next()
		},
	})
}

// triggerAutomations dispatches automation enrollment off the request path.
// System collections never trigger automations and are skipped early.
func (app *BaseApp) triggerAutomations(record *Record, event string) {
	if record.Collection().System {
		return
	}

	routine.FireAndForget(func() {
		app.dispatchAutomationTrigger(record, event)
	})
}
