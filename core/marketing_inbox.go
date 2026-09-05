package core

import (
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"
)

const marketingInboxCron = "__pbMailInbox__"

const maxInboundBodyBytes = 2_000_000

// inboundEmail is the parsed content of a single fetched IMAP message.
type inboundEmail struct {
	from       string
	to         string
	subject    string
	messageId  string
	body       string
	receivedAt types.DateTime
}

// processMailInbox polls the configured IMAP mailbox for unseen messages
// and stores them as _mailInbox records. It runs once per cron tick.
func (app *BaseApp) processMailInbox() {
	cfg := app.Settings().Marketing
	if !cfg.Enabled || !cfg.IMAP.Enabled {
		return
	}

	c, err := dialIMAP(cfg.IMAP)
	if err != nil {
		app.Logger().Error("Failed to connect to the IMAP server", "error", err.Error())
		return
	}
	defer c.Logout()

	if err := c.Login(cfg.IMAP.Username, cfg.IMAP.Password); err != nil {
		app.Logger().Error("Failed to login to the IMAP server", "error", err.Error())
		return
	}

	mailbox := cfg.IMAP.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if _, err := c.Select(mailbox, false); err != nil {
		app.Logger().Error("Failed to select the IMAP mailbox", "mailbox", mailbox, "error", err.Error())
		return
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	ids, err := c.Search(criteria)
	if err != nil {
		app.Logger().Error("Failed to search for unseen IMAP messages", "error", err.Error())
		return
	}
	if len(ids) == 0 {
		return
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)

	section := &imap.BodySectionName{}
	messages := make(chan *imap.Message, len(ids))
	if err := c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}, messages); err != nil {
		app.Logger().Error("Failed to fetch the unseen IMAP messages", "error", err.Error())
		return
	}

	for message := range messages {
		if err := app.ingestInboundMessage(message, section); err != nil {
			app.Logger().Error("Failed to ingest inbound message", "error", err.Error())
			continue
		}
		app.markInboundSeen(c, message.SeqNum)
	}
}

// dialIMAP establishes the raw IMAP connection, either with implicit TLS
// or plain with a best-effort StartTLS upgrade.
func dialIMAP(cfg IMAPConfig) (*client.Client, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	if cfg.TLS {
		return client.DialTLS(addr, nil)
	}

	c, err := client.Dial(addr)
	if err != nil {
		return nil, err
	}

	if ok, _ := c.SupportStartTLS(); ok {
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			c.Logout()
			return nil, err
		}
	}

	return c, nil
}

// ingestInboundMessage parses a fetched message and stores it as a
// _mailInbox record, skipping messages that were already ingested.
func (app *BaseApp) ingestInboundMessage(message *imap.Message, section *imap.BodySectionName) error {
	literal := message.GetBody(section)
	if literal == nil {
		return errors.New("missing message body")
	}

	email, err := parseInboundEmail(literal)
	if err != nil {
		return err
	}

	if app.hasInboundMessage(email.messageId) {
		return nil
	}

	collection, err := app.FindCachedCollectionByNameOrId(CollectionNameMailInbox)
	if err != nil {
		return err
	}

	record := NewRecord(collection)
	record.Set("from", email.from)
	record.Set("to", email.to)
	record.Set("subject", email.subject)
	record.Set("body", email.body)
	record.Set("messageId", email.messageId)
	record.Set("receivedAt", email.receivedAt)

	if err := app.Save(record); err != nil {
		return err
	}

	app.suppressBouncedRecipient(email)

	return nil
}

func (app *BaseApp) hasInboundMessage(messageId string) bool {
	_, err := app.FindFirstRecordByData(CollectionNameMailInbox, "messageId", messageId)
	return err == nil
}

// markInboundSeen flags an ingested message as \Seen so that the next
// poll no longer returns it.
func (app *BaseApp) markInboundSeen(c *client.Client, seqNum uint32) {
	seqset := new(imap.SeqSet)
	seqset.AddNum(seqNum)

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := c.Store(seqset, item, []interface{}{imap.SeenFlag}, nil); err != nil {
		app.Logger().Warn("Failed to mark inbound message as seen", "error", err.Error())
	}
}

