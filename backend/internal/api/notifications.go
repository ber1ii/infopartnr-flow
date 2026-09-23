package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"infopartnr-flow/backend/internal/auth"
	"infopartnr-flow/backend/internal/db"
)

var validNotifyEvents = map[string]bool{
	"purchase":             true,
	"subscription_start":   true,
	"subscription_renewal": true,
	"booked_call":          true,
	"lead":                 true,
	"refund":               true,
}

func (a *API) listNotificationChannels(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	chs, err := a.Q.ListNotificationChannelsByWorkspace(r.Context(), p.WorkspaceID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if chs == nil {
		chs = []db.NotificationChannel{}
	}
	writeJSON(w, http.StatusOK, chs)
}

func (a *API) createNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind           string   `json:"kind"`
		WebhookURL     string   `json:"webhook_url"`
		MinAmountCents int64    `json:"min_amount_cents"`
		Events         []string `json:"events"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	if req.Kind != "slack" && req.Kind != "discord" {
		writeErr(w, http.StatusBadRequest, "kind must be slack or discord")
		return
	}
	req.WebhookURL = strings.TrimSpace(req.WebhookURL)
	if !strings.HasPrefix(req.WebhookURL, "https://") {
		writeErr(w, http.StatusBadRequest, "webhook_url must be a valid https URL")
		return
	}
	if req.MinAmountCents < 0 {
		writeErr(w, http.StatusBadRequest, "min_amount_cents must be >= 0")
		return
	}
	if len(req.Events) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one event is required")
		return
	}
	for _, e := range req.Events {
		if !validNotifyEvents[e] {
			writeErr(w, http.StatusBadRequest, "invalid event: "+e)
			return
		}
	}

	p := auth.FromContext(r.Context())
	ch, err := a.Q.CreateNotificationChannel(r.Context(), db.CreateNotificationChannelParams{
		WorkspaceID:    p.WorkspaceID,
		ClientID:       uuid.NullUUID{}, // workspace-level: all clients
		Kind:           req.Kind,
		WebhookUrl:     req.WebhookURL,
		MinAmountCents: req.MinAmountCents,
		Events:         req.Events,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (a *API) updateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		WebhookURL     string   `json:"webhook_url"`
		MinAmountCents int64    `json:"min_amount_cents"`
		Events         []string `json:"events"`
		IsActive       bool     `json:"is_active"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.WebhookURL = strings.TrimSpace(req.WebhookURL)
	if !strings.HasPrefix(req.WebhookURL, "https://") {
		writeErr(w, http.StatusBadRequest, "webhook_url must be a valid https URL")
		return
	}
	if req.MinAmountCents < 0 {
		writeErr(w, http.StatusBadRequest, "min_amount_cents must be >= 0")
		return
	}
	if len(req.Events) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one event is required")
		return
	}
	for _, e := range req.Events {
		if !validNotifyEvents[e] {
			writeErr(w, http.StatusBadRequest, "invalid event: "+e)
			return
		}
	}

	p := auth.FromContext(r.Context())
	ch, err := a.Q.UpdateNotificationChannel(r.Context(), db.UpdateNotificationChannelParams{
		ID:             id,
		WorkspaceID:    p.WorkspaceID,
		WebhookUrl:     req.WebhookURL,
		MinAmountCents: req.MinAmountCents,
		Events:         req.Events,
		IsActive:       req.IsActive,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (a *API) deleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.FromContext(r.Context())
	n, err := a.Q.DeleteNotificationChannel(r.Context(), db.DeleteNotificationChannelParams{
		ID: id, WorkspaceID: p.WorkspaceID,
	})
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
