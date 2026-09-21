package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"infopartnr-flow/backend/internal/db"
)

func (a *API) listVideos(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	rows, err := a.Q.ListVideosWithCostsByClient(r.Context(), c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rows == nil {
		rows = []db.ListVideosWithCostsByClientRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// videoCtx loads the video for the client scoped in the URL. 404s (not
// found, not the raw error) if it doesn't exist or belongs to someone else,
// same ID-probing protection as clientCtx.
func (a *API) videoCtx(r *http.Request) (db.Video, bool) {
	c := clientFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "videoID"))
	if err != nil {
		return db.Video{}, false
	}
	v, err := a.Q.GetVideo(r.Context(), db.GetVideoParams{ID: id, ClientID: c.ID})
	if err != nil {
		return db.Video{}, false
	}
	return v, true
}

func (a *API) listVideoCosts(w http.ResponseWriter, r *http.Request) {
	v, ok := a.videoCtx(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	rows, err := a.Q.ListVideoCostsByVideo(r.Context(), v.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rows == nil {
		rows = []db.VideoCost{}
	}
	writeJSON(w, http.StatusOK, rows)
}

var videoCostKinds = map[string]bool{"production": true, "ad_spend": true, "other": true}

func (a *API) addVideoCost(w http.ResponseWriter, r *http.Request) {
	v, ok := a.videoCtx(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		Kind        string `json:"kind"`
		AmountCents int64  `json:"amount_cents"`
		Note        string `json:"note"`
		IncurredOn  string `json:"incurred_on"` // "2026-09-20"; empty = today
	}
	if !decode(w, r, &in) {
		return
	}
	if !videoCostKinds[in.Kind] {
		writeErr(w, http.StatusBadRequest, "kind must be 'production', 'ad_spend' or 'other'")
		return
	}
	if in.AmountCents <= 0 {
		writeErr(w, http.StatusBadRequest, "amount_cents must be positive")
		return
	}
	day := time.Now().UTC()
	if in.IncurredOn != "" {
		parsed, err := time.Parse("2006-01-02", in.IncurredOn)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "incurred_on must be YYYY-MM-DD")
			return
		}
		day = parsed
	}
	row, err := a.Q.AddVideoCost(r.Context(), db.AddVideoCostParams{
		VideoID:     v.ID,
		Kind:        in.Kind,
		AmountCents: in.AmountCents,
		Note:        strings.TrimSpace(in.Note),
		IncurredOn:  pgtype.Date{Time: day, Valid: true},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (a *API) deleteVideoCost(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	costID, err := uuid.Parse(chi.URLParam(r, "costID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	_, err = a.Q.DeleteVideoCost(r.Context(), db.DeleteVideoCostParams{ID: costID, ClientID: c.ID})
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
