package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

func setupMarketing(t *testing.T) *tests.TestApp {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}

	app.Settings().Marketing.Enabled = true
	if err := app.Save(app.Settings()); err != nil {
		t.Fatal(err)
	}

	customers := core.NewBaseCollection("customers")
	customers.Fields.Add(&core.TextField{Name: "name"})
	customers.Fields.Add(&core.EmailField{Name: "email"})
	if err := app.Save(customers); err != nil {
		t.Fatal(err)
	}

	for _, addr := range []string{"a@example.com", "b@example.com"} {
		record := core.NewRecord(customers)
		record.Set("name", "Friend")
		record.Set("email", addr)
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
	}

	return app
}

func newCampaign(t *testing.T, app core.App) *core.Record {
	t.Helper()

	collection, err := app.FindCollectionByNameOrId(core.CollectionNameMailCampaigns)
	if err != nil {
		t.Fatal(err)
	}

	campaign := core.NewRecord(collection)
	campaign.Set("name", "Promo")
	campaign.Set("subject", "Hello {RECORD:name}")
	campaign.Set("body", `<p>Hi {RECORD:name}</p><a href="https://example.com">Shop</a>`)
	campaign.Set("audienceCollection", "customers")
	campaign.Set("status", core.MailCampaignStatusDraft)
	if err := app.Save(campaign); err != nil {
		t.Fatal(err)
	}

	return campaign
}

func runCronJob(app core.App, id string) {
	for _, job := range app.Cron().Jobs() {
		if job.Id() == id {
			job.Run()
			return
		}
	}
}

func queuedCount(t *testing.T, app core.App, filter string) int {
	t.Helper()
	total, err := app.CountRecordsByFilter(core.CollectionNameMailMessages, filter)
	if err != nil {
		t.Fatal(err)
	}
	return int(total)
}

func TestMarketingCampaignSend(t *testing.T) {
	app := setupMarketing(t)
	defer app.Cleanup()

	campaign := newCampaign(t, app)

	if err := app.SendCampaign(campaign); err != nil {
		t.Fatal(err)
	}

	if got := queuedCount(t, app, "campaign='"+campaign.Id+"'"); got != 2 {
		t.Fatalf("expected 2 queued messages, got %d", got)
	}
	if campaign.GetString("status") != core.MailCampaignStatusSending {
		t.Fatalf("expected campaign status sending, got %q", campaign.GetString("status"))
	}

	runCronJob(app, "__pbMailSender__")

	if app.TestMailer.TotalSend() != 2 {
		t.Fatalf("expected 2 sent emails, got %d", app.TestMailer.TotalSend())
	}

	body := app.TestMailer.FirstMessage().HTML
	for _, want := range []string{"Hi Friend", "/api/marketing/o/", "/api/marketing/c/", "/api/marketing/unsubscribe/"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected rendered body to contain %q, got:\n%s", want, body)
		}
	}

	refreshed, err := app.FindRecordById(core.CollectionNameMailCampaigns, campaign.Id)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.GetString("status") != core.MailCampaignStatusSent {
		t.Fatalf("expected campaign status sent, got %q", refreshed.GetString("status"))
	}
	if refreshed.GetInt("totalSent") != 2 {
		t.Fatalf("expected totalSent 2, got %d", refreshed.GetInt("totalSent"))
	}
}

func TestMarketingCampaignSegment(t *testing.T) {
	app := setupMarketing(t)
	defer app.Cleanup()

	segments, err := app.FindCollectionByNameOrId(core.CollectionNameMailSegments)
	if err != nil {
		t.Fatal(err)
	}
	segment := core.NewRecord(segments)
	segment.Set("name", "A customers")
	segment.Set("collection", "customers")
	segment.Set("filter", "email='a@example.com'")
	if err := app.Save(segment); err != nil {
		t.Fatal(err)
	}

	campaign := newCampaign(t, app)
	campaign.Set("audienceCollection", "")
	campaign.Set("audienceFilter", "")
	campaign.Set("segment", segment.Id)
	if err := app.Save(campaign); err != nil {
		t.Fatal(err)
	}

	if err := app.SendCampaign(campaign); err != nil {
		t.Fatal(err)
	}

	if got := queuedCount(t, app, "campaign='"+campaign.Id+"'"); got != 1 {
		t.Fatalf("expected the segment to narrow the audience to 1 queued message, got %d", got)
	}
}

