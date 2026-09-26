-- ===== Redirect engine =====

-- name: GetLinkForRedirect :one
SELECT id, client_id, video_id, target_url, is_active, expires_at
FROM tracking_links
WHERE slug = $1 AND domain_id IS NULL
LIMIT 1;

-- name: ListActiveVariants :many
SELECT id, target_url, weight
FROM link_variants
WHERE link_id = $1 AND is_active
ORDER BY created_at;

-- Bulk insert for the async batch writer (pgx CopyFrom).
-- name: InsertClicks :copyfrom
INSERT INTO clicks (
    link_id, variant_id, client_id, video_id, trakyo_id, ip_hash,
    user_agent, referrer, country, device_type, browser, os, is_bot
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: GetClickByTrakyoID :one
SELECT id, link_id, client_id, video_id, trakyo_id, created_at
FROM clicks
WHERE trakyo_id = $1;

-- ===== Auth =====

-- name: CreateUser :one
INSERT INTO users (email, password_hash, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CountUsers :one
SELECT COUNT(*)::bigint FROM users;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateWorkspace :one
INSERT INTO workspaces (name) VALUES ($1) RETURNING *;

-- name: CreateMembership :one
INSERT INTO memberships (workspace_id, user_id, role, client_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListMembershipsByUser :many
SELECT m.id, m.workspace_id, m.role, m.client_id, w.name AS workspace_name
FROM memberships m
JOIN workspaces w ON w.id = m.workspace_id
WHERE m.user_id = $1;

-- ===== Invitations =====

-- name: CreateInvitation :one
INSERT INTO invitations (workspace_id, client_id, email, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInvitationByTokenHash :one
SELECT i.id, i.workspace_id, i.client_id, i.email, i.expires_at, i.accepted_at, c.name AS client_name
FROM invitations i
JOIN clients c ON c.id = i.client_id
WHERE i.token_hash = $1;

-- Returns 0 rows affected if already accepted -> prevents double-accept races.
-- name: MarkInvitationAccepted :execrows
UPDATE invitations SET accepted_at = now() WHERE id = $1 AND accepted_at IS NULL;

-- name: ListInvitationsByClient :many
SELECT id, email, expires_at, accepted_at, created_at
FROM invitations
WHERE client_id = $1
ORDER BY created_at DESC;

-- ===== Clients =====

-- name: CreateClient :one
INSERT INTO clients (workspace_id, name, contact_email, timezone, currency)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetClient :one
SELECT * FROM clients WHERE id = $1 AND workspace_id = $2;

-- name: ListClientsByWorkspace :many
SELECT * FROM clients WHERE workspace_id = $1 ORDER BY name;

-- name: ListChannelsByClient :many
SELECT id, client_id, google_channel_id, title, thumbnail_url, status, connected_at, last_synced_at
FROM youtube_channels
WHERE client_id = $1;

-- ===== Videos =====

-- name: ListVideosByClient :many
SELECT * FROM videos WHERE client_id = $1 ORDER BY published_at DESC NULLS LAST;

-- name: GetVideo :one
SELECT * FROM videos WHERE id = $1 AND client_id = $2;

-- name: AddVideoCost :one
INSERT INTO video_costs (video_id, kind, amount_cents, note, incurred_on)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- ===== Links (always pass client_id so a client can't touch another's links) =====

-- name: CreateLink :one
INSERT INTO tracking_links (client_id, video_id, domain_id, slug, name, target_url, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListLinksByClient :many
SELECT * FROM tracking_links WHERE client_id = $1 ORDER BY created_at DESC;

-- name: UpdateLink :one
UPDATE tracking_links
SET name       = COALESCE(sqlc.narg('name'), name),
    target_url = COALESCE(sqlc.narg('target_url'), target_url),
    is_active  = COALESCE(sqlc.narg('is_active'), is_active),
    video_id   = COALESCE(sqlc.narg('video_id'), video_id),
    updated_at = now()
WHERE id = sqlc.arg('id') AND client_id = sqlc.arg('client_id')
RETURNING *;

-- name: DeleteLink :one
DELETE FROM tracking_links WHERE id = $1 AND client_id = $2 RETURNING slug;

-- name: AddLinkVariant :one
INSERT INTO link_variants (link_id, target_url, weight)
VALUES ($1, $2, $3)
RETURNING *;

-- ===== A/B variants =====

-- name: GetLinkByID :one
SELECT * FROM tracking_links WHERE id = $1 AND client_id = $2;

-- name: CountLinkVariants :one
SELECT COUNT(*)::bigint FROM link_variants WHERE link_id = $1;

-- Per-variant clicks (bots excluded) + conversions/revenue via the click each
-- conversion was attributed to.
-- name: ListVariantsWithStats :many
SELECT
    lv.id, lv.link_id, lv.target_url, lv.weight, lv.is_active, lv.created_at,
    COALESCE(kc.clicks, 0)::bigint AS clicks,
    COALESCE(cvs.conversions, 0)::bigint AS conversions,
    COALESCE(cvs.revenue_cents, 0)::bigint AS revenue_cents
FROM link_variants lv
LEFT JOIN (
    SELECT k.variant_id, COUNT(*) AS clicks
    FROM clicks k
    WHERE k.link_id = sqlc.arg(link_id) AND k.variant_id IS NOT NULL AND NOT k.is_bot
    GROUP BY k.variant_id
) kc ON kc.variant_id = lv.id
LEFT JOIN (
    SELECT k.variant_id,
           COUNT(*) FILTER (WHERE cv.event_type <> 'refund') AS conversions,
           SUM(cv.amount_cents) AS revenue_cents
    FROM conversions cv
    JOIN clicks k ON k.id = cv.click_id
    WHERE k.link_id = sqlc.arg(link_id) AND k.variant_id IS NOT NULL
    GROUP BY k.variant_id
) cvs ON cvs.variant_id = lv.id
WHERE lv.link_id = sqlc.arg(link_id)
ORDER BY lv.created_at;

-- Target URL is intentionally immutable: editing it mid-test would mix data.
-- Caller must verify link ownership (GetLinkByID) first.
-- name: UpdateLinkVariant :one
UPDATE link_variants
SET weight    = COALESCE(sqlc.narg('weight'), weight),
    is_active = COALESCE(sqlc.narg('is_active'), is_active)
WHERE id = sqlc.arg('id') AND link_id = sqlc.arg('link_id')
RETURNING *;

-- name: DeleteLinkVariant :execrows
DELETE FROM link_variants WHERE id = $1 AND link_id = $2;

-- ===== Conversions =====

-- Returns no row (pgx.ErrNoRows) if the event was already ingested -> idempotent.
-- name: InsertConversion :one
INSERT INTO conversions (
    client_id, click_id, link_id, video_id, subscription_id, trakyo_id, source,
    event_type, external_id, amount_cents, currency, email, attribution_method, raw, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (client_id, source, external_id) DO NOTHING
RETURNING id;

-- Email fallback: most recent click linked to this email.
-- name: FindClickIDByEmail :one
SELECT c.id, c.link_id, c.video_id, c.trakyo_id
FROM identities i
JOIN clicks c ON c.trakyo_id = i.trakyo_id
WHERE i.client_id = $1 AND i.email = $2
ORDER BY c.created_at DESC
LIMIT 1;

-- ===== Analytics =====
-- Columns are table-qualified everywhere: sqlc reports "ambiguous" otherwise.

-- name: GetClientOverview :one
SELECT
    (SELECT COUNT(*) FROM clicks k
      WHERE k.client_id = sqlc.arg(client_id) AND NOT k.is_bot
        AND k.created_at >= sqlc.arg(from_ts) AND k.created_at < sqlc.arg(to_ts))::bigint AS clicks,
    (SELECT COUNT(DISTINCT k.ip_hash) FROM clicks k
      WHERE k.client_id = sqlc.arg(client_id) AND NOT k.is_bot
        AND k.created_at >= sqlc.arg(from_ts) AND k.created_at < sqlc.arg(to_ts))::bigint AS unique_visitors,
    (SELECT COUNT(*) FROM conversions cv
      WHERE cv.client_id = sqlc.arg(client_id) AND cv.event_type <> 'refund'
        AND cv.occurred_at >= sqlc.arg(from_ts) AND cv.occurred_at < sqlc.arg(to_ts))::bigint AS conversions,
    (SELECT COALESCE(SUM(cv.amount_cents), 0) FROM conversions cv
      WHERE cv.client_id = sqlc.arg(client_id)
        AND cv.occurred_at >= sqlc.arg(from_ts) AND cv.occurred_at < sqlc.arg(to_ts))::bigint AS revenue_cents,
    (SELECT COALESCE(SUM(vc.amount_cents), 0) FROM video_costs vc
      JOIN videos v ON v.id = vc.video_id
      WHERE v.client_id = sqlc.arg(client_id)
        AND vc.incurred_on >= sqlc.arg(from_ts)::date AND vc.incurred_on < sqlc.arg(to_ts)::date)::bigint AS cost_cents,
    (SELECT COALESCE(SUM(vc.amount_cents), 0) FROM video_costs vc
      JOIN videos v ON v.id = vc.video_id
      WHERE v.client_id = sqlc.arg(client_id))::bigint AS lifetime_cost_cents;

-- name: ClicksByDay :many
SELECT date_trunc('day', k.created_at)::timestamptz AS day, COUNT(*)::bigint AS clicks
FROM clicks k
WHERE k.client_id = sqlc.arg(client_id) AND NOT k.is_bot
  AND k.created_at >= sqlc.arg(from_ts) AND k.created_at < sqlc.arg(to_ts)
GROUP BY 1
ORDER BY 1;

-- name: VideoLeaderboard :many
SELECT
    v.id, v.title, v.youtube_video_id, v.thumbnail_url,
    COALESCE(cl.clicks, 0)::bigint AS clicks,
    COALESCE(cvs.conversions, 0)::bigint AS conversions,
    COALESCE(cvs.revenue_cents, 0)::bigint AS revenue_cents,
    COALESCE(co.cost_cents, 0)::bigint AS cost_cents
FROM videos v
LEFT JOIN (
    SELECT k.video_id, COUNT(*) AS clicks
    FROM clicks k
    WHERE k.client_id = sqlc.arg(client_id) AND NOT k.is_bot
      AND k.created_at >= sqlc.arg(from_ts) AND k.created_at < sqlc.arg(to_ts)
    GROUP BY k.video_id
) cl ON cl.video_id = v.id
LEFT JOIN (
    SELECT cv.video_id,
           COUNT(*) FILTER (WHERE cv.event_type <> 'refund') AS conversions,
           SUM(cv.amount_cents) AS revenue_cents
    FROM conversions cv
    WHERE cv.client_id = sqlc.arg(client_id)
      AND cv.occurred_at >= sqlc.arg(from_ts) AND cv.occurred_at < sqlc.arg(to_ts)
    GROUP BY cv.video_id
) cvs ON cvs.video_id = v.id
LEFT JOIN (
    SELECT vc.video_id, SUM(vc.amount_cents) AS cost_cents
    FROM video_costs vc
    GROUP BY vc.video_id
) co ON co.video_id = v.id
WHERE v.client_id = sqlc.arg(client_id)
ORDER BY revenue_cents DESC, clicks DESC
LIMIT sqlc.arg(row_limit);
-- ===== Integrations =====

-- name: ListIntegrationsByClient :many
SELECT i.id, i.provider, i.is_active, (i.webhook_secret_enc IS NOT NULL)::boolean AS has_secret, i.created_at
FROM integrations i
WHERE i.client_id = $1
ORDER BY i.created_at;

-- name: CreateIntegration :one
INSERT INTO integrations (client_id, provider, webhook_secret_enc)
VALUES ($1, $2, $3)
RETURNING id, provider, is_active, (webhook_secret_enc IS NOT NULL)::boolean AS has_secret, created_at;

-- name: SetIntegrationSecret :execrows
UPDATE integrations SET webhook_secret_enc = $3 WHERE id = $1 AND client_id = $2;

-- name: DeleteIntegration :execrows
DELETE FROM integrations WHERE id = $1 AND client_id = $2;

-- Public webhook lookup (no tenant context; the URL id is the credential, the signature is the proof).
-- name: GetIntegrationForWebhook :one
SELECT id, client_id, provider, webhook_secret_enc, is_active
FROM integrations
WHERE id = $1;

-- ===== Attribution =====

-- Tenant-scoped: a trakyo_id from another client's click must never attribute.
-- name: GetClickForClient :one
SELECT k.id, k.link_id, k.video_id, k.trakyo_id
FROM clicks k
WHERE k.trakyo_id = $1 AND k.client_id = $2;

-- name: UpsertIdentity :exec
INSERT INTO identities (client_id, email, trakyo_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- ===== Stripe subscriptions & refunds =====

-- name: UpsertSubscription :one
INSERT INTO subscriptions (client_id, external_id, customer_email, click_id, link_id, video_id, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (client_id, external_id) DO UPDATE
SET customer_email = COALESCE(NULLIF(EXCLUDED.customer_email, ''), subscriptions.customer_email),
    click_id = COALESCE(subscriptions.click_id, EXCLUDED.click_id),
    link_id  = COALESCE(subscriptions.link_id, EXCLUDED.link_id),
    video_id = COALESCE(subscriptions.video_id, EXCLUDED.video_id)
RETURNING id;

-- name: GetSubscriptionByExternalID :one
SELECT s.id, s.click_id, s.link_id, s.video_id, s.customer_email
FROM subscriptions s
WHERE s.client_id = $1 AND s.external_id = $2;

-- name: CancelSubscription :exec
UPDATE subscriptions
SET status = 'canceled', canceled_at = now()
WHERE client_id = $1 AND external_id = $2;

-- Refunds inherit attribution from the original payment (matched via the stored Stripe object).
-- name: FindConversionByPayment :one
SELECT cv.click_id, cv.link_id, cv.video_id, cv.subscription_id, cv.trakyo_id, cv.email, cv.attribution_method
FROM conversions cv
WHERE cv.client_id = sqlc.arg(client_id)
  AND cv.source = 'stripe'
  AND cv.event_type <> 'refund'
  AND (cv.raw->>'payment_intent' = sqlc.arg(payment_intent)::text
       OR cv.raw->>'charge' = sqlc.arg(charge)::text)
ORDER BY cv.occurred_at
LIMIT 1;

-- ===== Conversions list (dashboard) =====

-- name: ListConversionsByClient :many
SELECT cv.id, cv.event_type, cv.source, cv.amount_cents, cv.currency, cv.email,
       cv.attribution_method, cv.occurred_at, cv.link_id,
       COALESCE(tl.slug, '')::text AS link_slug,
       COALESCE(tl.name, '')::text AS link_name
FROM conversions cv
LEFT JOIN tracking_links tl ON tl.id = cv.link_id
WHERE cv.client_id = sqlc.arg(client_id)
ORDER BY cv.occurred_at DESC
LIMIT sqlc.arg(row_limit);

-- ===== Calendly reschedule handling =====
-- A reschedule fires a new invitee.created with a new invitee uri; we need
-- to find the conversion row for the invitee it replaced so we can update
-- it in place instead of double-counting the booking.

-- name: GetConversionBySourceExternalID :one
SELECT id, click_id, link_id, video_id, trakyo_id, email, attribution_method
FROM conversions
WHERE client_id = sqlc.arg(client_id) AND source = sqlc.arg(source) AND external_id = sqlc.arg(external_id);

-- name: UpdateConversionOnReschedule :execrows
UPDATE conversions
SET external_id = sqlc.arg(new_external_id),
    occurred_at = sqlc.arg(occurred_at),
    click_id = COALESCE(sqlc.narg(click_id), click_id),
    link_id = COALESCE(sqlc.narg(link_id), link_id),
    attribution_method = COALESCE(sqlc.narg(attribution_method), attribution_method)
WHERE id = sqlc.arg(id) AND client_id = sqlc.arg(client_id);

-- ===== Video costs =====

-- name: ListVideosWithCostsByClient :many
SELECT
    v.id, v.client_id, v.channel_id, v.youtube_video_id, v.title, v.thumbnail_url,
    v.published_at, v.created_at,
    COALESCE(SUM(vc.amount_cents), 0)::bigint AS total_cost_cents,
    COUNT(vc.id)::bigint AS cost_count
FROM videos v
LEFT JOIN video_costs vc ON vc.video_id = v.id
WHERE v.client_id = $1
GROUP BY v.id
ORDER BY v.published_at DESC NULLS LAST, v.created_at DESC;

-- name: ListVideoCostsByVideo :many
SELECT * FROM video_costs WHERE video_id = $1 ORDER BY incurred_on DESC, created_at DESC;

-- Tenant-scoped delete: joins to videos so a cost can't be deleted via a
-- guessed id unless it belongs to a video owned by this client.
-- name: DeleteVideoCost :one
DELETE FROM video_costs vc
USING videos v
WHERE vc.id = $1 AND vc.video_id = v.id AND v.client_id = $2
RETURNING vc.id;

-- ===== YouTube OAuth =====

-- One row per Google channel. Re-connecting the same channel (same
-- google_channel_id) updates it in place rather than duplicating; a client
-- can have more than one channel connected (some agencies run several
-- per client), so this is keyed on google_channel_id, not client_id.
-- name: UpsertYoutubeChannel :one
INSERT INTO youtube_channels (client_id, google_channel_id, title, thumbnail_url, refresh_token_enc, status)
VALUES ($1, $2, $3, $4, $5, 'connected')
ON CONFLICT (google_channel_id) DO UPDATE
SET client_id = EXCLUDED.client_id,
    title = EXCLUDED.title,
    thumbnail_url = EXCLUDED.thumbnail_url,
    refresh_token_enc = EXCLUDED.refresh_token_enc,
    status = 'connected',
    connected_at = now()
RETURNING *;

-- name: DeleteYoutubeChannel :execrows
DELETE FROM youtube_channels WHERE id = $1 AND client_id = $2;

-- ===== YouTube Sync =====

-- name: ListClientChannelsToSync :many
SELECT id, client_id, google_channel_id, refresh_token_enc
FROM youtube_channels
WHERE client_id = $1 AND status = 'connected' AND refresh_token_enc IS NOT NULL;

-- name: GetChannelForSync :one
SELECT id, client_id, google_channel_id, refresh_token_enc
FROM youtube_channels
WHERE id = $1 AND client_id = $2 AND status = 'connected' AND refresh_token_enc IS NOT NULL;

-- name: UpsertVideo :one
INSERT INTO videos (client_id, channel_id, youtube_video_id, title, thumbnail_url, published_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (youtube_video_id) DO UPDATE
SET title = EXCLUDED.title,
    thumbnail_url = EXCLUDED.thumbnail_url,
    channel_id = EXCLUDED.channel_id,
    published_at = COALESCE(EXCLUDED.published_at, videos.published_at)
RETURNING id;

-- name: UpsertVideoStatDaily :exec
INSERT INTO video_stats_daily (video_id, day, views, watch_minutes, subs_gained)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (video_id, day) DO UPDATE
SET views = EXCLUDED.views,
    watch_minutes = EXCLUDED.watch_minutes,
    subs_gained = EXCLUDED.subs_gained;

-- name: UpdateChannelSyncStatus :exec
UPDATE youtube_channels SET last_synced_at = now() WHERE id = $1;

-- name: MarkChannelRevoked :exec
UPDATE youtube_channels SET status = 'revoked' WHERE id = $1;

-- ===== Video analytics (per-video expansion on the Videos page) =====

-- name: ListVideoStatsDaily :many
SELECT day, views, watch_minutes, subs_gained
FROM video_stats_daily
WHERE video_id = sqlc.arg(video_id)
  AND day::timestamptz >= sqlc.arg(from_ts)::timestamptz
  AND day::timestamptz < sqlc.arg(to_ts)::timestamptz
ORDER BY day;

-- Clicks/conversions/revenue for one video in a window. Cost is deliberately
-- left out here -- it's a lifetime total already returned by
-- ListVideosWithCostsByClient, no need to compute it twice.
-- name: GetVideoOverview :one
SELECT
    (SELECT COUNT(*) FROM clicks k
      WHERE k.video_id = sqlc.arg(video_id) AND NOT k.is_bot
        AND k.created_at >= sqlc.arg(from_ts) AND k.created_at < sqlc.arg(to_ts))::bigint AS clicks,
    (SELECT COUNT(*) FROM conversions cv
      WHERE cv.video_id = sqlc.arg(video_id) AND cv.event_type <> 'refund'
        AND cv.occurred_at >= sqlc.arg(from_ts) AND cv.occurred_at < sqlc.arg(to_ts))::bigint AS conversions,
    (SELECT COALESCE(SUM(cv.amount_cents), 0) FROM conversions cv
      WHERE cv.video_id = sqlc.arg(video_id)
        AND cv.occurred_at >= sqlc.arg(from_ts) AND cv.occurred_at < sqlc.arg(to_ts))::bigint AS revenue_cents;

-- Lifetime revenue attributed to this video across all conversions and all
-- time -- deliberately NOT bounded by from_ts/to_ts like GetVideoOverview.
-- Renewals keep accruing to the same video indefinitely, so LTV is the
-- all-time sum, not a windowed one. Refunds are already negative amounts,
-- so they net out naturally.
-- name: GetVideoLTV :one
SELECT
    COALESCE(SUM(cv.amount_cents), 0)::bigint AS ltv_cents,
    COUNT(DISTINCT cv.subscription_id) FILTER (WHERE cv.subscription_id IS NOT NULL)::bigint AS subscription_count
FROM conversions cv
WHERE cv.video_id = sqlc.arg(video_id);

-- ===== Channel analytics =====

-- name: UpsertChannelStatDaily :exec
INSERT INTO channel_stats_daily (channel_id, client_id, day, views, watch_minutes, subs_gained, subs_lost, likes, comments)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (channel_id, day) DO UPDATE
SET views = EXCLUDED.views,
    watch_minutes = EXCLUDED.watch_minutes,
    subs_gained = EXCLUDED.subs_gained,
    subs_lost = EXCLUDED.subs_lost,
    likes = EXCLUDED.likes,
    comments = EXCLUDED.comments;

-- Summed across all of this client's channels, zero-filled by the handler.
-- name: ListChannelStatsDailyByClient :many
SELECT day,
       SUM(views)::bigint AS views,
       SUM(watch_minutes)::bigint AS watch_minutes,
       SUM(subs_gained)::bigint AS subs_gained,
       SUM(subs_lost)::bigint AS subs_lost
FROM channel_stats_daily
WHERE client_id = sqlc.arg(client_id)
  AND day::timestamptz >= sqlc.arg(from_ts)::timestamptz
  AND day::timestamptz < sqlc.arg(to_ts)::timestamptz
GROUP BY day
ORDER BY day;

-- name: GetChannelAnalyticsOverview :one
SELECT
    COALESCE(SUM(views), 0)::bigint AS views,
    COALESCE(SUM(watch_minutes), 0)::bigint AS watch_minutes,
    COALESCE(SUM(subs_gained), 0)::bigint AS subs_gained,
    COALESCE(SUM(subs_lost), 0)::bigint AS subs_lost,
    COALESCE(SUM(likes), 0)::bigint AS likes,
    COALESCE(SUM(comments), 0)::bigint AS comments
FROM channel_stats_daily
WHERE client_id = sqlc.arg(client_id)
  AND day::timestamptz >= sqlc.arg(from_ts)::timestamptz
  AND day::timestamptz < sqlc.arg(to_ts)::timestamptz;

-- name: ListActiveSlackChannels :many
SELECT * FROM notification_channels
WHERE kind = 'slack' AND is_active = true
  AND (client_id IS NULL OR client_id = $1);

-- name: ListNotificationChannelsByWorkspace :many
SELECT * FROM notification_channels WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (workspace_id, client_id, kind, webhook_url, min_amount_cents, events)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: UpdateNotificationChannel :one
UPDATE notification_channels
SET webhook_url = $3, min_amount_cents = $4, events = $5, is_active = $6
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteNotificationChannel :execrows
DELETE FROM notification_channels WHERE id = $1 AND workspace_id = $2;