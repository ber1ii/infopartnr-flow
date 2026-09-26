// backend/internal/api/attribution.go
package api

import (
	"net/http"
	"strings"

	"infopartnr-flow/backend/internal/attribution"
)

// GET /clients/{id}/attribution/lookup?email=...
// Called by the client's backend right before stripe.checkout.sessions.create,
// so client_reference_id can be set even when the session is created long
// after, and on a different device than, the visitor's Typeform/Calendly step.
func (a *API) lookupAttribution(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		writeErr(w, http.StatusBadRequest, "email is required")
		return
	}
	res, err := attribution.Resolve(r.Context(), a.Q, c.ID, "", email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if res.Method == "none" {
		writeJSON(w, http.StatusOK, map[string]any{"trakyo_id": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trakyo_id": res.TrakyoID})
}
