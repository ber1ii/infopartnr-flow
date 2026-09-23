package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"infopartnr-flow/backend/internal/db"
)

const (
	injectMarkerStart = "── Related link ──"
	injectMarkerEnd   = "── 			 ──"
)

// injectLinkBlock replaces an existing flow:start/flow:end block if present,
// or appends a new one -- re-running injection never duplicates the link.
func injectLinkBlock(original, linkURL string) string {
	block := injectMarkerStart + "\n" + linkURL + "\n" + injectMarkerEnd
	startIdx := strings.Index(original, injectMarkerStart)
	endIdx := strings.Index(original, injectMarkerEnd)
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		endIdx += len(injectMarkerEnd)
		return original[:startIdx] + block + original[endIdx:]
	}
	sep := "\n\n"
	if strings.TrimSpace(original) == "" {
		sep = ""
	}
	return original + sep + block
}

type ytVideoSnippet struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	CategoryID      string   `json:"categoryId"`
	Tags            []string `json:"tags,omitempty"`
	DefaultLanguage string   `json:"defaultLanguage,omitempty"`
}

type ytVideoListResponse struct {
	Items []struct {
		ID      string         `json:"id"`
		Snippet ytVideoSnippet `json:"snippet"`
	} `json:"items"`
}

func (a *API) fetchVideoSnippet(ctx context.Context, access, youtubeVideoID string) (ytVideoSnippet, error) {
	body, err := a.googleGet(ctx, access, "https://www.googleapis.com/youtube/v3/videos", url.Values{
		"part": {"snippet"},
		"id":   {youtubeVideoID},
	})
	if err != nil {
		return ytVideoSnippet{}, err
	}
	var out ytVideoListResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return ytVideoSnippet{}, err
	}
	if len(out.Items) == 0 {
		return ytVideoSnippet{}, fmt.Errorf("video %s not found on YouTube (deleted/private?)", youtubeVideoID)
	}
	return out.Items[0].Snippet, nil
}

// updateVideoSnippet overwrites the ENTIRE snippet -- caller must have
// fetched title/categoryId/tags/defaultLanguage fresh and resend them
// unchanged, or YouTube silently wipes whatever's omitted.
func (a *API) updateVideoSnippet(ctx context.Context, access, youtubeVideoID string, s ytVideoSnippet) error {
	buf, err := json.Marshal(map[string]any{"id": youtubeVideoID, "snippet": s})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		"https://www.googleapis.com/youtube/v3/videos?part=snippet", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("videos.update failed: %d %s", resp.StatusCode, respBody)
	}
	return nil
}

// accessTokenForVideo resolves a fresh access token for the channel a video
// belongs to -- same refresh-token flow as SyncChannel, scoped to one video.
func (a *API) accessTokenForVideo(ctx context.Context, v db.Video, clientID uuid.UUID) (string, error) {
	if !v.ChannelID.Valid {
		return "", errors.New("this video has no connected channel")
	}
	ch, err := a.Q.GetChannelForSync(ctx, db.GetChannelForSyncParams{ID: v.ChannelID.UUID, ClientID: clientID})
	if err != nil {
		return "", errors.New("channel not connected or missing a token -- reconnect it first")
	}
	raw, err := a.Box.Decrypt(ch.RefreshTokenEnc)
	if err != nil {
		return "", err
	}
	access, err := a.refreshGoogleToken(ctx, string(raw))
	if err != nil {
		if strings.Contains(err.Error(), "invalid_grant") {
			_ = a.Q.MarkChannelRevoked(ctx, ch.ID)
		}
		return "", err
	}
	return access, nil
}

type injectionPreviewResp struct {
	CurrentDescription string `json:"current_description"`
	NewDescription     string `json:"new_description"`
	AlreadyInjected    bool   `json:"already_injected"`
}

// GET /clients/{clientID}/videos/{videoID}/description-injection/preview?link_url=...
// Fetches the CURRENT full description from YouTube and shows what it'd
// become -- writes nothing.
func (a *API) previewDescriptionInjection(w http.ResponseWriter, r *http.Request) {
	v, ok := a.videoCtx(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	linkURL := strings.TrimSpace(r.URL.Query().Get("link_url"))
	if linkURL == "" {
		writeErr(w, http.StatusBadRequest, "link_url is required")
		return
	}
	c := clientFrom(r.Context())
	access, err := a.accessTokenForVideo(r.Context(), v, c.ID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	snip, err := a.fetchVideoSnippet(r.Context(), access, v.YoutubeVideoID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, injectionPreviewResp{
		CurrentDescription: snip.Description,
		NewDescription:     injectLinkBlock(snip.Description, linkURL),
		AlreadyInjected:    strings.Contains(snip.Description, injectMarkerStart),
	})
}

// POST /clients/{clientID}/videos/{videoID}/description-injection
// body: {"description": "<final description text, e.g. from the preview above, possibly hand-edited>"}
// Re-fetches the snippet right before writing -- title/categoryId/tags/
// defaultLanguage are resent exactly as they are *now*, not as they were
// at preview time, so any drift since preview doesn't get wiped.
func (a *API) applyDescriptionInjection(w http.ResponseWriter, r *http.Request) {
	v, ok := a.videoCtx(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		Description string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Description) == "" {
		writeErr(w, http.StatusBadRequest, "description is required")
		return
	}
	c := clientFrom(r.Context())
	access, err := a.accessTokenForVideo(r.Context(), v, c.ID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	snip, err := a.fetchVideoSnippet(r.Context(), access, v.YoutubeVideoID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	snip.Description = in.Description
	if err := a.updateVideoSnippet(r.Context(), access, v.YoutubeVideoID, snip); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
