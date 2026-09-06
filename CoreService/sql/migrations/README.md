# Separate edit time from activity time

Rollout order:

1. Back up the database, then run `20260906_edited_at.sql` against the existing database as its owner.
2. Deploy CoreService.
3. Deploy the frontend.

The migration is transactional and rerunnable. It adds columns/triggers without removing or redefining UpdatedAt. Existing timestamps are copied as a best-effort historical baseline; exact historical edit times cannot be recovered from mixed activity timestamps.

The existing schema's named updated-at triggers are temporarily disabled only during backfill, inside the transaction, so backfill does not overwrite UpdatedAt. Table locks protect that step. Schedule this migration appropriately for database size.

Old backend versions continue to work after migration: database triggers maintain EditedAt. Old clients can ignore the additional JSON field. New frontend versions fall back to UpdatedAt when talking to an old backend. New backend reads also tolerate the old schema: they extract EditedAt through the row JSON and fall back to UpdatedAt. Until migration is applied, recency retains its legacy activity-time semantics rather than failing. Migration-first remains the recommended order.

Title/content, collection details, entry collection membership, and mood/tag associations count as edits. Favorite/archive toggles (including bulk operations) do not. Association replacement by an older client counts as an edit even if it deletes/reinserts the same links. UpdatedAt still tracks general activity.

The backend uses versioned read-cache keys to avoid serving old JSON without EditedAt after deployment. Existing prefix invalidation still applies.

Run `../tests/edited_at.sql` on a disposable database initialized from schema.sql to exercise timestamp semantics. It rolls back its fixture data. This migration and SQL test have not been run against production.
