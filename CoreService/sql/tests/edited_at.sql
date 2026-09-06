-- Run after schema.sql or migration. All fixtures are rolled back.
BEGIN;
DO $$
DECLARE
  uid uuid;
  eid int;
  cid int;
  baseline timestamptz := '2020-01-01';
  stamp timestamptz;
BEGIN
  INSERT INTO users(email, password_hash) VALUES (uuid_generate_v4()::text || '@example.invalid', 'test') RETURNING id INTO uid;
  INSERT INTO chapters(user_id, title) VALUES (uid, 'Test') RETURNING id INTO cid;
  INSERT INTO entries(user_id, title, chapter_id) VALUES (uid, 'Test', cid) RETURNING id INTO eid;
  UPDATE entries SET edited_at = baseline WHERE id = eid;
  UPDATE chapters SET edited_at = baseline WHERE id = cid;
  UPDATE entries SET is_favourite = true, is_archived = true WHERE id = eid;
  UPDATE chapters SET is_favourite = true, is_archived = true WHERE id = cid;
  SELECT edited_at INTO stamp FROM entries WHERE id = eid;
  IF stamp <> baseline THEN RAISE EXCEPTION 'Entry activity changed edit time'; END IF;
  SELECT edited_at INTO stamp FROM chapters WHERE id = cid;
  IF stamp <> baseline THEN RAISE EXCEPTION 'Collection activity changed edit time'; END IF;
  UPDATE entries SET title = title, content = content WHERE id = eid;
  SELECT edited_at INTO stamp FROM entries WHERE id = eid;
  IF stamp <> baseline THEN RAISE EXCEPTION 'No-op changed edit time'; END IF;
  UPDATE entries SET content = 'Edited' WHERE id = eid;
  SELECT edited_at INTO stamp FROM entries WHERE id = eid;
  IF stamp <= baseline THEN RAISE EXCEPTION 'Content edit not tracked'; END IF;
  UPDATE chapters SET description = 'Edited' WHERE id = cid;
  SELECT edited_at INTO stamp FROM chapters WHERE id = cid;
  IF stamp <= baseline THEN RAISE EXCEPTION 'Collection edit not tracked'; END IF;
END;
$$;
ROLLBACK;