// parseInboundEmail extracts the addressing, subject and body of a raw
// RFC822 message.
func parseInboundEmail(r io.Reader) (*inboundEmail, error) {
	mr, err := mail.CreateReader(r)
	if err != nil {
		return nil, err
	}

	email := &inboundEmail{receivedAt: types.NowDateTime()}

	if from, err := mr.Header.AddressList("From"); err == nil && len(from) > 0 {
		email.from = from[0].Address
	}
	if email.from == "" {
		return nil, errors.New("missing sender address")
	}

	if to, err := mr.Header.AddressList("To"); err == nil {
		addresses := make([]string, len(to))
		for i, address := range to {
			addresses[i] = address.Address
		}
		email.to = strings.Join(addresses, ", ")
	}

	email.subject, _ = mr.Header.Subject()

	email.messageId, _ = mr.Header.MessageID()
	if email.messageId == "" {
		email.messageId = security.RandomString(32)
	}

	if date, err := mr.Header.Date(); err == nil && !date.IsZero() {
		if received, err := types.ParseDateTime(date); err == nil {
			email.receivedAt = received
		}
	}

	email.body = readInboundBody(mr)

	return email, nil
}

// readInboundBody walks the MIME parts and returns the message content as
// HTML, preferring a text/html part and falling back to escaped text/plain.
func readInboundBody(mr *mail.Reader) string {
	var htmlBody, plainBody string

	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}

		header, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}

		content, _ := io.ReadAll(io.LimitReader(part.Body, maxInboundBodyBytes))
		mediaType, _, _ := header.ContentType()

		switch mediaType {
		case "text/html":
			if htmlBody == "" {
				htmlBody = string(content)
			}
		case "text/plain":
			if plainBody == "" {
				plainBody = string(content)
			}
		}
	}

	if htmlBody != "" {
		return htmlBody
	}
	if plainBody == "" {
		return ""
	}

	return "<pre>" + html.EscapeString(strings.TrimSpace(plainBody)) + "</pre>"
}

var inboundAddressRegex = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

// isBounceNotification reports whether an inbound message looks like a
// delivery failure report.
func isBounceNotification(email *inboundEmail) bool {
	local, _, _ := strings.Cut(strings.ToLower(email.from), "@")
	if local == "mailer-daemon" || local == "postmaster" {
		return true
	}

	subject := strings.ToLower(email.subject)
	for _, prefix := range []string{"undelivered", "delivery status notification", "mail delivery failed"} {
		if strings.HasPrefix(subject, prefix) {
			return true
		}
	}

	return false
}

// suppressBouncedRecipient tries to extract the failed recipient out of a
// bounce notification and adds it to the suppression list. It is best-effort
// and never fails the ingestion.
func (app *BaseApp) suppressBouncedRecipient(email *inboundEmail) {
	if !isBounceNotification(email) {
		return
	}

	recipient := extractBouncedRecipient(email)
	if recipient == "" || app.isSuppressed(recipient) {
		return
	}

	collection, err := app.FindCachedCollectionByNameOrId(CollectionNameMailSuppressions)
	if err != nil {
		return
	}

	record := NewRecord(collection)
	record.Set("email", recipient)
	record.Set("reason", MailSuppressionReasonBounce)
	record.Set("source", "imap")

	if err := app.Save(record); err != nil {
		app.Logger().Warn("Failed to suppress bounced recipient", "email", recipient, "error", err.Error())
	}
}

// extractBouncedRecipient returns the first address mentioned in the body
// that differs from the bounce sender.
func extractBouncedRecipient(email *inboundEmail) string {
	for _, match := range inboundAddressRegex.FindAllString(email.body, -1) {
		if !strings.EqualFold(match, email.from) {
			return match
		}
	}

	return ""
}
