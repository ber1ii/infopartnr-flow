-- name: GetLinkBySlug :one
SELECT id, target_url FROM tracking_links WHERE slug = $1 LIMIT 1;

-- name: InsertClick :exec
INSERT INTO clicks (link_id, trakyo_id, ip_hash, user_agent)
VALUES ($1, $2, $3, $4);