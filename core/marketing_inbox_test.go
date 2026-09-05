package core_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	imapclient "github.com/emersion/go-imap/client"
	imapserver "github.com/emersion/go-imap/server"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func setupInbox(t *testing.T) (*tests.TestApp, string) {
	t.Helper()

	app := setupMarketing(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	server := imapserver.New(memory.New())
	server.AllowInsecureAuth = true
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	app.Settings().Marketing.IMAP = core.IMAPConfig{
		Enabled:  true,
		Host:     "127.0.0.1",
		Port:     listener.Addr().(*net.TCPAddr).Port,
		Username: "username",
		Password: "password",
	}

	return app, listener.Addr().String()
}

func appendIMAPMessage(t *testing.T, addr, raw string) {
	t.Helper()

	c, err := imapclient.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Logout()

	if err := c.Login("username", "password"); err != nil {
		t.Fatal(err)
	}

	if err := c.Append("INBOX", nil, time.Now(), strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
}

func inboxCount(t *testing.T, app core.App, filter string) int {
	t.Helper()

	total, err := app.CountRecordsByFilter(core.CollectionNameMailInbox, filter)
	if err != nil {
		t.Fatal(err)
	}

	return int(total)
}

func TestMarketingInboxPolling(t *testing.T) {
	app, addr := setupInbox(t)
	defer app.Cleanup()

	appendIMAPMessage(t, addr, "From: Ada <ada@example.com>\r\n"+
		"To: support@acme.test\r\n"+
		"Subject: Re: Summer Sale\r\n"+
		"Date: Mon, 01 Jun 2026 10:00:00 +0000\r\n"+
		"Message-ID: <reply-1@example.com>\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"Sounds great!")

	runCronJob(app, "__pbMailInbox__")

	received, err := app.FindFirstRecordByData(core.CollectionNameMailInbox, "messageId", "reply-1@example.com")
	if err != nil {
		t.Fatalf("expected the reply to be ingested: %v", err)
	}
	if from := received.GetString("from"); from != "ada@example.com" {
		t.Fatalf("expected from ada@example.com, got %q", from)
	}
	if to := received.GetString("to"); to != "support@acme.test" {
		t.Fatalf("expected to support@acme.test, got %q", to)
	}
	if subject := received.GetString("subject"); subject != "Re: Summer Sale" {
		t.Fatalf("expected subject Re: Summer Sale, got %q", subject)
	}
	if body := received.GetString("body"); !strings.Contains(body, "Sounds great!") {
		t.Fatalf("expected the body to contain the reply text, got %q", body)
	}
	if total := inboxCount(t, app, ""); total != 1 {
		t.Fatalf("expected 1 inbox record, got %d", total)
	}

	// a rerun must not duplicate the already ingested message
	runCronJob(app, "__pbMailInbox__")

	if total := inboxCount(t, app, "messageId='reply-1@example.com'"); total != 1 {
		t.Fatalf("expected the rerun to not duplicate the message, got %d records", total)
	}
}

func TestMarketingInboxBounce(t *testing.T) {
	app, addr := setupInbox(t)
	defer app.Cleanup()

	appendIMAPMessage(t, addr, "From: Mail Delivery System <mailer-daemon@mail.acme.test>\r\n"+
		"To: support@acme.test\r\n"+
		"Subject: Undelivered Mail Returned to Sender\r\n"+
		"Message-ID: <bounce-1@mail.acme.test>\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"The following address failed:\r\n"+
		"\r\n"+
		"b@example.com\r\n"+
		"\r\n"+
		"Reason: mailbox unavailable")

	runCronJob(app, "__pbMailInbox__")

	if total := inboxCount(t, app, "messageId='bounce-1@mail.acme.test'"); total != 1 {
		t.Fatalf("expected the bounce message to be ingested, got %d records", total)
	}

	suppression, err := app.FindFirstRecordByData(core.CollectionNameMailSuppressions, "email", "b@example.com")
	if err != nil {
		t.Fatalf("expected a suppression for the bounced recipient: %v", err)
	}
	if reason := suppression.GetString("reason"); reason != core.MailSuppressionReasonBounce {
		t.Fatalf("expected suppression reason bounce, got %q", reason)
	}
}

func TestMarketingReplyToHeader(t *testing.T) {
	app := setupMarketing(t)
	defer app.Cleanup()

	app.Settings().Marketing.ReplyTo = "replies@example.com"

	campaign := newCampaign(t, app)
	if err := app.SendCampaign(campaign); err != nil {
		t.Fatal(err)
	}

	runCronJob(app, "__pbMailSender__")

	if app.TestMailer.TotalSend() == 0 {
		t.Fatal("expected at least 1 sent email")
	}
	if replyTo := app.TestMailer.FirstMessage().Headers["Reply-To"]; replyTo != "replies@example.com" {
		t.Fatalf("expected Reply-To replies@example.com, got %q", replyTo)
	}
}
