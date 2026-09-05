package migrations

import (
	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		if err := createMailSegmentsCollection(txApp); err != nil {
			return err
		}

		return addCampaignSegmentField(txApp)
	}, func(txApp core.App) error {
		if err := removeCampaignSegmentField(txApp); err != nil {
			return err
		}

		col, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailSegments)
		if err != nil {
			return nil
		}

		return txApp.Delete(col)
	})
}

func createMailSegmentsCollection(txApp core.App) error {
	col := core.NewBaseCollection(core.CollectionNameMailSegments)
	col.System = true

	col.Fields.Add(&core.TextField{Name: "name", System: true, Required: true})
	col.Fields.Add(&core.TextField{Name: "collection", System: true, Required: true})
	col.Fields.Add(&core.TextField{Name: "filter", System: true, Max: 2000})
	addTimestamps(col)

	col.AddIndex("idx_mailSegments_name", true, "name", "")

	return txApp.Save(col)
}

func addCampaignSegmentField(txApp core.App) error {
	segments, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailSegments)
	if err != nil {
		return err
	}

	col, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailCampaigns)
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{Name: "segment", System: true, CollectionId: segments.Id, MaxSelect: 1})

	return txApp.Save(col)
}

func removeCampaignSegmentField(txApp core.App) error {
	col, err := txApp.FindCollectionByNameOrId(core.CollectionNameMailCampaigns)
	if err != nil {
		return nil
	}

	col.Fields.RemoveByName("segment")

	return txApp.Save(col)
}
