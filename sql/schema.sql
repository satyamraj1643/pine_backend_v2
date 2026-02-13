-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- -----------------------------------------------------------------------------
-- 1. Users Table
-- -----------------------------------------------------------------------------
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255), -- Nullable if user only signs within via social login
    is_email_verified BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    role VARCHAR(50) NOT NULL DEFAULT 'USER',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for fast login lookups
CREATE INDEX idx_users_email ON users(email);

-- -----------------------------------------------------------------------------
-- 2. Social Accounts Table (Federated Identities)
-- -----------------------------------------------------------------------------
CREATE TABLE social_accounts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL, -- e.g., 'google', 'facebook', 'github'
    provider_id VARCHAR(255) NOT NULL, -- Unique ID from the provider
    email VARCHAR(255), -- Email stored in the social account (may match or differ from main user email)
    avatar_url TEXT,
    access_token TEXT, -- Optional: Store encrypted if acting on behalf of user
    refresh_token TEXT, -- Optional: Store encrypted if offline access needed
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Ensure a provider ID is unique for that provider (e.g. only one Google user 12345)
    UNIQUE(provider, provider_id)
);

-- Index for looking up users by social tokens
CREATE INDEX idx_social_provider ON social_accounts(provider, provider_id);

-- -----------------------------------------------------------------------------
-- 3. User Profiles Table
-- -----------------------------------------------------------------------------
CREATE TABLE user_profiles (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    nickname VARCHAR(50),
    avatar_url TEXT,
    bio TEXT,
    personality TEXT, -- Can be changed to JSONB if structured data is required
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
