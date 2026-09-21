package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"infopartnr-flow/backend/internal/auth"
	"infopartnr-flow/backend/internal/db"
)

// Requested once, up front, so we don't have to send the user back through
// consent when the video-sync and analytics work lands later.
const youtubeScopes = "https://www.googleapis.com/auth/youtube.readonly " +
	"https://www.googleapis.com/auth/youtube.force-ssl " +
	"https://www.googleapis.com/auth/yt-analytics.readonly"

func (a *API) youtubeRedirectURL() string {
	return strings.TrimRight(a.PublicURL, "/") + "/api/youtube/oauth/callback"
}

// ===== state param: client_id/workspace_id, tamper-proof via the same
// AES-GCM box used for integration secrets, expires in 10 minutes =====

type youtubeState struct {
	ClientID    uuid.UUID `json:"client_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Exp         int64     `json:"exp"`
}

func (a *API) encodeYoutubeState(clientID, workspaceID uuid.UUID) (string, error) {
	payload, err := json.Marshal(youtubeState{
		ClientID: clientID, WorkspaceID: workspaceID, Exp: time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	enc, err := a.Box.Encrypt(payload)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(enc), nil
}

func (a *API) decodeYoutubeState(s string) (youtubeState, error) {
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return youtubeState{}, err
	}
	dec, err := a.Box.Decrypt(raw)
	if err != nil {
		return youtubeState{}, err
	}
	var st youtubeState
	if err := json.Unmarshal(dec, &st); err != nil {
		return youtubeState{}, err
	}
	if time.Now().Unix() > st.Exp {
		return youtubeState{}, errors.New("state expired")
	}
	return st, nil
}

// ===== Google API calls (plain net/http, matching the Calendly client's
// style -- no google.golang.org/api dependency) =====

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (a *API) exchangeGoogleCode(ctx context.Context, code string) (googleTokenResponse, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {a.GoogleClientID},
		"client_secret": {a.GoogleClientSecret},
		"redirect_uri":  {a.youtubeRedirectURL()},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return googleTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return googleTokenResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return googleTokenResponse{}, errors.New("google token exchange failed: " + string(body))
	}
	var tok googleTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return googleTokenResponse{}, err
	}
	return tok, nil
}

func (a *API) refreshGoogleToken(ctx context.Context, refreshToken string) (string, error) {
	form := url.Values{
		"client_id":     {a.GoogleClientID},
		"client_secret": {a.GoogleClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("google token refresh failed: " + string(body))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

type youtubeChannelsResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title      string `json:"title"`
			Thumbnails struct {
				Default struct {
					URL string `json:"url"`
				} `json:"default"`
			} `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
}

func (a *API) fetchYoutubeChannel(ctx context.Context, accessToken string) (channelID, title, thumb string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/youtube/v3/channels?part=snippet&mine=true", nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", errors.New("youtube channels.list failed: " + string(body))
	}
	var out youtubeChannelsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", "", err
	}
	if len(out.Items) == 0 {
		return "", "", "", errors.New("no YouTube channel found on this Google account")
	}
	item := out.Items[0]
	return item.ID, item.Snippet.Title, item.Snippet.Thumbnails.Default.URL, nil
}

// ===== HTTP handlers =====

