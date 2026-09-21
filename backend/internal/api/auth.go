package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"infopartnr-flow/backend/internal/auth"
	"infopartnr-flow/backend/internal/db"
)

type authResponse struct {
	Token       string     `json:"token,omitempty"`
	UserID      uuid.UUID  `json:"user_id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	Role        string     `json:"role"`
	ClientID    *uuid.UUID `json:"client_id,omitempty"`
}

// register creates the first user + workspace (owner). Closed afterwards;
// further users join through invites.
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email         string `json:"email"`
		Password      string `json:"password"`
		Name          string `json:"name"`
		WorkspaceName string `json:"workspace_name"`
	}
	if !decode(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(email, "@") || len(req.Password) < 10 || len(req.Password) > 72 {
		writeErr(w, http.StatusBadRequest, "valid email and a 10-72 character password are required")
		return
	}
	ctx := r.Context()
	n, err := a.Q.CountUsers(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n > 0 {
		writeErr(w, http.StatusForbidden, "registration is closed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	wsName := strings.TrimSpace(req.WorkspaceName)
	if wsName == "" {
		wsName = "My Agency"
	}

	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback(ctx)
	qtx := a.Q.WithTx(tx)

	user, err := qtx.CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: hash, Name: strings.TrimSpace(req.Name)})
	if err != nil {
		log.Printf("register: create user: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	ws, err := qtx.CreateWorkspace(ctx, wsName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := qtx.CreateMembership(ctx, db.CreateMembershipParams{
		WorkspaceID: ws.ID, UserID: user.ID, Role: auth.RoleOwner,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	token, err := a.Auth.Issue(user.ID, ws.ID, auth.RoleOwner, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, authResponse{
		Token: token, UserID: user.ID, Email: user.Email, Name: user.Name,
		WorkspaceID: ws.ID, Role: auth.RoleOwner,
	})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	email := strings.ToLower(strings.TrimSpace(req.Email))

	user, err := a.Q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.BurnPassword(req.Password)
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	ms, err := a.Q.ListMembershipsByUser(ctx, user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(ms) == 0 {
		writeErr(w, http.StatusForbidden, "account has no workspace access")
		return
	}
	m := ms[0] // multi-workspace switching can come later

	var cid *uuid.UUID
	if m.ClientID.Valid {
		id := m.ClientID.UUID
		cid = &id
	}
	token, err := a.Auth.Issue(user.ID, m.WorkspaceID, m.Role, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, authResponse{
		Token: token, UserID: user.ID, Email: user.Email, Name: user.Name,
		WorkspaceID: m.WorkspaceID, Role: m.Role, ClientID: cid,
	})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	user, err := a.Q.GetUserByID(r.Context(), p.UserID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "user no longer exists")
		return
	}
	resp := authResponse{
		UserID: user.ID, Email: user.Email, Name: user.Name,
		WorkspaceID: p.WorkspaceID, Role: p.Role,
	}
	if p.Role == auth.RoleClient {
		id := p.ClientID
		resp.ClientID = &id
	}
	writeJSON(w, http.StatusOK, resp)
}
