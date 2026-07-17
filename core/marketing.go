package core

// Marketing system collection names.
const (
	CollectionNameMailTemplates    = "_mailTemplates"
	CollectionNameMailSuppressions = "_mailSuppressions"
	CollectionNameMailCampaigns    = "_mailCampaigns"
	CollectionNameMailAutomations  = "_mailAutomations"
	CollectionNameMailEnrollments  = "_mailEnrollments"
	CollectionNameMailMessages     = "_mailMessages"
	CollectionNameMailSegments     = "_mailSegments"
	CollectionNameMailInbox        = "_mailInbox"
)

// Delivery states of a single queued email (a _mailMessages record).
const (
	MailMessageStatusQueued   = "queued"
	MailMessageStatusSending  = "sending"
	MailMessageStatusSent     = "sent"
	MailMessageStatusFailed   = "failed"
	MailMessageStatusBounced  = "bounced"
	MailMessageStatusCanceled = "canceled"
)

// Lifecycle states of a broadcast (a _mailCampaigns record).
const (
	MailCampaignStatusDraft     = "draft"
	MailCampaignStatusScheduled = "scheduled"
	MailCampaignStatusSending   = "sending"
	MailCampaignStatusSent      = "sent"
	MailCampaignStatusPaused    = "paused"
	MailCampaignStatusFailed    = "failed"
	MailCampaignStatusCanceled  = "canceled"
)

// Progress states of a record moving through an automation (a _mailEnrollments record).
const (
	MailEnrollmentStatusActive    = "active"
	MailEnrollmentStatusCompleted = "completed"
	MailEnrollmentStatusExited    = "exited"
	MailEnrollmentStatusFailed    = "failed"
)

// Reasons an address lands on the global suppression list (a _mailSuppressions record).
const (
	MailSuppressionReasonUnsubscribe = "unsubscribe"
	MailSuppressionReasonBounce      = "bounce"
	MailSuppressionReasonComplaint   = "complaint"
	MailSuppressionReasonManual      = "manual"
)

// Automation trigger events and step kinds.
const (
	MailTriggerEventCreate = "create"
	MailTriggerEventUpdate = "update"

	MailStepKindEmail = "email"
	MailStepKindDelay = "delay"
)
