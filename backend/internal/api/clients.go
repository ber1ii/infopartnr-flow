package api

import (
	"net/http"
	"strings"
	"time"

	"infopartnr-flow/backend/internal/auth"
	"infopartnr-flow/backend/internal/db"
)

func (a *API) listClients(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	clients, err := a.Q.ListClientsByWorkspace(r.Context(), p.WorkspaceID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if clients == nil {
		clients = []db.Client{}
	}
	writeJSON(w, http.StatusOK, clients)
}

func (a *API) createClient(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		ContactEmail string `json:"contact_email"`
		Timezone     string `json:"timezone"`
		Currency     string `json:"currency"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid timezone")
		return
	}
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if len(req.Currency) != 3 {
		writeErr(w, http.StatusBadRequest, "currency must be a 3-letter code")
		return
	}

	p := auth.FromContext(r.Context())
	c, err := a.Q.CreateClient(r.Context(), db.CreateClientParams{
		WorkspaceID: p.WorkspaceID, Name: req.Name, ContactEmail: strings.TrimSpace(req.ContactEmail),
		Timezone: req.Timezone, Currency: req.Currency,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) getClient(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, clientFrom(r.Context()))
}
