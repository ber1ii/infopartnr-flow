package webhooks

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"infopartnr-flow/backend/internal/crypto"
	"infopartnr-flow/backend/internal/db"
)

const trakyoHiddenField = "trakyo_id"

type Typeform struct {
	Q   *db.Queries
	Box *crypto.Box
}

type typeformPayload struct {
	EventType    string `json:"event_type"`
	FormResponse struct {
		Token       string            `json:"token"`
		SubmittedAt time.Time         `json:"submitted_at"`
		Hidden      map[string]string `json:"hidden"`
		Answers     []struct {
			Type  string `json:"type"`
			Email string `json:"email"`
		} `json:"answers"`
	} `json:"form_response"`
}

func (t *Typeform) Serve(w http.ResponseWriter, r *http.Request) {
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

	integ, secret, err := loadSecret(r.Context(), t.Q, t.Box, id, "typeform")
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

	const prefix = "sha256="
	sig := r.Header.Get("Typeform-Signature")
	if !strings.HasPrefix(sig, prefix) || !secureEqual(strings.TrimPrefix(sig, prefix), hmacBase64(secret, body)) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	var payload typeformPayload
	if json.Unmarshal(body, &payload) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if payload.EventType != "" && payload.EventType != "form_response" {
		w.WriteHeader(http.StatusOK) // ack event types we don't otherwise act on
		return
	}
	if payload.FormResponse.Token == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	trakyoID := payload.FormResponse.Hidden[trakyoHiddenField]
	var email string
	for _, a := range payload.FormResponse.Answers {
		if a.Type == "email" && a.Email != "" {
			email = strings.ToLower(strings.TrimSpace(a.Email))
			break
		}
	}

	occurred := payload.FormResponse.SubmittedAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}

	clickID, linkID, videoID, method := resolveAttribution(r.Context(), t.Q, integ.ClientID, trakyoID, email)

	_, err = t.Q.InsertConversion(r.Context(), db.InsertConversionParams{
		ClientID:          integ.ClientID,
		ClickID:           int8OrNull(clickID),
		LinkID:            uuidOrNull(linkID),
		VideoID:           uuidOrNull(videoID),
		TrakyoID:          trakyoID,
		Source:            "typeform",
		EventType:         "lead",
		ExternalID:        payload.FormResponse.Token, // one row per submission, safe to retry
		AmountCents:       0,
		Currency:          "USD",
		Email:             email,
		AttributionMethod: method,
		Raw:               body,
		OccurredAt:        occurred,
	})
	// ON CONFLICT DO NOTHING means a retried/duplicate delivery returns
	// pgx.ErrNoRows here -- that's success, not a failure to report.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
