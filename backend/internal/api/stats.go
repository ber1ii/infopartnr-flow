package api

import (
	"net/http"
	"strconv"
	"time"

	"infopartnr-flow/backend/internal/db"
)

// parseRange returns [from, to) covering the last N days (UTC), including today.
func parseRange(r *http.Request) (from, to time.Time, days int) {
	days = 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 365 {
			days = n
		}
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -(days - 1)), today.AddDate(0, 0, 1), days
}

type overviewResp struct {
	Clicks            int64    `json:"clicks"`
	UniqueVisitors    int64    `json:"unique_visitors"`
	Conversions       int64    `json:"conversions"`
	RevenueCents      int64    `json:"revenue_cents"`
	CostCents         int64    `json:"cost_cents"`          // costs incurred during the selected period
	LifetimeCostCents int64    `json:"lifetime_cost_cents"` // total ever spent, for reference
	ROIPercent        *float64 `json:"roi_percent"`         // null when no costs in-period; period revenue vs period cost
	ConversionRate    float64  `json:"conversion_rate"`
	Currency          string   `json:"currency"`
}

func (a *API) statsOverview(w http.ResponseWriter, r *http.Request) {
	from, to, _ := parseRange(r)
	c := clientFrom(r.Context())
	row, err := a.Q.GetClientOverview(r.Context(), db.GetClientOverviewParams{ClientID: c.ID, FromTs: from, ToTs: to})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	resp := overviewResp{
		Clicks: row.Clicks, UniqueVisitors: row.UniqueVisitors, Conversions: row.Conversions,
		RevenueCents: row.RevenueCents, CostCents: row.CostCents, LifetimeCostCents: row.LifetimeCostCents,
		Currency: c.Currency,
	}
	// Same window on both sides now: period revenue against period cost.
	if row.CostCents > 0 {
		roi := float64(row.RevenueCents-row.CostCents) / float64(row.CostCents) * 100
		resp.ROIPercent = &roi
	}
	if row.Clicks > 0 {
		resp.ConversionRate = float64(row.Conversions) / float64(row.Clicks)
	}
	writeJSON(w, http.StatusOK, resp)
}

type dayPoint struct {
	Date   string `json:"date"`
	Clicks int64  `json:"clicks"`
}

func (a *API) statsClicksByDay(w http.ResponseWriter, r *http.Request) {
	from, to, days := parseRange(r)
	c := clientFrom(r.Context())
	rows, err := a.Q.ClicksByDay(r.Context(), db.ClicksByDayParams{ClientID: c.ID, FromTs: from, ToTs: to})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Day.UTC().Format("2006-01-02")] = row.Clicks
	}
	out := make([]dayPoint, 0, days) // zero-fill so the chart has no gaps
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		out = append(out, dayPoint{Date: k, Clicks: counts[k]})
	}
	writeJSON(w, http.StatusOK, out)
}
