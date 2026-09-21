package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"infopartnr-flow/backend/internal/db"
)

type integrationDTO struct {
	ID         uuid.UUID `json:"id"`
	Provider   string    `json:"provider"`
	IsActive   bool      `json:"is_active"`
	HasSecret  bool      `json:"has_secret"`
	WebhookURL string    `json:"webhook_url"`
	CreatedAt  time.Time `json:"created_at"`
}

func (a *API) integrationDTO(id uuid.UUID, provider string, active, hasSecret bool, created time.Time) integrationDTO {
	return integrationDTO{
		ID:         id,
		Provider:   provider,
		IsActive:   active,
		HasSecret:  hasSecret,
		WebhookURL: strings.TrimRight(a.PublicURL, "/") + "/webhooks/" + provider + "/" + id.String(),
		CreatedAt:  created,
	}
}

func (a *API) listIntegrations(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	rows, err := a.Q.ListIntegrationsByClient(r.Context(), c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]integrationDTO, 0, len(rows))
	for _, x := range rows {
		out = append(out, a.integrationDTO(x.ID, x.Provider, x.IsActive, x.HasSecret, x.CreatedAt))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createIntegration(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	var in struct {
		Provider            string `json:"provider"`
		SigningSecret       string `json:"signing_secret"`
		PersonalAccessToken string `json:"personal_access_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	switch in.Provider {
	case "stripe", "typeform":
		a.createSecretIntegration(w, r, c.ID, in.Provider, in.SigningSecret)
	case "calendly":
		a.createCalendlyIntegration(w, r, c.ID, in.PersonalAccessToken)
	default:
		writeErr(w, http.StatusBadRequest, "provider must be 'stripe', 'calendly' or 'typeform'")
	}
}

// createSecretIntegration handles providers where the client (browser or
// the provider's dashboard) generates the signing secret and pastes it in:
// Stripe's whsec_ secret, and Typeform's browser-generated one.
func (a *API) createSecretIntegration(w http.ResponseWriter, r *http.Request, clientID uuid.UUID, provider, secret string) {
	secret = strings.TrimSpace(secret)
	var enc []byte
	if secret != "" {
		if provider == "stripe" && !strings.HasPrefix(secret, "whsec_") {
			writeErr(w, http.StatusBadRequest, "signing_secret must start with whsec_")
			return
		}
		if len(secret) < 16 {
			writeErr(w, http.StatusBadRequest, "signing_secret is too short")
			return
		}
		var err error
		if enc, err = a.Box.Encrypt([]byte(secret)); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	row, err := a.Q.CreateIntegration(r.Context(), db.CreateIntegrationParams{
		ClientID: clientID, Provider: provider, WebhookSecretEnc: enc,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "integration already exists for this client")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, a.integrationDTO(row.ID, row.Provider, row.IsActive, row.HasSecret, row.CreatedAt))
}

// createCalendlyIntegration registers the webhook with Calendly itself: it
// creates the integration row to get a stable id for the callback URL,
// generates a signing key, asks Calendly to create an organization-scoped
// webhook subscription pointed at that URL, then stores the signing key
// encrypted. The personal access token is used only for this request and
// is never written to the database. Any failure rolls back the row.
func (a *API) createCalendlyIntegration(w http.ResponseWriter, r *http.Request, clientID uuid.UUID, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		writeErr(w, http.StatusBadRequest, "personal_access_token is required")
		return
	}
	ctx := r.Context()

	row, err := a.Q.CreateIntegration(ctx, db.CreateIntegrationParams{ClientID: clientID, Provider: "calendly"})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "integration already exists for this client")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	signingKey, err := randomHex(24)
	if err != nil {
		a.rollbackIntegration(ctx, row.ID, clientID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	callbackURL := strings.TrimRight(a.PublicURL, "/") + "/webhooks/calendly/" + row.ID.String()

	// TEMPORARY TEST BYPASS -- remove before shipping. Lets you exercise
	// the webhook-receiving side locally without a real paid Calendly
	// account / ngrok tunnel. Skips ONLY the two live Calendly API calls;
	// the row is still created and the signing key still generated,
	// encrypted, and stored exactly as in the real path.
	if os.Getenv("SKIP_CALENDLY_API") == "true" {
		log.Printf("SKIP_CALENDLY_API: integration_id=%s signing_key=%s callback_url=%s", row.ID, signingKey, callbackURL)
	} else {
		cc := newCalendlyClient(token)
		orgURI, err := cc.currentUser(ctx)
		if err != nil {
			a.rollbackIntegration(ctx, row.ID, clientID)
			writeErr(w, http.StatusBadGateway, "could not authenticate with Calendly: check the personal access token and plan")
			return
		}
		if _, err := cc.createWebhookSubscription(ctx, callbackURL, orgURI, signingKey); err != nil {
			a.rollbackIntegration(ctx, row.ID, clientID)
			writeErr(w, http.StatusBadGateway, "could not register the Calendly webhook (Calendly rejects localhost URLs -- deploy or use ngrok)")
			return
		}
	}

	enc, err := a.Box.Encrypt([]byte(signingKey))
	if err != nil {
		a.rollbackIntegration(ctx, row.ID, clientID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n, err := a.Q.SetIntegrationSecret(ctx, db.SetIntegrationSecretParams{ID: row.ID, ClientID: clientID, WebhookSecretEnc: enc}); err != nil || n == 0 {
		a.rollbackIntegration(ctx, row.ID, clientID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, a.integrationDTO(row.ID, row.Provider, true, true, row.CreatedAt))
}

func (a *API) rollbackIntegration(ctx context.Context, id, clientID uuid.UUID) {
	_, _ = a.Q.DeleteIntegration(ctx, db.DeleteIntegrationParams{ID: id, ClientID: clientID})
}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (a *API) setIntegrationSecret(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "integrationID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		SigningSecret string `json:"signing_secret"`
	}
	if !decode(w, r, &in) {
		return
	}
	secret := strings.TrimSpace(in.SigningSecret)
	if len(secret) < 8 {
		writeErr(w, http.StatusBadRequest, "signing_secret is too short")
		return
	}
	enc, err := a.Box.Encrypt([]byte(secret))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	n, err := a.Q.SetIntegrationSecret(r.Context(), db.SetIntegrationSecretParams{ID: id, ClientID: c.ID, WebhookSecretEnc: enc})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "integrationID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	// Note: for Calendly this only removes our row. The subscription on
	// Calendly's side is left behind; our endpoint then 404s and Calendly
	// disables it after enough failed deliveries (see handoff notes).
	n, err := a.Q.DeleteIntegration(r.Context(), db.DeleteIntegrationParams{ID: id, ClientID: c.ID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
