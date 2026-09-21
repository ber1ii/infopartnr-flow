// Package collect implements the public POST /collect endpoint that
// track.js calls to link an email address it observes on the page to the
// visitor's trakyo_id, for email-fallback attribution.
package collect

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"infopartnr-flow/backend/internal/db"
)

const maxBodyBytes = 4096

type Handler struct {
	Q *db.Queries
}

// Serve is public, CORS-open, and always answers 204 -- it must never leak
// whether a given id/email pair is known, and must never block whatever
// flow on the client site called it.
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	var in struct {
		TrakyoID string `json:"trakyo_id"`
		Email    string `json:"email"`
	}
	if json.Unmarshal(body, &in) == nil {
		trakyoID := strings.TrimSpace(in.TrakyoID)
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if trakyoID != "" && email != "" {
			if click, err := h.Q.GetClickByTrakyoID(r.Context(), trakyoID); err == nil {
				_ = h.Q.UpsertIdentity(r.Context(), db.UpsertIdentityParams{
					ClientID: click.ClientID,
					Email:    email,
					TrakyoID: trakyoID,
				})
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
