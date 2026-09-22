package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"infopartnr-flow/backend/internal/db"
)

const syncWindowDays = 90

func (a *API) googleGet(ctx context.Context, token, endpoint string, q url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google api %s: %d %s", endpoint, resp.StatusCode, body)
	}
	return body, nil
}

type ytPlaylistItemResponse struct {
	NextPageToken string `json:"nextPageToken"`
	Items         []struct {
		Snippet struct {
			Title       string `json:"title"`
			PublishedAt string `json:"publishedAt"`
			ResourceId  struct {
				VideoId string `json:"videoId"`
			} `json:"resourceId"`
			Thumbnails struct {
				High struct {
					Url string `json:"url"`
				} `json:"high"`
				Default struct {
					Url string `json:"url"`
				} `json:"default"`
			} `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
}

// SyncChannel pulls uploads + daily stats for one channel.
func (a *API) SyncChannel(ctx context.Context, channelID, clientID uuid.UUID, googleChannelID string, tokenEnc []byte) error {
	raw, err := a.Box.Decrypt(tokenEnc)
	if err != nil {
		return err
	}
	access, err := a.refreshGoogleToken(ctx, string(raw))
	if err != nil {
		if strings.Contains(err.Error(), "invalid_grant") {
			_ = a.Q.MarkChannelRevoked(ctx, channelID)
		}
		return err
	}

	videoIDs, err := a.syncUploads(ctx, access, channelID, clientID, googleChannelID)
	if err != nil {
		return fmt.Errorf("uploads: %w", err)
	}
	a.syncAnalytics(ctx, access, videoIDs)
	a.syncChannelAnalytics(ctx, access, channelID, clientID)

	return a.Q.UpdateChannelSyncStatus(ctx, channelID)
}

// returns youtube_video_id -> videos.id
func (a *API) syncUploads(ctx context.Context, access string, channelID, clientID uuid.UUID, googleChannelID string) (map[string]uuid.UUID, error) {
	if len(googleChannelID) < 3 {
		return nil, fmt.Errorf("bad channel id %q", googleChannelID)
	}
	uploads := "UU" + googleChannelID[2:]
	out := map[string]uuid.UUID{}
	pageToken := ""

	for page := 0; page < 20; page++ { // cap: 1000 videos
		q := url.Values{"part": {"snippet"}, "maxResults": {"50"}, "playlistId": {uploads}}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		body, err := a.googleGet(ctx, access, "https://www.googleapis.com/youtube/v3/playlistItems", q)
		if err != nil {
			return nil, err
		}
		var pl ytPlaylistItemResponse
		if err := json.Unmarshal(body, &pl); err != nil {
			return nil, err
		}
		for _, it := range pl.Items {
			s := it.Snippet
			if s.Title == "Private video" || s.Title == "Deleted video" {
				continue
			}
			pub, perr := time.Parse(time.RFC3339, s.PublishedAt)
			var pubPtr *time.Time
			if perr == nil {
				pubPtr = &pub
			}
			thumb := s.Thumbnails.High.Url
			if thumb == "" {
				thumb = s.Thumbnails.Default.Url
			}
			id, err := a.Q.UpsertVideo(ctx, db.UpsertVideoParams{
				ClientID:       clientID,
				ChannelID:      uuid.NullUUID{UUID: channelID, Valid: true},
				YoutubeVideoID: s.ResourceId.VideoId,
				Title:          s.Title,
				ThumbnailUrl:   thumb,
				PublishedAt:    pubPtr,
			})
			if err != nil {
				return nil, err
			}
			out[s.ResourceId.VideoId] = id
		}
		if pl.NextPageToken == "" {
			break
		}
		pageToken = pl.NextPageToken
	}
	return out, nil
}

// Per-video failures are logged, not fatal: one bad video shouldn't block the rest.
func (a *API) syncAnalytics(ctx context.Context, access string, videoIDs map[string]uuid.UUID) {
	end := time.Now().UTC().Format("2006-01-02")
	start := time.Now().UTC().AddDate(0, 0, -syncWindowDays).Format("2006-01-02")

	for ytID, vid := range videoIDs {
		body, err := a.googleGet(ctx, access, "https://youtubeanalytics.googleapis.com/v2/reports", url.Values{
			"ids":        {"channel==MINE"},
			"startDate":  {start},
			"endDate":    {end},
			"metrics":    {"views,estimatedMinutesWatched,subscribersGained"},
			"dimensions": {"day"},
			"filters":    {"video==" + ytID},
			"sort":       {"day"},
		})
		if err != nil {
			log.Printf("youtube sync: analytics %s: %v", ytID, err)
			continue
		}
		var rep struct {
			Rows [][]any `json:"rows"`
		}
		if err := json.Unmarshal(body, &rep); err != nil {
			log.Printf("youtube sync: analytics parse %s: %v", ytID, err)
			continue
		}
		for _, row := range rep.Rows {
			if len(row) < 4 {
				continue
			}
			ds, _ := row[0].(string)
			day, err := time.Parse("2006-01-02", ds)
			if err != nil {
				continue
			}
			num := func(v any) int64 { f, _ := v.(float64); return int64(f) }
			if err := a.Q.UpsertVideoStatDaily(ctx, db.UpsertVideoStatDailyParams{
				VideoID:      vid,
				Day:          pgtype.Date{Time: day, Valid: true},
				Views:        num(row[1]),
				WatchMinutes: num(row[2]),
				SubsGained:   int32(num(row[3])),
			}); err != nil {
				log.Printf("youtube sync: stat upsert %s: %v", ytID, err)
			}
		}
	}
}

// One Analytics API call per channel per sync, separate from the per-video
// loop in syncAnalytics -- ids=channel==MINE with no `filters` gives
// channel-level totals (subs gained/lost etc.) that aren't derivable by
// summing video_stats_daily (deleted/private videos, channel-only metrics).
func (a *API) syncChannelAnalytics(ctx context.Context, access string, channelID, clientID uuid.UUID) {
	end := time.Now().UTC().Format("2006-01-02")
	start := time.Now().UTC().AddDate(0, 0, -syncWindowDays).Format("2006-01-02")

	body, err := a.googleGet(ctx, access, "https://youtubeanalytics.googleapis.com/v2/reports", url.Values{
		"ids":        {"channel==MINE"},
		"startDate":  {start},
		"endDate":    {end},
		"metrics":    {"views,estimatedMinutesWatched,subscribersGained,subscribersLost,likes,comments"},
		"dimensions": {"day"},
		"sort":       {"day"},
	})
	if err != nil {
		log.Printf("youtube sync: channel analytics %s: %v", channelID, err)
		return
	}
	var rep struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(body, &rep); err != nil {
		log.Printf("youtube sync: channel analytics parse %s: %v", channelID, err)
		return
	}
	num := func(v any) int64 { f, _ := v.(float64); return int64(f) }
	for _, row := range rep.Rows {
		if len(row) < 7 {
			continue
		}
		ds, _ := row[0].(string)
		day, err := time.Parse("2006-01-02", ds)
		if err != nil {
			continue
		}
		if err := a.Q.UpsertChannelStatDaily(ctx, db.UpsertChannelStatDailyParams{
			ChannelID:    channelID,
			ClientID:     clientID,
			Day:          pgtype.Date{Time: day, Valid: true},
			Views:        num(row[1]),
			WatchMinutes: num(row[2]),
			SubsGained:   int32(num(row[3])),
			SubsLost:     int32(num(row[4])),
			Likes:        num(row[5]),
			Comments:     num(row[6]),
		}); err != nil {
			log.Printf("youtube sync: channel stat upsert %s: %v", channelID, err)
		}
	}
}

// ===== triggers =====

// POST /clients/{clientID}/youtube/channels/{channelID}/sync
func (a *API) syncYoutubeChannel(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	ch, err := a.Q.GetChannelForSync(r.Context(), db.GetChannelForSyncParams{ID: id, ClientID: c.ID})
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.SyncChannel(r.Context(), ch.ID, ch.ClientID, ch.GoogleChannelID, ch.RefreshTokenEnc); err != nil {
		log.Printf("youtube sync: channel %s: %v", ch.ID, err)
		writeErr(w, http.StatusBadGateway, "sync failed, check backend logs")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type youtubeSyncFailure struct {
	ChannelID string `json:"channel_id"`
	Error     string `json:"error"`
}

type youtubeSyncResponse struct {
	Synced []string             `json:"synced"`
	Failed []youtubeSyncFailure `json:"failed"`
}

// syncYoutubeClient handles POST /clients/{clientID}/youtube/sync -- syncs
// every connected channel for this client, sequentially, and returns once
// all of them are done. Per-channel failures don't abort the rest; they're
// reported back so the frontend can surface which channel needs attention
// (e.g. a revoked token).
func (a *API) syncYoutubeClient(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	chs, err := a.Q.ListClientChannelsToSync(r.Context(), c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(chs) == 0 {
		writeErr(w, http.StatusNotFound, "no connected channels for this client")
		return
	}

	resp := youtubeSyncResponse{Synced: []string{}, Failed: []youtubeSyncFailure{}}
	for _, ch := range chs {
		if err := a.SyncChannel(r.Context(), ch.ID, ch.ClientID, ch.GoogleChannelID, ch.RefreshTokenEnc); err != nil {
			log.Printf("youtube sync: client %s channel %s: %v", c.ID, ch.ID, err)
			resp.Failed = append(resp.Failed, youtubeSyncFailure{
				ChannelID: ch.ID.String(),
				Error:     "sync failed, check backend logs",
			})
			continue
		}
		resp.Synced = append(resp.Synced, ch.ID.String())
	}
	writeJSON(w, http.StatusOK, resp)
}
