package core

import (
	validation "github.com/pocketbase/ozzo-validation/v4"
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
}

// Validate makes MarketingConfig validatable by implementing [validation.Validatable] interface.
func (c MarketingConfig) Validate() error {
	return validation.ValidateStruct(&c,
		validation.Field(&c.RatePerMinute, validation.Min(0)),
		validation.Field(&c.MaxAttempts, validation.Min(0)),
	)
}
