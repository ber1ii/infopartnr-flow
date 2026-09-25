package api

import (
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"infopartnr-flow/backend/internal/db"
)

var (
	slugRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)
	reserved = map[string]bool{"api": true, "healthz": true, "assets": true}
)

const slugAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomSlug(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = slugAlphabet[int(b[i])%len(slugAlphabet)]
	}
	return string(b)
}

func validTarget(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (a *API) listLinks(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	links, err := a.Q.ListLinksByClient(r.Context(), c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if links == nil {
		links = []db.TrackingLink{}
	}
	writeJSON(w, http.StatusOK, links)
}

func (a *API) createLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string     `json:"name"`
		Slug      string     `json:"slug"`
		TargetURL string     `json:"target_url"`
		VideoID   *uuid.UUID `json:"video_id"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validTarget(req.TargetURL) {
		writeErr(w, http.StatusBadRequest, "target_url must be a valid http(s) URL")
		return
	}
	custom := req.Slug != ""
	if custom && (!slugRe.MatchString(req.Slug) || reserved[strings.ToLower(req.Slug)]) {
		writeErr(w, http.StatusBadRequest, "invalid slug (letters, digits, - and _ only, max 50)")
		return
	}

	ctx := r.Context()
	c := clientFrom(ctx)

	var video uuid.NullUUID
	if req.VideoID != nil {
		if _, err := a.Q.GetVideo(ctx, db.GetVideoParams{ID: *req.VideoID, ClientID: c.ID}); err != nil {
			writeErr(w, http.StatusBadRequest, "video not found for this client")
			return
		}
		video = uuid.NullUUID{UUID: *req.VideoID, Valid: true}
	}

	for attempt := 0; attempt < 5; attempt++ {
		slug := req.Slug
		if !custom {
			slug = randomSlug(7)
		}
		link, err := a.Q.CreateLink(ctx, db.CreateLinkParams{
			ClientID: c.ID, VideoID: video, Slug: slug, Name: strings.TrimSpace(req.Name),
			TargetUrl: req.TargetURL, ExpiresAt: req.ExpiresAt,
		})
		if err == nil {
			a.Cache.Invalidate(slug) // clear any cached "not found"
			writeJSON(w, http.StatusCreated, link)
			return
		}
		if isUniqueViolation(err) {
			if custom {
				writeErr(w, http.StatusConflict, "slug already taken")
				return
			}
			continue
		}
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeErr(w, http.StatusInternalServerError, "could not allocate slug")
}

func (a *API) updateLink(w http.ResponseWriter, r *http.Request) {
	linkID, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Name      *string    `json:"name"`
		TargetURL *string    `json:"target_url"`
		IsActive  *bool      `json:"is_active"`
		VideoID   *uuid.UUID `json:"video_id"`
	}
	if !decode(w, r, &req) {
		return
	}

	c := clientFrom(r.Context())
	params := db.UpdateLinkParams{ID: linkID, ClientID: c.ID}
	if req.Name != nil {
		params.Name = pgtype.Text{String: strings.TrimSpace(*req.Name), Valid: true}
	}
	if req.TargetURL != nil {
		if !validTarget(*req.TargetURL) {
			writeErr(w, http.StatusBadRequest, "target_url must be a valid http(s) URL")
			return
		}
		params.TargetUrl = pgtype.Text{String: *req.TargetURL, Valid: true}
	}
	if req.IsActive != nil {
		params.IsActive = pgtype.Bool{Bool: *req.IsActive, Valid: true}
	}
	if req.VideoID != nil {
		if _, err := a.Q.GetVideo(r.Context(), db.GetVideoParams{ID: *req.VideoID, ClientID: c.ID}); err != nil {
			writeErr(w, http.StatusBadRequest, "video not found for this client")
			return
		}
		params.VideoID = uuid.NullUUID{UUID: *req.VideoID, Valid: true}
	}

	link, err := a.Q.UpdateLink(r.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.Cache.Invalidate(link.Slug)
	writeJSON(w, http.StatusOK, link)
}

func (a *API) deleteLink(w http.ResponseWriter, r *http.Request) {
	linkID, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	c := clientFrom(r.Context())
	slug, err := a.Q.DeleteLink(r.Context(), db.DeleteLinkParams{ID: linkID, ClientID: c.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.Cache.Invalidate(slug)
	w.WriteHeader(http.StatusNoContent)
}
