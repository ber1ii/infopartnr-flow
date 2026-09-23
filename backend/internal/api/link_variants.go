package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"infopartnr-flow/backend/internal/db"
)

const (
	maxVariantsPerLink = 10
	maxVariantWeight   = 1000
)

// linkFor loads {linkID} scoped to the caller's client (tenant gate for all
// variant routes). Writes 404 and returns false if missing/foreign.
func (a *API) linkFor(w http.ResponseWriter, r *http.Request) (db.TrackingLink, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return db.TrackingLink{}, false
	}
	link, err := a.Q.GetLinkByID(r.Context(), db.GetLinkByIDParams{ID: id, ClientID: clientFrom(r.Context()).ID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return db.TrackingLink{}, false
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return db.TrackingLink{}, false
	}
	return link, true
}

func (a *API) listVariants(w http.ResponseWriter, r *http.Request) {
	link, ok := a.linkFor(w, r)
	if !ok {
		return
	}
	rows, err := a.Q.ListVariantsWithStats(r.Context(), link.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rows == nil {
		rows = []db.ListVariantsWithStatsRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (a *API) createVariant(w http.ResponseWriter, r *http.Request) {
	link, ok := a.linkFor(w, r)
	if !ok {
		return
	}
	var req struct {
		TargetURL string `json:"target_url"`
		Weight    *int32 `json:"weight"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validTarget(req.TargetURL) {
		writeErr(w, http.StatusBadRequest, "target_url must be a valid http(s) URL")
		return
	}
	weight := int32(1)
	if req.Weight != nil {
		weight = *req.Weight
	}
	if weight < 1 || weight > maxVariantWeight {
		writeErr(w, http.StatusBadRequest, "weight must be between 1 and 1000")
		return
	}
	n, err := a.Q.CountLinkVariants(r.Context(), link.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n >= maxVariantsPerLink {
		writeErr(w, http.StatusBadRequest, "too many variants on this link (max 10)")
		return
	}
	v, err := a.Q.AddLinkVariant(r.Context(), db.AddLinkVariantParams{
		LinkID: link.ID, TargetUrl: req.TargetURL, Weight: weight,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.Cache.Invalidate(link.Slug)
	writeJSON(w, http.StatusCreated, v)
}

func (a *API) updateVariant(w http.ResponseWriter, r *http.Request) {
	link, ok := a.linkFor(w, r)
	if !ok {
		return
	}
	vid, err := uuid.Parse(chi.URLParam(r, "variantID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Weight   *int32 `json:"weight"`
		IsActive *bool  `json:"is_active"`
	}
	if !decode(w, r, &req) {
		return
	}
	params := db.UpdateLinkVariantParams{ID: vid, LinkID: link.ID}
	if req.Weight != nil {
		if *req.Weight < 1 || *req.Weight > maxVariantWeight {
			writeErr(w, http.StatusBadRequest, "weight must be between 1 and 1000")
			return
		}
		params.Weight = pgtype.Int4{Int32: *req.Weight, Valid: true}
	}
	if req.IsActive != nil {
		params.IsActive = pgtype.Bool{Bool: *req.IsActive, Valid: true}
	}
	v, err := a.Q.UpdateLinkVariant(r.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.Cache.Invalidate(link.Slug)
	writeJSON(w, http.StatusOK, v)
}

func (a *API) deleteVariant(w http.ResponseWriter, r *http.Request) {
	link, ok := a.linkFor(w, r)
	if !ok {
		return
	}
	vid, err := uuid.Parse(chi.URLParam(r, "variantID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	n, err := a.Q.DeleteLinkVariant(r.Context(), db.DeleteLinkVariantParams{ID: vid, LinkID: link.ID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	a.Cache.Invalidate(link.Slug)
	w.WriteHeader(http.StatusNoContent)
}
