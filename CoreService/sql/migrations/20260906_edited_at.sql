-- Additive, rerunnable migration. Run before deploying the EditedAt-aware backend.
BEGIN;
ALTER TABLE entries ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ;
ALTER TABLE chapters ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ;
-- Keep legacy updated_at intact during the one-time backfill.
ALTER TABLE entries DISABLE TRIGGER trg_entries_updated;
ALTER TABLE chapters DISABLE TRIGGER trg_chapters_updated;
-- Historical edit time is unavailable: preserve the best existing baseline.
UPDATE entries SET edited_at = COALESCE(updated_at, created_at) WHERE edited_at IS NULL;
UPDATE chapters SET edited_at = COALESCE(updated_at, created_at) WHERE edited_at IS NULL;
ALTER TABLE entries ENABLE TRIGGER trg_entries_updated;
ALTER TABLE chapters ENABLE TRIGGER trg_chapters_updated;
ALTER TABLE entries ALTER COLUMN edited_at SET DEFAULT NOW(), ALTER COLUMN edited_at SET NOT NULL;
ALTER TABLE chapters ALTER COLUMN edited_at SET DEFAULT NOW(), ALTER COLUMN edited_at SET NOT NULL;

CREATE OR REPLACE FUNCTION track_entry_edit() RETURNS TRIGGER AS $$
BEGIN
  IF ROW(NEW.title, NEW.content, NEW.chapter_id) IS DISTINCT FROM ROW(OLD.title, OLD.content, OLD.chapter_id) THEN
    NEW.edited_at = NOW();
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION track_chapter_edit() RETURNS TRIGGER AS $$
BEGIN
  IF ROW(NEW.title, NEW.description, NEW.color) IS DISTINCT FROM ROW(OLD.title, OLD.description, OLD.color) THEN
    NEW.edited_at = NOW();
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_entries_edited ON entries;
CREATE TRIGGER trg_entries_edited BEFORE UPDATE ON entries FOR EACH ROW EXECUTE FUNCTION track_entry_edit();
DROP TRIGGER IF EXISTS trg_chapters_edited ON chapters;
CREATE TRIGGER trg_chapters_edited BEFORE UPDATE ON chapters FOR EACH ROW EXECUTE FUNCTION track_chapter_edit();

CREATE OR REPLACE FUNCTION track_entry_metadata_edit() RETURNS TRIGGER AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN UPDATE entries SET edited_at = NOW() WHERE id = OLD.entry_id; END IF;
  IF TG_OP <> 'DELETE' THEN UPDATE entries SET edited_at = NOW() WHERE id = NEW.entry_id; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION track_chapter_metadata_edit() RETURNS TRIGGER AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN UPDATE chapters SET edited_at = NOW() WHERE id = OLD.chapter_id; END IF;
  IF TG_OP <> 'DELETE' THEN UPDATE chapters SET edited_at = NOW() WHERE id = NEW.chapter_id; END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_entry_moods_edited ON entry_moods;
CREATE TRIGGER trg_entry_moods_edited AFTER INSERT OR UPDATE OR DELETE ON entry_moods FOR EACH ROW EXECUTE FUNCTION track_entry_metadata_edit();
DROP TRIGGER IF EXISTS trg_entry_tags_edited ON entry_collections;
CREATE TRIGGER trg_entry_tags_edited AFTER INSERT OR UPDATE OR DELETE ON entry_collections FOR EACH ROW EXECUTE FUNCTION track_entry_metadata_edit();
DROP TRIGGER IF EXISTS trg_chapter_tags_edited ON chapter_collections;
CREATE TRIGGER trg_chapter_tags_edited AFTER INSERT OR UPDATE OR DELETE ON chapter_collections FOR EACH ROW EXECUTE FUNCTION track_chapter_metadata_edit();
COMMIT;
