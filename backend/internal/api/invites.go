package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"infopartnr-flow/backend/internal/auth"
	"infopartnr-flow/backend/internal/db"
)

const inviteTTL = 7 * 24 * time.Hour

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

// The raw token is shown once; only its hash is stored.
func newInviteToken() (raw, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

// POST /clients/{clientID}/invites  (staff)
func (a *API) createInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(email, "@") {
		writeErr(w, http.StatusBadRequest, "valid email required")
		return
	}
	ctx := r.Context()
	c := clientFrom(ctx)
	p := auth.FromContext(ctx)

	raw, hash := newInviteToken()
	exp := time.Now().Add(inviteTTL)
	inv, err := a.Q.CreateInvitation(ctx, db.CreateInvitationParams{
		WorkspaceID: p.WorkspaceID, ClientID: c.ID, Email: email, TokenHash: hash, ExpiresAt: exp,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": inv.ID, "email": email, "expires_at": exp,
		"invite_url": a.AppURL + "/invite/" + raw, // no email sending yet: copy and send this link
	})
}

// GET /clients/{clientID}/invites  (staff)
func (a *API) listInvites(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Q.ListInvitationsByClient(r.Context(), clientFrom(r.Context()).ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rows == nil {
		rows = []db.ListInvitationsByClientRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (a *API) loadInvite(w http.ResponseWriter, r *http.Request) (db.GetInvitationByTokenHashRow, bool) {
	inv, err := a.Q.GetInvitationByTokenHash(r.Context(), hashToken(chi.URLParam(r, "token")))
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "invite not found")
		return inv, false
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return inv, false
	}
	if inv.AcceptedAt != nil || time.Now().After(inv.ExpiresAt) {
		writeErr(w, http.StatusGone, "invite already used or expired")
		return inv, false
	}
	return inv, true
}

// GET /invites/{token}  (public) - lets the frontend show "Join <client>".
func (a *API) inviteInfo(w http.ResponseWriter, r *http.Request) {
	inv, ok := a.loadInvite(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": inv.Email, "client_name": inv.ClientName})
}

// POST /invites/{token}/accept  (public)
func (a *API) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Password) < 10 || len(req.Password) > 72 {
		writeErr(w, http.StatusBadRequest, "password must be 10-72 characters")
		return
	}
	inv, ok := a.loadInvite(w, r)
	if !ok {
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	ctx := r.Context()
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback(ctx)
	qtx := a.Q.WithTx(tx)

	if n, err := qtx.MarkInvitationAccepted(ctx, inv.ID); err != nil || n == 0 {
		writeErr(w, http.StatusGone, "invite already used or expired")
		return
	}
	user, err := qtx.CreateUser(ctx, db.CreateUserParams{Email: inv.Email, PasswordHash: hash, Name: strings.TrimSpace(req.Name)})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "an account with this email already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := qtx.CreateMembership(ctx, db.CreateMembershipParams{
		WorkspaceID: inv.WorkspaceID, UserID: user.ID, Role: auth.RoleClient,
		ClientID: uuid.NullUUID{UUID: inv.ClientID, Valid: true},
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	cid := inv.ClientID
	token, err := a.Auth.Issue(user.ID, inv.WorkspaceID, auth.RoleClient, &cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, authResponse{
		Token: token, UserID: user.ID, Email: user.Email, Name: user.Name,
		WorkspaceID: inv.WorkspaceID, Role: auth.RoleClient, ClientID: &cid,
	})
}
