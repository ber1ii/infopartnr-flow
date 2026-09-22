-- Infopartnr Flow schema.
-- Dev: `docker compose down -v && docker compose up -d` re-applies this file.
-- Before prod: move to goose migrations.
-- Money is stored as integer cents. Refunds are negative amounts.

-- ===== Tenancy & auth =====

CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    contact_email TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'UTC',
    currency TEXT NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX clients_workspace_idx ON clients (workspace_id);

-- role 'client' users are pinned to exactly one client; owner/agency_admin see the whole workspace.
CREATE TABLE memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'agency_admin', 'client')),
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id),
    CHECK ((role = 'client') = (client_id IS NOT NULL))
);

CREATE TABLE invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== YouTube =====

CREATE TABLE youtube_channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    google_channel_id TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL DEFAULT '',
    thumbnail_url TEXT NOT NULL DEFAULT '',
    refresh_token_enc BYTEA,                 -- AES-GCM encrypted, never plaintext
    status TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'revoked')),
    connected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_synced_at TIMESTAMPTZ
);
CREATE INDEX youtube_channels_client_idx ON youtube_channels (client_id);

CREATE TABLE videos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    channel_id UUID REFERENCES youtube_channels(id) ON DELETE SET NULL,
    youtube_video_id TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL DEFAULT '',
    thumbnail_url TEXT NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX videos_client_idx ON videos (client_id);

-- YouTube Analytics API data, one row per video per day.
CREATE TABLE video_stats_daily (
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    views BIGINT NOT NULL DEFAULT 0,
    watch_minutes BIGINT NOT NULL DEFAULT 0,
    subs_gained INTEGER NOT NULL DEFAULT 0,
    impressions BIGINT NOT NULL DEFAULT 0,
    ctr_bps INTEGER NOT NULL DEFAULT 0,      -- click-through rate in basis points
    PRIMARY KEY (video_id, day)
);

CREATE TABLE video_costs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('production', 'ad_spend', 'other')),
    amount_cents BIGINT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    incurred_on DATE NOT NULL DEFAULT CURRENT_DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX video_costs_video_idx ON video_costs (video_id);

-- ===== Links & tracking =====

CREATE TABLE domains (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    hostname TEXT NOT NULL UNIQUE,           -- e.g. go.clientbrand.com
    verified BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tracking_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    video_id UUID REFERENCES videos(id) ON DELETE SET NULL,
    domain_id UUID REFERENCES domains(id) ON DELETE SET NULL,  -- NULL = default platform domain
    slug TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    target_url TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Same slug may exist on different domains.
CREATE UNIQUE INDEX tracking_links_domain_slug_idx
    ON tracking_links (COALESCE(domain_id, '00000000-0000-0000-0000-000000000000'::uuid), slug);
CREATE INDEX tracking_links_client_idx ON tracking_links (client_id);
CREATE INDEX tracking_links_video_idx ON tracking_links (video_id);

-- A/B destinations. If a link has active variants, traffic is split by weight.
CREATE TABLE link_variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    link_id UUID NOT NULL REFERENCES tracking_links(id) ON DELETE CASCADE,
    target_url TEXT NOT NULL,
    weight INTEGER NOT NULL DEFAULT 1 CHECK (weight > 0),
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX link_variants_link_idx ON link_variants (link_id);

CREATE TABLE clicks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    link_id UUID NOT NULL REFERENCES tracking_links(id) ON DELETE CASCADE,
    variant_id UUID REFERENCES link_variants(id) ON DELETE SET NULL,
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,  -- denormalized for fast scoping
    video_id UUID REFERENCES videos(id) ON DELETE SET NULL,            -- denormalized for rollups
    trakyo_id TEXT NOT NULL UNIQUE,
    ip_hash TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    referrer TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    device_type TEXT NOT NULL DEFAULT '',    -- desktop / mobile / tablet
    browser TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    is_bot BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX clicks_client_time_idx ON clicks (client_id, created_at);
CREATE INDEX clicks_link_time_idx ON clicks (link_id, created_at);
CREATE INDEX clicks_video_time_idx ON clicks (video_id, created_at);

-- Email <-> trakyo_id map for cross-device attribution fallback.
CREATE TABLE identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    trakyo_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (client_id, email, trakyo_id)
);
CREATE INDEX identities_email_idx ON identities (client_id, email);

-- ===== Revenue =====

CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,               -- Stripe subscription id
    customer_email TEXT NOT NULL DEFAULT '',
    click_id BIGINT REFERENCES clicks(id) ON DELETE SET NULL,
    link_id UUID REFERENCES tracking_links(id) ON DELETE SET NULL,
    video_id UUID REFERENCES videos(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'active',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    canceled_at TIMESTAMPTZ,
    UNIQUE (client_id, external_id)
);

CREATE TABLE conversions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    click_id BIGINT REFERENCES clicks(id) ON DELETE SET NULL,
    link_id UUID REFERENCES tracking_links(id) ON DELETE SET NULL,
    video_id UUID REFERENCES videos(id) ON DELETE SET NULL,
    subscription_id UUID REFERENCES subscriptions(id) ON DELETE SET NULL,
    trakyo_id TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL CHECK (source IN ('stripe', 'calendly', 'typeform', 'manual')),
    event_type TEXT NOT NULL CHECK (event_type IN
        ('purchase', 'subscription_start', 'subscription_renewal', 'booked_call', 'lead', 'refund')),
    external_id TEXT NOT NULL,               -- provider event/object id, used for idempotency
    amount_cents BIGINT NOT NULL DEFAULT 0,  -- negative for refunds
    currency TEXT NOT NULL DEFAULT 'USD',
    email TEXT NOT NULL DEFAULT '',
    attribution_method TEXT NOT NULL DEFAULT 'none' CHECK (attribution_method IN ('track_id', 'email', 'none')),
    raw JSONB,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (client_id, source, external_id)
);
CREATE INDEX conversions_client_time_idx ON conversions (client_id, occurred_at);
CREATE INDEX conversions_video_time_idx ON conversions (video_id, occurred_at);
CREATE INDEX conversions_email_idx ON conversions (client_id, email);

-- ===== Integrations, alerts, sharing =====

CREATE TABLE integrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),   -- used in webhook URL: /webhooks/stripe/{id}
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('stripe', 'calendly', 'typeform')),
    webhook_secret_enc BYTEA,
    config JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (client_id, provider)
);

CREATE TABLE notification_channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,   -- NULL = all clients
    kind TEXT NOT NULL CHECK (kind IN ('slack', 'discord')),
    webhook_url TEXT NOT NULL,
    min_amount_cents BIGINT NOT NULL DEFAULT 0,
    events TEXT[] NOT NULL DEFAULT '{purchase,booked_call}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE report_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- YouTube Analytics API data, one row per channel per day. Channel-level
-- totals (subscriber deltas, etc.) are fetched via a separate channel-level
-- Analytics API call per sync -- never derive these by summing video_stats_daily.
CREATE TABLE channel_stats_daily (
    channel_id UUID NOT NULL REFERENCES youtube_channels(id) ON DELETE CASCADE,
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,  -- denormalized for fast scoping
    day DATE NOT NULL,
    views BIGINT NOT NULL DEFAULT 0,
    watch_minutes BIGINT NOT NULL DEFAULT 0,
    subs_gained INTEGER NOT NULL DEFAULT 0,
    subs_lost INTEGER NOT NULL DEFAULT 0,
    likes BIGINT NOT NULL DEFAULT 0,
    comments BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, day)
);
CREATE INDEX channel_stats_daily_client_day_idx ON channel_stats_daily (client_id, day);