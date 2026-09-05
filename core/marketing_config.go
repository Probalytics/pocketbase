package core

import (
	"encoding/json"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/ozzo-validation/v4/is"
)

// MarketingConfig holds the settings for the marketing email subsystem.
type MarketingConfig struct {
	// Enabled turns the background sender on. When false, queued
	// messages stay put and no marketing email leaves the server.
	Enabled bool `form:"enabled" json:"enabled"`

	// RatePerMinute caps how many queued messages are sent each minute.
	RatePerMinute int `form:"ratePerMinute" json:"ratePerMinute"`

	// MaxAttempts is how many times a failing message is retried
	// before it is marked as failed.
	MaxAttempts int `form:"maxAttempts" json:"maxAttempts"`

	// TrackOpens embeds a tracking pixel to record when a message is opened.
	TrackOpens bool `form:"trackOpens" json:"trackOpens"`

	// TrackClicks rewrites links to record when a recipient clicks through.
	TrackClicks bool `form:"trackClicks" json:"trackClicks"`

	// UnsubscribeText is the label of the unsubscribe link appended to every message.
	UnsubscribeText string `form:"unsubscribeText" json:"unsubscribeText"`

	// MailingAddress is the physical postal address shown in the footer
	// to satisfy anti-spam regulations such as CAN-SPAM.
	MailingAddress string `form:"mailingAddress" json:"mailingAddress"`

	// ReplyTo is an optional Reply-To address set on every outgoing
	// marketing message.
	ReplyTo string `form:"replyTo" json:"replyTo"`

	// IMAP configures the inbound mailbox polling.
	IMAP IMAPConfig `form:"imap" json:"imap"`
}

// Validate makes MarketingConfig validatable by implementing [validation.Validatable] interface.
func (c MarketingConfig) Validate() error {
	return validation.ValidateStruct(&c,
		validation.Field(&c.RatePerMinute, validation.Min(0)),
		validation.Field(&c.MaxAttempts, validation.Min(0)),
		validation.Field(&c.ReplyTo, is.EmailFormat),
		validation.Field(&c.IMAP),
	)
}

// IMAPConfig holds the connection settings of the inbound IMAP mailbox.
type IMAPConfig struct {
	// @todo temp workaround to avoid introducing breaking changes;
	// consider refactoring and/or normalizing with the other Settings sensitive fields
	//
	// hidePassword specifies whether to hide the password field from the struct JSON serialization.
	hidePassword bool

	Enabled  bool   `form:"enabled" json:"enabled"`
	Host     string `form:"host" json:"host"`
	Port     int    `form:"port" json:"port"`
	Username string `form:"username" json:"username"`
	Password string `form:"password" json:"password"`

	// Whether to connect with implicit TLS.
	//
	// When set to false the connection starts plain and a StartTLS
	// upgrade is attempted if the server supports it.
	TLS bool `form:"tls" json:"tls"`

	// Mailbox is the mailbox to poll (defaults to "INBOX" when empty).
	Mailbox string `form:"mailbox" json:"mailbox"`
}

// Validate makes IMAPConfig validatable by implementing [validation.Validatable] interface.
func (c IMAPConfig) Validate() error {
	return validation.ValidateStruct(&c,
		validation.Field(
			&c.Host,
			validation.When(c.Enabled, validation.Required),
			is.Host,
		),
		validation.Field(
			&c.Port,
			validation.When(c.Enabled, validation.Required),
			validation.Min(0),
		),
	)
}

// MarshalJSON implements the [json.Marshaler] interface.
func (c IMAPConfig) MarshalJSON() ([]byte, error) {
	type alias IMAPConfig

	if c.hidePassword {
		v := struct {
			alias
			Password string `json:"password,omitempty"`
		}{alias(c), ""}

		return json.Marshal(v)
	}

	return json.Marshal(alias(c))
}
