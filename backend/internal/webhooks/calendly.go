package webhooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"infopartnr-flow/backend/internal/crypto"
	"infopartnr-flow/backend/internal/db"
)

// Reject requests whose signature timestamp has drifted too far from now,
// so a captured request body can't be replayed indefinitely.
const calendlySignatureTolerance = 5 * time.Minute

type Calendly struct {
	Q   *db.Queries
	Box *crypto.Box
}

type calendlyPayload struct {
	Event   string `json:"event"`
	Payload struct {
		URI         string `json:"uri"`
		Email       string `json:"email"`
		Rescheduled bool   `json:"rescheduled"`
		OldInvitee  string `json:"old_invitee"`
		Tracking    struct {
			UTMContent string `json:"utm_content"`
		} `json:"tracking"`
		ScheduledEvent struct {
			URI       string    `json:"uri"`
			StartTime time.Time `json:"start_time"`
		} `json:"scheduled_event"`
	} `json:"payload"`
}

func (c *Calendly) Serve(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "integrationID"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	integ, secret, err := loadSecret(r.Context(), c.Q, c.Box, id, "calendly")
	switch {
	case errors.Is(err, errIntegrationNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case errors.Is(err, errIntegrationInactive), errors.Is(err, errNoSecret):
		http.Error(w, "integration not ready", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !validCalendlySignature(r.Header.Get("Calendly-Webhook-Signature"), secret, body) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	var payload calendlyPayload
	if json.Unmarshal(body, &payload) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if payload.Event != "invitee.created" {
		w.WriteHeader(http.StatusOK) // e.g. invitee.canceled -- ack, no-op
		return
	}

	externalID := lastPathSegment(payload.Payload.URI)
	if externalID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	occurred := payload.Payload.ScheduledEvent.StartTime
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}

	// A reschedule fires a fresh invitee.created with a new invitee uri and
	// `old_invitee` pointing at the one it replaces. Update that row in
	// place instead of inserting a second booked_call for the same booking.
	if payload.Payload.Rescheduled && payload.Payload.OldInvitee != "" {
		oldID := lastPathSegment(payload.Payload.OldInvitee)
		existing, err := c.Q.GetConversionBySourceExternalID(ctx, db.GetConversionBySourceExternalIDParams{
			ClientID: integ.ClientID, Source: "calendly", ExternalID: oldID,
		})
		if err == nil {
			params := db.UpdateConversionOnRescheduleParams{
				ID: existing.ID, ClientID: integ.ClientID, NewExternalID: externalID, OccurredAt: occurred,
			}
			if existing.AttributionMethod == "none" {
				trakyoID := payload.Payload.Tracking.UTMContent
				email := strings.ToLower(strings.TrimSpace(payload.Payload.Email))
				clickID, linkID, _, method := resolveAttribution(ctx, c.Q, integ.ClientID, trakyoID, email)
				if method != "none" {
					params.ClickID = pgtype.Int8{Int64: *clickID, Valid: true}
					params.LinkID = uuid.NullUUID{UUID: *linkID, Valid: true}
					params.AttributionMethod = pgtype.Text{String: method, Valid: true}
				}
			}
			_, _ = c.Q.UpdateConversionOnReschedule(ctx, params)
			w.WriteHeader(http.StatusOK)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	trakyoID := payload.Payload.Tracking.UTMContent
	email := strings.ToLower(strings.TrimSpace(payload.Payload.Email))
	clickID, linkID, videoID, method := resolveAttribution(ctx, c.Q, integ.ClientID, trakyoID, email)

	_, err = c.Q.InsertConversion(ctx, db.InsertConversionParams{
		ClientID:          integ.ClientID,
		ClickID:           int8OrNull(clickID),
		LinkID:            uuidOrNull(linkID),
		VideoID:           uuidOrNull(videoID),
		TrakyoID:          trakyoID,
		Source:            "calendly",
		EventType:         "booked_call",
		ExternalID:        externalID,
		AmountCents:       0,
		Currency:          "USD",
		Email:             email,
		AttributionMethod: method,
		Raw:               body,
		OccurredAt:        occurred,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// validCalendlySignature checks Calendly's "t=<unix>,v1=<hex hmac>" header,
// where the signed message is "<t>.<raw body>".
func validCalendlySignature(header string, secret, body []byte) bool {
	if header == "" {
		return false
	}
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			sig = kv[1]
		}
	}
	if ts == "" || sig == "" {
		return false
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := time.Since(time.Unix(sec, 0)); d > calendlySignatureTolerance || d < -calendlySignatureTolerance {
		return false
	}
	return secureEqual(sig, hmacHex(secret, fmt.Appendf(nil, "%s.%s", ts, body)))
}

func lastPathSegment(uri string) string {
	uri = strings.TrimRight(uri, "/")
	if idx := strings.LastIndex(uri, "/"); idx >= 0 {
		return uri[idx+1:]
	}
	return uri
}
