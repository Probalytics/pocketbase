package core

import (
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/routine"
)

const (
	marketingAutomationsCron = "__pbMailAutomations__"
	storeKeyHasAutomations   = "__pbHasMailAutomations__"
)

// registerMarketingHooks wires the background sender, the automation runner
// and the record triggers that enroll records into automations.
func (app *BaseApp) registerMarketingHooks() {
	app.Cron().Add(marketingSenderCron, "* * * * *", func() {
		app.processScheduledCampaigns()
		app.processMailQueue()
	})
	app.Cron().Add(marketingAutomationsCron, "* * * * *", app.processAutomationEnrollments)
	app.Cron().Add(marketingInboxCron, "* * * * *", app.processMailInbox)

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

	invalidateFlag := func(e *RecordEvent) error {
		app.Store().Remove(storeKeyHasAutomations)
		return e.Next()
	}
	app.OnRecordAfterCreateSuccess(CollectionNameMailAutomations).BindFunc(invalidateFlag)
	app.OnRecordAfterUpdateSuccess(CollectionNameMailAutomations).BindFunc(invalidateFlag)
	app.OnRecordAfterDeleteSuccess(CollectionNameMailAutomations).BindFunc(invalidateFlag)
}

// triggerAutomations dispatches automation enrollment off the request path.
// It skips system collections and, via a cached flag, does nothing at all
// when there are no enabled automations to match against.
func (app *BaseApp) triggerAutomations(record *Record, event string) {
	if record.Collection().System || !app.hasEnabledAutomations() {
		return
	}

	routine.FireAndForget(func() {
		app.dispatchAutomationTrigger(record, event)
	})
}

// hasEnabledAutomations reports whether any enabled automation exists,
// caching the result until an automation record changes.
func (app *BaseApp) hasEnabledAutomations() bool {
	if cached := app.Store().Get(storeKeyHasAutomations); cached != nil {
		enabled, _ := cached.(bool)
		return enabled
	}

	total, err := app.CountRecordsByFilter(CollectionNameMailAutomations, "enabled=true")
	if err != nil {
		return false
	}

	enabled := total > 0
	app.Store().Set(storeKeyHasAutomations, enabled)

	return enabled
}
