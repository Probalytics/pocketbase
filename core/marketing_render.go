package core

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var (
	mailVarRegex  = regexp.MustCompile(`{RECORD:(\w+)}`)
	mailHrefRegex = regexp.MustCompile(`(?i)(<a\b[^>]*\shref=")(https?://[^"]+)(")`)
)

// resolveMailContent substitutes {APP_NAME}, {APP_URL} and per-record
// {RECORD:field} placeholders. It runs when a message is queued, so the
// stored copy already reflects the recipient.
func resolveMailContent(app App, text string, record *Record) string {
	meta := app.Settings().Meta

	replacements := strings.NewReplacer(
		"{APP_NAME}", meta.AppName,
		"{APP_URL}", meta.AppURL,
	)
	text = replacements.Replace(text)

	if record != nil {
		text = mailVarRegex.ReplaceAllStringFunc(text, func(match string) string {
			field := mailVarRegex.FindStringSubmatch(match)[1]
			return html.EscapeString(record.GetString(field))
		})
	}

	return text
}

// buildMarketingBody produces the final HTML delivered to a recipient:
// the stored body, click tracking, a compliance footer and an open pixel.
func buildMarketingBody(app App, message *Record) string {
	cfg := app.Settings().Marketing
	base := marketingBaseURL(app)
	token := message.GetString("token")

	body := message.GetString("body")

	if cfg.TrackClicks {
		body = rewriteLinksForTracking(body, base, token)
	}

	body += marketingFooter(app, token)

	if cfg.TrackOpens {
		body += fmt.Sprintf(
			`<img src="%s/api/marketing/o/%s" alt="" width="1" height="1" style="display:none">`,
			base, token,
		)
	}

	return body
}

func rewriteLinksForTracking(body, base, token string) string {
	return mailHrefRegex.ReplaceAllStringFunc(body, func(match string) string {
		parts := mailHrefRegex.FindStringSubmatch(match)
		tracked := fmt.Sprintf("%s/api/marketing/c/%s?u=%s", base, token, url.QueryEscape(parts[2]))
		return parts[1] + tracked + parts[3]
	})
}

func marketingFooter(app App, token string) string {
	cfg := app.Settings().Marketing
	base := marketingBaseURL(app)

	label := cfg.UnsubscribeText
	if label == "" {
		label = "Unsubscribe"
	}

	var address string
	if cfg.MailingAddress != "" {
		address = "<div>" + html.EscapeString(cfg.MailingAddress) + "</div>"
	}

	return fmt.Sprintf(
		`<div style="margin-top:24px;padding-top:16px;border-top:1px solid #e5e7eb;`+
			`font-size:12px;color:#94a3b8;text-align:center">%s`+
			`<div><a href="%s/api/marketing/unsubscribe/%s" style="color:#94a3b8">%s</a></div></div>`,
		address, base, token, html.EscapeString(label),
	)
}

func marketingBaseURL(app App) string {
	return strings.TrimRight(app.Settings().Meta.AppURL, "/")
}

// recordEmail resolves the address to mail a source record: the "email"
// field when present, otherwise the auth identity of the record.
func recordEmail(record *Record) string {
	if email := record.GetString("email"); email != "" {
		return email
	}
	return record.Email()
}
