package api

import (
	"net/http"
	"strconv"
	"time"

	"infopartnr-flow/backend/internal/db"
)

type conversionDTO struct {
	ID                int64     `json:"id"`
	EventType         string    `json:"event_type"`
	Source            string    `json:"source"`
	AmountCents       int64     `json:"amount_cents"`
	Currency          string    `json:"currency"`
	Email             string    `json:"email"`
	AttributionMethod string    `json:"attribution_method"`
	OccurredAt        time.Time `json:"occurred_at"`
	LinkID            *string   `json:"link_id"`
	LinkSlug          string    `json:"link_slug"`
	LinkName          string    `json:"link_name"`
}

// GET /clients/{id}/conversions?limit=50 (max 200). Visible to client users for their own client.
func (a *API) listConversions(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, 200)
	}
	rows, err := a.Q.ListConversionsByClient(r.Context(), db.ListConversionsByClientParams{
		ClientID: c.ID, RowLimit: int32(limit),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]conversionDTO, 0, len(rows))
	for _, x := range rows {
		d := conversionDTO{
			ID: x.ID, EventType: x.EventType, Source: x.Source, AmountCents: x.AmountCents,
			Currency: x.Currency, Email: x.Email, AttributionMethod: x.AttributionMethod,
			OccurredAt: x.OccurredAt, LinkSlug: x.LinkSlug, LinkName: x.LinkName,
		}
		if x.LinkID.Valid {
			s := x.LinkID.UUID.String()
			d.LinkID = &s
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}
