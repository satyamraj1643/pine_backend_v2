-- Pine Journal — Full Schema
-- Run: psql -U postgres -h localhost -d pine -f schema.sql

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ─── Users ──────────────────────────────────────────────────
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email         VARCHAR(
        255) NOT NULL UNIQUE,
    name          VARCHAR(200) NOT NULL DEFAULT '',
    password_hash VARCHAR(255) NOT NULL,
    profile_picture TEXT,
    is_verified   BOOLEAN NOT NULL DEFAULT FALSE,
    otp_code      VARCHAR(6),
    otp_expires   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_users_email ON users(email);

-- ─── Collections (tags) ─────────────────────────────────────
CREATE TABLE collections (
    id         SERIAL PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    color      VARCHAR(30)  NOT NULL DEFAULT '#2196F3',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_collections_user ON collections(user_id);

-- ─── Moods ──────────────────────────────────────────────────
CREATE TABLE moods (
    id         SERIAL PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    emoji      VARCHAR(100) NOT NULL DEFAULT '',
    color      VARCHAR(30)  NOT NULL DEFAULT '#FF9800',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_moods_user ON moods(user_id);

-- ─── Chapters ───────────────────────────────────────────────
CREATE TABLE chapters (
    id            SERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title         VARCHAR(300) NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    color         VARCHAR(30)  NOT NULL DEFAULT '#2196F3',
    is_favourite  BOOLEAN NOT NULL DEFAULT FALSE,
    is_archived   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_chapters_user ON chapters(user_id);

-- ─── Chapter ↔ Collection (M2M) ────────────────────────────
CREATE TABLE chapter_collections (
    chapter_id    INT NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    collection_id INT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    PRIMARY KEY (chapter_id, collection_id)
);

-- ─── Entries ────────────────────────────────────────────────
CREATE TABLE entries (
    id            SERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title         VARCHAR(500) NOT NULL DEFAULT 'Untitled',
    content       TEXT NOT NULL DEFAULT '',
    chapter_id    INT REFERENCES chapters(id) ON DELETE SET NULL,
    is_favourite  BOOLEAN NOT NULL DEFAULT FALSE,
    is_archived   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_entries_user    ON entries(user_id);
CREATE INDEX idx_entries_chapter ON entries(chapter_id);

-- ─── Entry ↔ Mood (M2M) ────────────────────────────────────
CREATE TABLE entry_moods (
    entry_id INT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    mood_id  INT NOT NULL REFERENCES moods(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, mood_id)
);

-- ─── Entry ↔ Collection (M2M) ──────────────────────────────
CREATE TABLE entry_collections (
    entry_id      INT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    collection_id INT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, collection_id)
);

-- ─── Updated-at trigger ─────────────────────────────────────
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated    BEFORE UPDATE ON users    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_chapters_updated BEFORE UPDATE ON chapters FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_entries_updated  BEFORE UPDATE ON entries  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