func TestMarketingSuppression(t *testing.T) {
	app := setupMarketing(t)
	defer app.Cleanup()

	suppressions, err := app.FindCollectionByNameOrId(core.CollectionNameMailSuppressions)
	if err != nil {
		t.Fatal(err)
	}
	suppression := core.NewRecord(suppressions)
	suppression.Set("email", "a@example.com")
	suppression.Set("reason", core.MailSuppressionReasonUnsubscribe)
	if err := app.Save(suppression); err != nil {
		t.Fatal(err)
	}

	campaign := newCampaign(t, app)
	if err := app.SendCampaign(campaign); err != nil {
		t.Fatal(err)
	}

	if got := queuedCount(t, app, "campaign='"+campaign.Id+"'"); got != 1 {
		t.Fatalf("expected suppressed recipient to be skipped (1 queued), got %d", got)
	}
}

func TestMarketingTestEmail(t *testing.T) {
	app := setupMarketing(t)
	defer app.Cleanup()

	campaign := newCampaign(t, app)

	if err := app.SendTestEmail(campaign, "qa@example.com", nil); err != nil {
		t.Fatal(err)
	}

	if app.TestMailer.TotalSend() != 1 {
		t.Fatalf("expected 1 test email, got %d", app.TestMailer.TotalSend())
	}
	if to := app.TestMailer.FirstMessage().To; len(to) != 1 || to[0].Address != "qa@example.com" {
		t.Fatalf("unexpected test recipient: %v", to)
	}
}

func TestMarketingAutomation(t *testing.T) {
	app := setupMarketing(t)
	defer app.Cleanup()

	automations, err := app.FindCollectionByNameOrId(core.CollectionNameMailAutomations)
	if err != nil {
		t.Fatal(err)
	}
	automation := core.NewRecord(automations)
	automation.Set("name", "Welcome")
	automation.Set("enabled", true)
	automation.Set("triggerCollection", "customers")
	automation.Set("triggerEvent", core.MailTriggerEventCreate)
	automation.Set("steps", `[{"kind":"email","subject":"Welcome","body":"<p>Welcome {RECORD:name}</p>"},{"kind":"delay","seconds":259200},{"kind":"email","subject":"Day 3","body":"<p>Still here?</p>"}]`)
	if err := app.Save(automation); err != nil {
		t.Fatal(err)
	}

	enrollments, err := app.FindCollectionByNameOrId(core.CollectionNameMailEnrollments)
	if err != nil {
		t.Fatal(err)
	}
	enrollment := core.NewRecord(enrollments)
	enrollment.Set("automation", automation.Id)
	enrollment.Set("collectionRef", "customers")
	customer, err := app.FindFirstRecordByFilter("customers", "email='a@example.com'")
	if err != nil {
		t.Fatal(err)
	}
	enrollment.Set("recordRef", customer.Id)
	enrollment.Set("email", "a@example.com")
	enrollment.Set("status", core.MailEnrollmentStatusActive)
	enrollment.Set("nextRunAt", types.NowDateTime())
	if err := app.Save(enrollment); err != nil {
		t.Fatal(err)
	}

	// first tick processes the email step and queues the welcome message
	runCronJob(app, "__pbMailAutomations__")

	afterEmail, err := app.FindRecordById(core.CollectionNameMailEnrollments, enrollment.Id)
	if err != nil {
		t.Fatal(err)
	}
	if afterEmail.GetInt("step") != 1 {
		t.Fatalf("expected enrollment to advance to step 1, got %d", afterEmail.GetInt("step"))
	}

	runCronJob(app, "__pbMailSender__")
	if app.TestMailer.TotalSend() != 1 {
		t.Fatalf("expected 1 automation email delivered, got %d", app.TestMailer.TotalSend())
	}

	// second tick processes the delay step and pushes the next run into the future
	runCronJob(app, "__pbMailAutomations__")

	afterDelay, err := app.FindRecordById(core.CollectionNameMailEnrollments, enrollment.Id)
	if err != nil {
		t.Fatal(err)
	}
	if afterDelay.GetInt("step") != 2 {
		t.Fatalf("expected enrollment to advance to step 2, got %d", afterDelay.GetInt("step"))
	}
	if afterDelay.GetDateTime("nextRunAt").Time().Before(types.NowDateTime().Add(48 * time.Hour).Time()) {
		t.Fatalf("expected the delay step to push nextRunAt at least ~2 days out")
	}
}
