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
	var clients []db.Client
	var err error
	if r.URL.Query().Get("archived") == "true" {
		clients, err = a.Q.ListArchivedClientsByWorkspace(r.Context(), p.WorkspaceID)
	} else {
		clients, err = a.Q.ListClientsByWorkspace(r.Context(), p.WorkspaceID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if clients == nil {
		clients = []db.Client{}
	}
	writeJSON(w, http.StatusOK, clients)
}

// archiveClient hides a client from the active list without deleting
// anything. All of its data (links, conversions, integrations) is
// untouched and the client's own dashboard pages stay reachable directly --
// this only affects the list/switcher. Fully reversible via restoreClient.
func (a *API) archiveClient(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	c := clientFrom(r.Context())
	n, err := a.Q.ArchiveClient(r.Context(), db.ArchiveClientParams{ID: c.ID, WorkspaceID: p.WorkspaceID})
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

func (a *API) restoreClient(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	c := clientFrom(r.Context())
	n, err := a.Q.RestoreClient(r.Context(), db.RestoreClientParams{ID: c.ID, WorkspaceID: p.WorkspaceID})
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