// connectYoutube returns a Google consent URL for the frontend to navigate
// the browser to (window.location.href = ...), not a redirect itself --
// the initiating request carries a Bearer token a browser redirect can't.
func (a *API) connectYoutube(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	p := auth.FromContext(r.Context())
	state, err := a.encodeYoutubeState(c.ID, p.WorkspaceID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	q := url.Values{
		"client_id":              {a.GoogleClientID},
		"redirect_uri":           {a.youtubeRedirectURL()},
		"response_type":          {"code"},
		"access_type":            {"offline"},
		"prompt":                 {"select_account consent"}, // forces a refresh_token every time, including reconnects
		"include_granted_scopes": {"true"},
		"scope":                  {youtubeScopes},
		"state":                  {state},
	}
	authURL := "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
}

// youtubeOAuthCallback is PUBLIC -- Google redirects the bare browser here,
// no Authorization header. The signed state param is what proves this
// belongs to a specific client/workspace; see encodeYoutubeState.
func (a *API) youtubeOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if reason := r.URL.Query().Get("error"); reason != "" {
		http.Redirect(w, r, strings.TrimRight(a.AppURL, "/")+"/clients?youtube=denied", http.StatusFound)
		return
	}

	st, err := a.decodeYoutubeState(r.URL.Query().Get("state"))
	if err != nil {
		http.Error(w, "This connection request expired or is invalid. Go back and click Connect YouTube again.", http.StatusBadRequest)
		return
	}
	redirectBase := strings.TrimRight(a.AppURL, "/") + "/c/" + st.ClientID.String() + "/integrations"

	// Defense in depth: re-check the client is still in the workspace the
	// state was minted for (it was already staff-authenticated when minted).
	if _, err := a.Q.GetClient(ctx, db.GetClientParams{ID: st.ClientID, WorkspaceID: st.WorkspaceID}); err != nil {
		http.Redirect(w, r, redirectBase+"?youtube=error", http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, redirectBase+"?youtube=error", http.StatusFound)
		return
	}

	tok, err := a.exchangeGoogleCode(ctx, code)
	if err != nil {
		log.Printf("youtube oauth: token exchange failed: %v", err)
		http.Redirect(w, r, redirectBase+"?youtube=error", http.StatusFound)
		return
	}
	if tok.RefreshToken == "" {
		// Shouldn't happen with prompt=consent, but if Google ever omits it,
		// there's nothing to store long-term -- fail loudly instead of
		// silently connecting a channel that will die on first token refresh.
		log.Printf("youtube oauth: no refresh_token in response (scope=%s)", tok.Scope)
		http.Redirect(w, r, redirectBase+"?youtube=error", http.StatusFound)
		return
	}

	channelID, title, thumb, err := a.fetchYoutubeChannel(ctx, tok.AccessToken)
	if err != nil {
		log.Printf("youtube oauth: channels.list failed: %v", err)
		http.Redirect(w, r, redirectBase+"?youtube=error", http.StatusFound)
		return
	}

	enc, err := a.Box.Encrypt([]byte(tok.RefreshToken))
	if err != nil {
		http.Redirect(w, r, redirectBase+"?youtube=error", http.StatusFound)
		return
	}

	ch, err := a.Q.UpsertYoutubeChannel(ctx, db.UpsertYoutubeChannelParams{
		ClientID: st.ClientID, GoogleChannelID: channelID, Title: title, ThumbnailUrl: thumb, RefreshTokenEnc: enc,
	})
	if err != nil {
		log.Printf("youtube oauth: upsert failed: %v", err)
		http.Redirect(w, r, redirectBase+"?youtube=error", http.StatusFound)
		return
	}

	// Detached context: r.Context() is cancelled once the redirect is sent.
	go func() {
		if err := a.SyncChannel(context.Background(), ch.ID, ch.ClientID, ch.GoogleChannelID, ch.RefreshTokenEnc); err != nil {
			log.Printf("youtube oauth: initial sync: %v", err)
		}
	}()

	http.Redirect(w, r, redirectBase+"?youtube=connected", http.StatusFound)
}

func (a *API) listYoutubeChannels(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	rows, err := a.Q.ListChannelsByClient(r.Context(), c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rows == nil {
		rows = []db.ListChannelsByClientRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (a *API) disconnectYoutube(w http.ResponseWriter, r *http.Request) {
	c := clientFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	n, err := a.Q.DeleteYoutubeChannel(r.Context(), db.DeleteYoutubeChannelParams{ID: id, ClientID: c.ID})
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
