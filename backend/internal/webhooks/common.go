// Package webhooks contains HTTP handlers for inbound provider webhooks
// (Stripe, Typeform, Calendly). Each provider signs its requests
// differently; the bits every handler shares -- loading + decrypting the
// integration's secret, resolving attribution, and the null-safe param
// conversions InsertConversion needs -- live here.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"infopartnr-flow/backend/internal/crypto"
	"infopartnr-flow/backend/internal/db"
)

// 1 MiB is generous for a Typeform/Calendly webhook payload and keeps a
// malicious sender from streaming an unbounded body at us.
const maxBodyBytes = 1 << 20

var (
	errIntegrationNotFound = errors.New("integration not found")
	errIntegrationInactive = errors.New("integration inactive")
	errNoSecret            = errors.New("integration has no signing secret configured")
)

// loadSecret fetches the integration for a webhook URL id, checks it is
// active and belongs to the expected provider, and decrypts its signing
// secret. The URL id is itself an unguessable UUID; the signature check
// each handler does afterward is the real proof the request came from the
// provider, not this lookup.
func loadSecret(ctx context.Context, q *db.Queries, box *crypto.Box, id uuid.UUID, provider string) (db.GetIntegrationForWebhookRow, []byte, error) {
	row, err := q.GetIntegrationForWebhook(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, nil, errIntegrationNotFound
	}
	if err != nil {
		return row, nil, err
	}
	if row.Provider != provider {
		// Don't reveal that the id belongs to a different provider.
		return row, nil, errIntegrationNotFound
	}
	if !row.IsActive {
		return row, nil, errIntegrationInactive
	}
	if len(row.WebhookSecretEnc) == 0 {
		return row, nil, errNoSecret
	}
	secret, err := box.Decrypt(row.WebhookSecretEnc)
	if err != nil {
		return row, nil, err
	}
	return row, secret, nil
}

func hmacBase64(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func hmacHex(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func secureEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// resolveAttribution tries track-id attribution first (a hidden field or
// tracking param carrying our trakyo_id), then falls back to the most
// recent click linked to the given email via /collect, and otherwise
// reports no attribution -- the conversion still gets recorded either way.
func resolveAttribution(ctx context.Context, q *db.Queries, clientID uuid.UUID, trakyoID, email string) (clickID *int64, linkID, videoID *uuid.UUID, method string) {
	if trakyoID != "" {
		if c, err := q.GetClickForClient(ctx, db.GetClickForClientParams{TrakyoID: trakyoID, ClientID: clientID}); err == nil {
			id, link := c.ID, c.LinkID
			return &id, &link, nullUUIDPtr(c.VideoID), "track_id"
		}
	}
	if email != "" {
		if c, err := q.FindClickIDByEmail(ctx, db.FindClickIDByEmailParams{ClientID: clientID, Email: email}); err == nil {
			id, link := c.ID, c.LinkID
			return &id, &link, nullUUIDPtr(c.VideoID), "email"
		}
	}
	return nil, nil, nil, "none"
}

func nullUUIDPtr(n uuid.NullUUID) *uuid.UUID {
	if !n.Valid {
		return nil
	}
	id := n.UUID
	return &id
}

func int8OrNull(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func uuidOrNull(v *uuid.UUID) uuid.NullUUID {
	if v == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *v, Valid: true}
}
