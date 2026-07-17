package migrations

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// note: the stock users collection is created with the default auth options,
// i.e. AuthRule = types.Pointer("") ("allow any record to authenticate").
// The down migration restores that default.
const defaultUsersAuthRule = ""

// workosUsersAuthRule blocks suspended (deprovisioned) users from
// authenticating.
//
// The field is deliberately named "suspended" (instead of e.g. "active")
// so that its zero value corresponds to the common case: BoolField columns
// are created as "BOOLEAN DEFAULT FALSE NOT NULL" and PocketBase has no
// field default-value mechanism, meaning every record-creation path would
// otherwise have to remember to opt users in. With the inverted semantics
// newly created records can authenticate by default and only an explicit
// suspended=true (e.g. from a WorkOS Directory Sync deprovision event)
// blocks them.
const workosUsersAuthRule = "suspended = false"

func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		// 1. create the organizations base collection (if it doesn't exist already)
		// ---------------------------------------------------------------
		organizations, err := txApp.FindCollectionByNameOrId("organizations")
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if organizations == nil {
			organizations = core.NewBaseCollection("organizations")

			// members can read only their own organization;
			// create/update/delete are left nil (superusers only)
			memberRule := "@request.auth.organization = id"
			organizations.ListRule = types.Pointer(memberRule)
			organizations.ViewRule = types.Pointer(memberRule)

			organizations.Fields.Add(&core.TextField{
				Name: "name",
				Max:  255,
			})
			organizations.Fields.Add(&core.TextField{
				Name:     "workosOrgId",
				Required: true,
			})
			organizations.Fields.Add(&core.JSONField{
				Name:    "domains",
				MaxSize: 2000, // array of verified email domains used for SSO routing
			})
			organizations.Fields.Add(&core.AutodateField{
				Name:     "created",
				OnCreate: true,
			})
			organizations.Fields.Add(&core.AutodateField{
				Name:     "updated",
				OnCreate: true,
				OnUpdate: true,
			})

			organizations.AddIndex("idx_organizations_workosOrgId", true, "workosOrgId", "")

			if err = txApp.Save(organizations); err != nil {
				return err
			}
		}

		// 2. extend the users auth collection (if it still exists)
		// ---------------------------------------------------------------
		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // renamed or deleted -> nothing to do
			}
			return err
		}

		if users.Fields.GetByName("organization") == nil {
			users.Fields.Add(&core.RelationField{
				Name:          "organization",
				CollectionId:  organizations.Id,
				MaxSelect:     1,
				CascadeDelete: false,
			})
		}

		if users.Fields.GetByName("workosUserId") == nil {
			users.Fields.Add(&core.TextField{
				Name:   "workosUserId",
				Hidden: true,
			})
		}

		if users.Fields.GetByName("suspended") == nil {
			users.Fields.Add(&core.BoolField{
				Name: "suspended",
			})
		}

		// combine the previous "allow any" semantics with the suspended check
		// (only when the collection still uses the stock default rule)
		if users.AuthRule == nil || *users.AuthRule == defaultUsersAuthRule {
			users.AuthRule = types.Pointer(workosUsersAuthRule)
		}

		// Changing the AuthRule normally rotates AuthToken.Secret to
		// invalidate previously issued auth tokens (see the collection
		// save hook in core/collection_model.go). Since existing users
		// records default to suspended=false, their sessions remain valid
		// under the new rule and there is no reason to force a global
		// logout on upgrade -> preserve the original secret.
		originalAuthTokenSecret := users.AuthToken.Secret

		if err = txApp.Save(users); err != nil {
			return err
		}

		if users.AuthToken.Secret != originalAuthTokenSecret {
			users.AuthToken.Secret = originalAuthTokenSecret
			if err = txApp.Save(users); err != nil {
				return err
			}
		}

		return nil
	}, func(txApp core.App) error {
		// remove the extra users fields and restore the default auth rule
		// ---------------------------------------------------------------
		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if users != nil {
			users.Fields.RemoveByName("organization")
			users.Fields.RemoveByName("workosUserId")
			users.Fields.RemoveByName("suspended")

			if users.AuthRule != nil && *users.AuthRule == workosUsersAuthRule {
				users.AuthRule = types.Pointer(defaultUsersAuthRule)
			}

			// preserve the auth token secret on rule revert
			// (see the note in the up migration)
			originalAuthTokenSecret := users.AuthToken.Secret

			if err = txApp.Save(users); err != nil {
				return err
			}

			if users.AuthToken.Secret != originalAuthTokenSecret {
				users.AuthToken.Secret = originalAuthTokenSecret
				if err = txApp.Save(users); err != nil {
					return err
				}
			}
		}

		// delete the organizations collection
		// ---------------------------------------------------------------
		organizations, err := txApp.FindCollectionByNameOrId("organizations")
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if organizations != nil {
			if err = txApp.Delete(organizations); err != nil {
				return err
			}
		}

		return nil
	})
}
