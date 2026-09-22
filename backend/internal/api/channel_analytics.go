package api

import (
	"net/http"

	"infopartnr-flow/backend/internal/db"
)

type channelAnalyticsOverview struct {
	Views        int64 `json:"views"`
	WatchMinutes int64 `json:"watch_minutes"`
	SubsGained   int64 `json:"subs_gained"`
	SubsLost     int64 `json:"subs_lost"`
	SubsNet      int64 `json:"subs_net"`
	Likes        int64 `json:"likes"`
	Comments     int64 `json:"comments"`
}

type channelDayPoint struct {
	Date         string `json:"date"`
	Views        int64  `json:"views"`
	WatchMinutes int64  `json:"watch_minutes"`
	SubsGained   int64  `json:"subs_gained"`
	SubsLost     int64  `json:"subs_lost"`
}

type topVideoRow struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	YoutubeVideoID string `json:"youtube_video_id"`
	ThumbnailURL   string `json:"thumbnail_url"`
	Clicks         int64  `json:"clicks"`
	Conversions    int64  `json:"conversions"`
	RevenueCents   int64  `json:"revenue_cents"`
	CostCents      int64  `json:"cost_cents"`
}

type channelAnalyticsResp struct {
	Overview  channelAnalyticsOverview `json:"overview"`
	Daily     []channelDayPoint        `json:"daily"`
	TopVideos []topVideoRow            `json:"top_videos"`
}

// GET /clients/{clientID}/youtube/analytics?days=30
func (a *API) youtubeAnalytics(w http.ResponseWriter, r *http.Request) {
	from, to, days := parseRange(r)
	c := clientFrom(r.Context())

	ov, err := a.Q.GetChannelAnalyticsOverview(r.Context(), db.GetChannelAnalyticsOverviewParams{
		ClientID: c.ID, FromTs: from, ToTs: to,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	rows, err := a.Q.ListChannelStatsDailyByClient(r.Context(), db.ListChannelStatsDailyByClientParams{
		ClientID: c.ID, FromTs: from, ToTs: to,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	byDay := make(map[string]db.ListChannelStatsDailyByClientRow, len(rows))
	for _, row := range rows {
		byDay[row.Day.Time.UTC().Format("2006-01-02")] = row
	}
	daily := make([]channelDayPoint, 0, days)
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		row := byDay[k] // zero value if missing -> zero-filled, matches statsClicksByDay
		daily = append(daily, channelDayPoint{
			Date: k, Views: row.Views, WatchMinutes: row.WatchMinutes,
			SubsGained: row.SubsGained, SubsLost: row.SubsLost,
		})
	}

	top, err := a.Q.VideoLeaderboard(r.Context(), db.VideoLeaderboardParams{
		ClientID: c.ID, FromTs: from, ToTs: to, RowLimit: 10,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	topVideos := make([]topVideoRow, 0, len(top))
	for _, v := range top {
		topVideos = append(topVideos, topVideoRow{
			ID: v.ID.String(), Title: v.Title, YoutubeVideoID: v.YoutubeVideoID, ThumbnailURL: v.ThumbnailUrl,
			Clicks: v.Clicks, Conversions: v.Conversions, RevenueCents: v.RevenueCents, CostCents: v.CostCents,
		})
	}

	writeJSON(w, http.StatusOK, channelAnalyticsResp{
		Overview: channelAnalyticsOverview{
			Views: ov.Views, WatchMinutes: ov.WatchMinutes,
			SubsGained: ov.SubsGained, SubsLost: ov.SubsLost, SubsNet: ov.SubsGained - ov.SubsLost,
			Likes: ov.Likes, Comments: ov.Comments,
		},
		Daily:     daily,
		TopVideos: topVideos,
	})
}
