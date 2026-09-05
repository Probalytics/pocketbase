package migrations

import (
	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		return createMailInboxCollection(txApp)
	}, func(txApp core.App) error {
		col, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailInbox)
		if err != nil {
			return nil
		}

		return txApp.Delete(col)
	})
}

func createMailInboxCollection(txApp core.App) error {
	col := core.NewBaseCollection(core.CollectionNameMailInbox)
	col.System = true

	col.Fields.Add(&core.EmailField{Name: "from", System: true, Required: true})
	col.Fields.Add(&core.TextField{Name: "to", System: true})
	col.Fields.Add(&core.TextField{Name: "subject", System: true})
	col.Fields.Add(&core.EditorField{Name: "body", System: true, MaxSize: 5_000_000})
	col.Fields.Add(&core.TextField{Name: "messageId", System: true})
	col.Fields.Add(&core.DateField{Name: "receivedAt", System: true})
	col.Fields.Add(&core.BoolField{Name: "read", System: true})
	addTimestamps(col)

	col.AddIndex("idx_mailInbox_messageId", true, "messageId", "")
	col.AddIndex("idx_mailInbox_from", false, "`from`", "")

	return txApp.Save(col)
}
