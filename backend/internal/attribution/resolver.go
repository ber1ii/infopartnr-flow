// Package attribution resolves a conversion back to a click, link and video.
package attribution

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"infopartnr-flow/backend/internal/db"
)

type Result struct {
	ClickID  pgtype.Int8
	LinkID   uuid.NullUUID
	VideoID  uuid.NullUUID
	TrakyoID string
	Method   string // track_id | email | none (matches conversions.attribution_method)
}

// Resolve tries trakyo_id first (scoped to the client), then the email->click identity map.
// When both a trakyo_id match and an email exist, the pair is stored for future email fallback.
func Resolve(ctx context.Context, q *db.Queries, clientID uuid.UUID, trakyoID, email string) (Result, error) {
	res := Result{Method: "none"}
	trakyoID = strings.TrimSpace(trakyoID)
	email = strings.ToLower(strings.TrimSpace(email))

	if trakyoID != "" {
		c, err := q.GetClickForClient(ctx, db.GetClickForClientParams{TrakyoID: trakyoID, ClientID: clientID})
		switch {
		case err == nil:
			res = Result{
				ClickID:  pgtype.Int8{Int64: c.ID, Valid: true},
				LinkID:   uuid.NullUUID{UUID: c.LinkID, Valid: true},
				VideoID:  c.VideoID,
				TrakyoID: c.TrakyoID,
				Method:   "track_id",
			}
			if email != "" {
				_ = q.UpsertIdentity(ctx, db.UpsertIdentityParams{ClientID: clientID, Email: email, TrakyoID: c.TrakyoID})
			}
			return res, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return res, err
		}
	}

	if email != "" {
		c, err := q.FindClickIDByEmail(ctx, db.FindClickIDByEmailParams{ClientID: clientID, Email: email})
		switch {
		case err == nil:
			return Result{
				ClickID:  pgtype.Int8{Int64: c.ID, Valid: true},
				LinkID:   uuid.NullUUID{UUID: c.LinkID, Valid: true},
				VideoID:  c.VideoID,
				TrakyoID: c.TrakyoID,
				Method:   "email",
			}, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return res, err
		}

		// Gmail/Google Workspace alias fallback -- see common.go's
		// resolveAttribution for the full rationale. Same restriction here:
		// only applied when both sides are gmail.com/googlemail.com.
		// Distinct variable: FindClickIDByEmailNormalized returns a
		// differently-named sqlc row type, even though structurally
		// identical, so it can't be assigned onto c above with =.
		cn, err := q.FindClickIDByEmailNormalized(ctx, db.FindClickIDByEmailNormalizedParams{ClientID: clientID, Email: email})
		switch {
		case err == nil:
			return Result{
				ClickID:  pgtype.Int8{Int64: cn.ID, Valid: true},
				LinkID:   uuid.NullUUID{UUID: cn.LinkID, Valid: true},
				VideoID:  cn.VideoID,
				TrakyoID: cn.TrakyoID,
				Method:   "email",
			}, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return res, err
		}
	}
	return res, nil
}
